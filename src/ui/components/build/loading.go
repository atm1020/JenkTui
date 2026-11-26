package build

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"github.com/atm1020/jenktui/src/jobref"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

// jobDetailsTree matches src/ui/components/job: projects only the job fields
// needed by the build page, instant ctrl+b triggers, replay/browser actions,
// and instant ctrl+j return to the job page, without fetching builds[].
const jobDetailsTree = "_class,name,url,color," +
	"healthReport[description,score]," +
	"lastBuild[number]," +
	"nextBuildNumber," +
	"property[parameterDefinitions[name,type,description,choices,defaultParameterValue[name,value]]]"

// buildDetailsTree projects only the build fields needed by the build page and
// its actions: build number (artifact fingerprint fallback), trigger causes
// (Started by / Details), and artifacts (Artifacts list & download). Build
// parameters (p) are fetched on demand from /parameters/ HTML.
const buildDetailsTree = "number," +
	"actions[causes[userName,userId,shortDescription]]," +
	"artifacts[fileName,relativePath]"

func (m *Model) fetchBuild() tea.Cmd {
	m.cancelInFlightRequest()
	requestCtx, cancel := m.ctx.RequestContext()
	m.cancelRequest = cancel
	m.fetchGeneration++
	generation := m.fetchGeneration
	run, jobURL, jenkins := m.pipelineRun, m.jobURL, m.ctx.Jenkins

	return func() tea.Msg {
		defer cancel()
		msg := loadBuild(requestCtx, jenkins, jobURL, run)
		msg.model = m
		msg.Generation = generation
		return msg
	}
}

type loadStepFailure struct {
	err   error
	title string
}

// pickLoadFailure selects the most relevant failure from a parallel load phase.
// When the parent context is still active, sibling cancellations triggered by
// another step's error are ignored so the root failure and its title win.
func pickLoadFailure(parentCtx context.Context, failures ...loadStepFailure) (loadStepFailure, bool) {
	if errors.Is(parentCtx.Err(), context.Canceled) {
		for _, f := range failures {
			if f.err != nil {
				return loadStepFailure{err: parentCtx.Err(), title: f.title}, true
			}
		}
		return loadStepFailure{err: parentCtx.Err(), title: "Could not load build details"}, true
	}
	if parentCtx.Err() == nil {
		for _, f := range failures {
			if f.err != nil && !errors.Is(f.err, context.Canceled) {
				return f, true
			}
		}
	}
	for _, f := range failures {
		if f.err != nil {
			return f, true
		}
	}
	if err := parentCtx.Err(); err != nil {
		return loadStepFailure{err: err, title: "Could not load build details"}, true
	}
	return loadStepFailure{}, false
}

func fetchJobDetails(ctx context.Context, jenkins *gojenkins.Jenkins, job *gojenkins.Job) error {
	resp, err := jenkins.Requester.GetJSON(ctx, job.Base, job.Raw, map[string]string{"tree": jobDetailsTree})
	if err != nil {
		return err
	}
	if resp == nil {
		return errors.New("empty response from Jenkins")
	}
	// The requester has already drained and closed the body.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return errors.New(strconv.Itoa(resp.StatusCode))
}

func fetchBuildDetails(ctx context.Context, job *gojenkins.Job, id int64) (*gojenkins.Build, error) {
	if job == nil || job.Jenkins == nil {
		return nil, errors.New("jenkins client is unavailable")
	}
	base := strings.TrimRight(job.Base, "/")
	if base == "" && job.Raw != nil && job.Raw.URL != "" {
		base = strings.TrimRight(strings.ReplaceAll(job.Raw.URL, job.Jenkins.Server, ""), "/")
	}
	b := &gojenkins.Build{
		Jenkins: job.Jenkins,
		Job:     job,
		Raw:     new(gojenkins.BuildResponse),
		Base:    base + "/" + strconv.FormatInt(id, 10),
	}
	resp, err := job.Jenkins.Requester.GetJSON(ctx, b.Base, b.Raw, map[string]string{"tree": buildDetailsTree})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, errors.New("empty response from Jenkins")
	}
	// The requester has already drained and closed the body.
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusOK {
		return b, nil
	}
	return nil, errors.New(strconv.Itoa(resp.StatusCode))
}

func extractBuildCause(build *gojenkins.Build) (startedBy, startedByDetails string) {
	if build == nil || build.Raw == nil {
		return "", ""
	}
	for _, action := range build.Raw.Actions {
		if action.Causes == nil {
			continue
		}
		for _, cause := range action.Causes {
			if user, ok := cause["userName"]; ok {
				startedBy = fmt.Sprintf("%v", user)
			} else if user, ok := cause["userId"]; ok {
				startedBy = fmt.Sprintf("%v", user)
			}
			if desc, ok := cause["shortDescription"]; ok {
				startedByDetails = fmt.Sprintf("%v", desc)
			}
		}
		break
	}
	return startedBy, startedByDetails
}

// loadBuild gathers everything needed for the first render. A known run from
// Job skips discovery and fetches projected build details and the initial stage
// node concurrently (1 RTT). A URL-only / My Runs reference resolves projected
// job details, pipeline run summary, and projected build details in parallel
// before fetching the initial stage node (2 RTTs). One context cancels the
// entire initial load, including its final stage-details request.
func loadBuild(ctx context.Context, jenkins *gojenkins.Jenkins, jobURL string, run gojenkins.PipelineRun) BuildFetchedMsg {
	id, err := strconv.ParseInt(run.ID, 10, 64)
	if err != nil || id <= 0 {
		return BuildFetchedMsg{Error: fmt.Errorf("invalid build ID: %q", run.ID), ErrorTitle: "Could not load build details"}
	}

	needJob := run.Job == nil
	needRun := run.Status == ""

	job := run.Job
	if needJob {
		if jenkins == nil || !jobref.IsJobURL(jobURL) {
			return BuildFetchedMsg{Error: fmt.Errorf("job is unavailable"), ErrorTitle: "Failed to get job"}
		}
		jobID, parents := jobref.ToIDAndParents(jobURL)
		job = jenkins.JobRef(jobID, parents...)
		run.Job = job
	}
	if run.Base == "" && job != nil && job.Base != "" {
		run.Base = strings.TrimRight(job.Base, "/") + "/" + run.ID
	}

	fetchNodeInPhase1 := !needRun && len(run.Stages) > 0

	var (
		jobErr   error
		loaded   *gojenkins.PipelineRun
		runErr   error
		build    *gojenkins.Build
		buildErr error
		node     *gojenkins.PipelineNode
		nodeErr  error
		stage    gojenkins.PipelineNode
	)
	if fetchNodeInPhase1 {
		stage = run.Stages[focusedStageIndex(run.Stages, 0)]
	}

	phaseCtx, cancelPhase := context.WithCancel(ctx)
	defer cancelPhase()

	var wg sync.WaitGroup
	if needJob {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := fetchJobDetails(phaseCtx, jenkins, job); err != nil {
				jobErr = err
				cancelPhase()
			}
		}()
	}
	if needRun {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r, err := job.GetPipelineRun(phaseCtx, run.ID)
			if err != nil {
				runErr = err
				cancelPhase()
				return
			}
			loaded = r
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		b, err := fetchBuildDetails(phaseCtx, job, id)
		if err != nil {
			buildErr = err
			cancelPhase()
			return
		}
		build = b
	}()

	if fetchNodeInPhase1 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			n, err := run.GetNode(phaseCtx, stage.ID)
			if err != nil {
				nodeErr = err
				cancelPhase()
				return
			}
			node = n
		}()
	}

	wg.Wait()

	if failure, ok := pickLoadFailure(ctx,
		loadStepFailure{err: jobErr, title: "Failed to get job"},
		loadStepFailure{err: runErr, title: fmt.Sprintf("Failed to get build #%s", run.ID)},
		loadStepFailure{err: buildErr, title: fmt.Sprintf("Failed to fetch build #%s", run.ID)},
		loadStepFailure{err: nodeErr, title: fmt.Sprintf("Could not get '%s' Build stage details", stage.Name)},
	); ok {
		return BuildFetchedMsg{Error: failure.err, ErrorTitle: failure.title}
	}

	if needRun && loaded != nil {
		// Keep the requested identity even when Jenkins omits wfapi self links.
		loaded.ID = run.ID
		if loaded.Base == "" {
			loaded.Base = strings.TrimRight(job.Base, "/") + "/" + run.ID
		}
		loaded.Job = job
		run = *loaded
	}

	if needRun && len(run.Stages) > 0 {
		stage = run.Stages[focusedStageIndex(run.Stages, 0)]
		node, err = run.GetNode(ctx, stage.ID)
		if err != nil {
			return BuildFetchedMsg{Error: err, ErrorTitle: fmt.Sprintf("Could not get '%s' Build stage details", stage.Name)}
		}
	}

	msg := BuildFetchedMsg{PipelineRun: run, Build: *build, Node: node}
	msg.StartedBy, msg.StartedByDetails = extractBuildCause(build)
	return msg
}
