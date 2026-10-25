package buildaction

import (
	"errors"
	"fmt"
	"strconv"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/confirm"
	"github.com/atm1020/jenktui/src/ui/util"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

// ConfirmAbort asks before stopping a running build. Finished builds are
// answered locally because Jenkins cannot cancel them.
func ConfirmAbort(ctx *context.ProgramContext, job *gojenkins.Job, buildID string, finished bool) tea.Cmd {
	return confirmAbort(ctx, buildID, finished, Abort(ctx, job, buildID))
}

// ConfirmAbortByURL is ConfirmAbort for callers that know only the job's URL,
// such as My Runs rows. The job is resolved after the user confirms, so an
// unnecessary Jenkins round trip is avoided when they decline.
func ConfirmAbortByURL(ctx *context.ProgramContext, jobURL, buildID string, finished bool) tea.Cmd {
	return confirmAbort(ctx, buildID, finished, AbortByURL(ctx, jobURL, buildID))
}

func confirmAbort(ctx *context.ProgramContext, buildID string, finished bool, abort tea.Cmd) tea.Cmd {
	if finished {
		return util.CmdHandler(util.FooterNotiMsg{
			Message: fmt.Sprintf("Build #%s has already finished", buildID),
			Type:    util.NotiWarning,
		})
	}

	dialog := confirm.NewConfirmDialog(
		fmt.Sprintf("Are you sure you want to cancel build #%s?", buildID),
		abort,
		util.CmdHandler(util.FooterNotiMsg{Message: "Build cancellation aborted"}),
		ctx.Config.KeyMapConfig.Dialog.Confirm,
	)
	return util.CmdHandler(util.OpenDialogMsg{Dialog: dialog})
}

// Abort stops a running build. Jenkins reports the stop as a plain boolean, so
// a refused stop is surfaced as an error rather than a silent no-op.
func Abort(ctx *context.ProgramContext, job *gojenkins.Job, buildID string) tea.Cmd {
	return func() tea.Msg {
		if job == nil {
			return util.ErrorMsg{
				Error: errors.New("job is not available"),
				Title: "Failed to cancel build",
			}
		}
		requestCtx, cancel := ctx.RequestContext()
		defer cancel()

		id, err := strconv.ParseInt(buildID, 10, 64)
		if err != nil {
			return util.ErrorMsg{Error: err, Title: "Failed to parse build ID"}
		}

		build, err := job.GetBuild(requestCtx, id)
		if err != nil {
			return util.ErrorMsg{Error: err, Title: "Failed to get build"}
		}

		stopped, err := build.Stop(requestCtx)
		if err != nil {
			return util.ErrorMsg{Error: err, Title: "Failed to cancel build"}
		}
		if !stopped {
			return util.ErrorMsg{
				Error: fmt.Errorf("build was not stopped"),
				Title: "Cancel failed",
			}
		}

		return util.FooterNotiMsg{
			Message: fmt.Sprintf("Build #%s cancelled successfully", buildID),
		}
	}
}

// AbortByURL resolves the job from its URL before stopping the build.
func AbortByURL(ctx *context.ProgramContext, jobURL, buildID string) tea.Cmd {
	return func() tea.Msg {
		job, err := resolveJob(ctx, jobURL)
		if err != nil {
			return util.ErrorMsg{Error: err, Title: "Failed to get job"}
		}
		return Abort(ctx, job, buildID)()
	}
}
