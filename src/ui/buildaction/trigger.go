// Package buildaction holds actions shared by the job, build, and My Runs
// views: opening jobs, logs, and browser pages, as well as starting and
// stopping builds. Each view knows a run by different means (a live job, a
// pipeline run, or just a stored URL), so entry points take the least a caller
// can be expected to have.
package buildaction

import (
	cc "context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/buildparams"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/confirm"
	"github.com/atm1020/jenktui/src/ui/util"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

type paramsRequestID uint64

var paramsRequestSequence atomic.Uint64

// ParamsRequest owns one view's parameter-fetch flow. Starting another fetch
// or invalidating the request makes any earlier result safe to ignore.
type ParamsRequest struct {
	id paramsRequestID
}

// ParamsFetchedMsg carries the result of one parameter request.
type ParamsFetchedMsg struct {
	requestID paramsRequestID
	Job       *gojenkins.Job
	Params    []gojenkins.ParameterDefinition
	Error     error
}

// Fetch loads the parameter definitions of an already-resolved job.
func (r *ParamsRequest) Fetch(ctx *context.ProgramContext, job *gojenkins.Job) tea.Cmd {
	r.id = paramsRequestID(paramsRequestSequence.Add(1))
	return fetchParams(ctx, job, r.id)
}

// FetchByURL resolves a job from its URL first. My Runs rows name a job by URL
// only, so the job has to be fetched before its parameters are known.
func (r *ParamsRequest) FetchByURL(ctx *context.ProgramContext, jobURL string) tea.Cmd {
	r.id = paramsRequestID(paramsRequestSequence.Add(1))
	requestID := r.id
	return func() tea.Msg {
		job, err := resolveJob(ctx, jobURL)
		if err != nil {
			return ParamsFetchedMsg{requestID: requestID, Error: err}
		}
		return fetchParams(ctx, job, requestID)()
	}
}

// Handle opens the trigger flow only when msg belongs to this request.
func (r *ParamsRequest) Handle(ctx *context.ProgramContext, windowWidth int, msg ParamsFetchedMsg) tea.Cmd {
	return r.handle(ctx, windowWidth, msg, nil)
}

// HandleWithTrigger is Handle with a caller-supplied trigger command. It lets a
// view annotate a successful trigger without duplicating the parameter dialogs.
func (r *ParamsRequest) HandleWithTrigger(
	ctx *context.ProgramContext,
	windowWidth int,
	msg ParamsFetchedMsg,
	trigger func(*gojenkins.Job, map[string]string) tea.Cmd,
) tea.Cmd {
	return r.handle(ctx, windowWidth, msg, trigger)
}

func (r *ParamsRequest) handle(
	ctx *context.ProgramContext,
	windowWidth int,
	msg ParamsFetchedMsg,
	trigger func(*gojenkins.Job, map[string]string) tea.Cmd,
) tea.Cmd {
	if msg.requestID == 0 || msg.requestID != r.id {
		return nil
	}
	r.Invalidate()
	if trigger == nil {
		return OpenTriggerDialog(ctx, windowWidth, msg)
	}
	return openTriggerDialog(ctx, windowWidth, msg, trigger)
}

// Invalidate prevents an in-flight result from affecting the view after it is
// superseded or the view is left.
func (r *ParamsRequest) Invalidate() {
	r.id = 0
}

func fetchParams(ctx *context.ProgramContext, job *gojenkins.Job, requestID paramsRequestID) tea.Cmd {
	return func() tea.Msg {
		if job == nil {
			return ParamsFetchedMsg{requestID: requestID, Error: errors.New("no job is available for this view")}
		}
		requestCtx, cancel := ctx.RequestContext()
		defer cancel()
		params, err := job.GetParameters(requestCtx)
		return ParamsFetchedMsg{requestID: requestID, Job: job, Params: params, Error: err}
	}
}

// OpenTriggerDialog turns a fetched parameter set into the dialog that performs the
// trigger: a plain confirmation for an unparameterised job, the parameter form
// otherwise.
func OpenTriggerDialog(ctx *context.ProgramContext, windowWidth int, msg ParamsFetchedMsg) tea.Cmd {
	return openTriggerDialog(ctx, windowWidth, msg, func(job *gojenkins.Job, params map[string]string) tea.Cmd {
		return Trigger(ctx, job, params)
	})
}

func openTriggerDialog(
	ctx *context.ProgramContext,
	windowWidth int,
	msg ParamsFetchedMsg,
	trigger func(*gojenkins.Job, map[string]string) tea.Cmd,
) tea.Cmd {
	if msg.Error != nil {
		return util.CmdHandler(util.ErrorMsg{
			Error: msg.Error,
			Title: "Failed to fetch build parameters",
		})
	}
	if msg.Job == nil {
		return util.CmdHandler(util.ErrorMsg{
			Error: errors.New("job is not available"),
			Title: "Failed to trigger build",
		})
	}

	onCancel := util.CmdHandler(util.FooterNotiMsg{Message: "Build trigger aborted"})

	if len(msg.Params) == 0 {
		dialog := confirm.NewConfirmDialog(
			fmt.Sprintf("Are you sure you want to trigger a new build for %s?", msg.Job.GetName()),
			trigger(msg.Job, nil),
			onCancel,
			ctx.Config.KeyMapConfig.Dialog.Confirm,
		)
		return util.CmdHandler(util.OpenDialogMsg{Dialog: dialog})
	}

	onBuild := func(params map[string]string) tea.Cmd {
		return trigger(msg.Job, params)
	}

	dialog := buildparams.NewBuildParamsDialog(
		ctx,
		msg.Job,
		msg.Job.GetName(),
		msg.Params,
		windowWidth,
		onBuild,
		onCancel,
		ctx.Config.KeyMapConfig.Dialog.BuildParams,
	)
	return util.CmdHandler(util.OpenDialogMsg{Dialog: dialog})
}

// Trigger starts the build. The trigger intent is persisted before Jenkins is
// called so a queue receipt is never lost, and the resulting RunTriggeredMsg
// tells the app shell which job to start tracking.
func Trigger(ctx *context.ProgramContext, job *gojenkins.Job, params map[string]string) tea.Cmd {
	return func() tea.Msg {
		if ctx == nil || ctx.PollManager == nil {
			return util.ErrorMsg{
				Error: errors.New("poll manager is not available"),
				Title: "Failed to trigger build",
			}
		}
		if job == nil {
			return util.ErrorMsg{
				Error: errors.New("job is not available"),
				Title: "Failed to trigger build",
			}
		}
		// Trigger work gets a deadline but is deliberately not tied to CleanUp:
		// cancelling after Jenkins accepts it could prevent intent persistence.
		requestCtx, cancel := ctx.RequestContext()
		defer cancel()

		if params == nil {
			params = make(map[string]string)
		}
		params["delay"] = "0sec"

		jobURL := job.GetDetails().URL
		intent, err := poll.NewTriggerIntent(jobURL, poll.KnownBuildBaseline(job), time.Now())
		if err != nil {
			return util.ErrorMsg{Error: err, Title: "Failed to prepare build tracking"}
		}
		invoke := func(invokeCtx cc.Context) (int64, error) {
			queueID, invokeErr := job.InvokeSimple(invokeCtx, params)
			if invokeErr != nil {
				return 0, invokeErr
			}
			if queueID <= 0 {
				return 0, errors.New("jenkins accepted the build trigger without returning a valid queue ID")
			}
			return queueID, nil
		}

		queueID, err := poll.CoordinateTrigger(requestCtx, ctx.PollManager, intent, invoke)
		if err != nil {
			return util.ErrorMsg{
				Error: err,
				Title: "Failed to trigger build",
			}
		}

		return util.RunTriggeredMsg{
			Job: job, IntentID: intent.IntentID, QueueID: store.QueueID(queueID), Tracked: true,
		}
	}
}

// resolveJob loads the job a URL names. Views that store only identifiers
// (My Runs rows) need the live job before they can act on it.
func resolveJob(ctx *context.ProgramContext, jobURL string) (*gojenkins.Job, error) {
	if ctx == nil || ctx.Jenkins == nil {
		return nil, errors.New("jenkins client is not available")
	}
	requestCtx, cancel := ctx.RequestContext()
	defer cancel()
	id, parentIDs := jobref.ToIDAndParents(jobURL)
	return ctx.Jenkins.GetJob(requestCtx, id, parentIDs...)
}
