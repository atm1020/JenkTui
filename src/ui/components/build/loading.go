package build

import (
	"context"
	"fmt"
	"strconv"

	"github.com/atm1020/jenktui/src/jobref"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

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

// loadBuild gathers everything needed for the first render. A known run from
// Job skips discovery; a stored My Runs reference (or a build-list entry without
// a run summary) is resolved here, after navigation. One context cancels the
// entire initial load, including its final stage-details request.
func loadBuild(ctx context.Context, jenkins *gojenkins.Jenkins, jobURL string, run gojenkins.PipelineRun) BuildFetchedMsg {
	id, err := strconv.ParseInt(run.ID, 10, 64)
	if err != nil || id <= 0 {
		return BuildFetchedMsg{Error: fmt.Errorf("invalid build ID: %q", run.ID), ErrorTitle: "Could not load build details"}
	}

	if run.Job == nil {
		if jenkins == nil || !jobref.IsJobURL(jobURL) {
			return BuildFetchedMsg{Error: fmt.Errorf("job is unavailable"), ErrorTitle: "Failed to get job"}
		}
		jobID, parents := jobref.ToIDAndParents(jobURL)
		run.Job, err = jenkins.GetJob(ctx, jobID, parents...)
		if err != nil {
			return BuildFetchedMsg{Error: err, ErrorTitle: "Failed to get job"}
		}
	}

	if run.Status == "" {
		loaded, err := run.Job.GetPipelineRun(ctx, run.ID)
		if err != nil {
			return BuildFetchedMsg{Error: err, ErrorTitle: fmt.Sprintf("Failed to get build #%s", run.ID)}
		}
		// Keep the requested identity even when Jenkins omits wfapi self links.
		loaded.ID = run.ID
		if loaded.Base == "" {
			loaded.Base = run.Job.Base + "/" + run.ID
		}
		run = *loaded
	}

	build, err := run.Job.GetBuild(ctx, id)
	if err != nil {
		return BuildFetchedMsg{Error: err, ErrorTitle: fmt.Sprintf("Failed to fetch build #%s", run.ID)}
	}
	msg := BuildFetchedMsg{PipelineRun: run, Build: *build}
	for _, action := range build.Raw.Actions {
		if action.Causes == nil {
			continue
		}
		for _, cause := range action.Causes {
			if user, ok := cause["userName"]; ok {
				msg.StartedBy = fmt.Sprintf("%v", user)
			} else if user, ok := cause["userId"]; ok {
				msg.StartedBy = fmt.Sprintf("%v", user)
			}
			if desc, ok := cause["shortDescription"]; ok {
				msg.StartedByDetails = fmt.Sprintf("%v", desc)
			}
		}
		break
	}

	if len(run.Stages) > 0 {
		stage := run.Stages[focusedStageIndex(run.Stages, 0)]
		msg.Node, err = run.GetNode(ctx, stage.ID)
		if err != nil {
			return BuildFetchedMsg{Error: err, ErrorTitle: fmt.Sprintf("Could not get '%s' Build stage details", stage.Name)}
		}
	}
	return msg
}
