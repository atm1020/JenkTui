package build

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/stagegraph"

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
	cache := m.ctx.GraphCapabilityCache()
	serverKey := m.ctx.GraphServerKey()

	return func() tea.Msg {
		defer cancel()
		msg := loadBuild(requestCtx, jenkins, cache, serverKey, jobURL, run)
		msg.model = m
		msg.Generation = generation
		return msg
	}
}

// loadBuild gathers everything needed for the first render. A known run from
// Job skips discovery; a stored My Runs reference (or a build-list entry without
// a run summary) is resolved here, after navigation. One context cancels the
// entire initial load, including the optional pipeline-graph-view probe and the
// final stage-details request.
func loadBuild(
	ctx context.Context,
	jenkins *gojenkins.Jenkins,
	cache *stagegraph.CapabilityCache,
	serverKey string,
	jobURL string,
	run gojenkins.PipelineRun,
) BuildFetchedMsg {
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

	var (
		build      *gojenkins.Build
		buildErr   error
		pluginTree *stagegraph.PluginTree
	)

	if len(run.Stages) > 0 && jenkins != nil && jenkins.Requester != nil {
		// Fetch build metadata and optional pipeline-graph-view tree concurrently
		// under one cancellable child context so plugin discovery adds no extra
		// serial round-trip and fails softly back to wfapi.
		childCtx, childCancel := context.WithCancel(ctx)
		var wg sync.WaitGroup
		wg.Add(1)
		go func() {
			defer wg.Done()
			tree, ok, _ := stagegraph.FetchTree(childCtx, jenkins, cache, serverKey, run)
			if ok {
				pluginTree = tree
			}
		}()
		build, buildErr = run.Job.GetBuild(childCtx, id)
		if buildErr != nil {
			childCancel()
		}
		wg.Wait()
		childCancel()
	} else {
		build, buildErr = run.Job.GetBuild(ctx, id)
	}

	if buildErr != nil {
		return BuildFetchedMsg{Error: buildErr, ErrorTitle: fmt.Sprintf("Failed to fetch build #%s", run.ID)}
	}

	var graph stagegraph.Graph
	if pluginTree != nil {
		graph = stagegraph.FromPluginTree(pluginTree, run)
	} else {
		graph = stagegraph.FromWFAPI(run)
	}

	msg := BuildFetchedMsg{
		PipelineRun: run,
		PluginTree:  pluginTree,
		StageGraph:  &graph,
		Build:       *build,
	}
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
		targetStage := initialStageToFetch(graph, run.Stages)
		if targetStage.ID != "" {
			msg.FocusedStageID = targetStage.ID
			msg.Node, err = run.GetNode(ctx, targetStage.ID)
			if err != nil {
				return BuildFetchedMsg{Error: err, ErrorTitle: fmt.Sprintf("Could not get '%s' Build stage details", targetStage.Name)}
			}
		}
	}
	return msg
}

func initialStageToFetch(graph stagegraph.Graph, rawStages []gojenkins.PipelineNode) stagegraph.StageNode {
	rows := stagegraph.Flatten(graph)
	idx := stagegraph.InitialFocusIndex(rows)
	if idx >= 0 {
		for i := range rows {
			if rows[i].Navigable && rows[i].NavIndex == idx {
				return rows[i].Node
			}
		}
	}
	if len(rawStages) > 0 {
		s := rawStages[0]
		return stagegraph.StageNode{ID: s.ID, Name: s.Name}
	}
	return stagegraph.StageNode{}
}
