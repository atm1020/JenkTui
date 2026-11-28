package build

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/stagegraph"
	"github.com/atm1020/jenktui/src/ui/styles"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

func webappBuildStages(base int64, failBranch string) []gojenkins.PipelineNode {
	unitStatus, intStatus, lintStatus := "SUCCESS", "SUCCESS", "SUCCESS"
	switch failBranch {
	case "Unit":
		unitStatus = "FAILED"
	case "Integration":
		intStatus = "FAILED"
	case "Lint":
		lintStatus = "FAILED"
	}
	return []gojenkins.PipelineNode{
		{ID: "6", Name: "Install", Status: "SUCCESS", StartTime: base + 100, Duration: 2100},
		{ID: "11", Name: "Parallel Tests", Status: "SUCCESS", StartTime: base + 2250, Duration: 350},
		{ID: "15", Name: "Unit", Status: unitStatus, StartTime: base + 2600, Duration: 4200},
		{ID: "16", Name: "Integration", Status: intStatus, StartTime: base + 2615, Duration: 6200},
		{ID: "17", Name: "Lint", Status: lintStatus, StartTime: base + 2620, Duration: 3200},
		{ID: "30", Name: "Bundle", Status: "SUCCESS", StartTime: base + 8900, Duration: 3100},
	}
}

func TestBuildPageParallelPluginGraphModernLegacyMissingAndReset(t *testing.T) {
	base := int64(1_700_000_000_000)

	t.Run("modern plugin-graph-view auto-focuses failed parallel branch and renders tree", func(t *testing.T) {
		var overviewHits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/job/webapp//42/api/json":
				_, _ = w.Write([]byte(`{"number":42,"result":"FAILURE","actions":[{"causes":[{"userName":"Alice","shortDescription":"Started by Alice"}]}]}`))
			case "/job/webapp/42/pipeline-overview/tree":
				overviewHits.Add(1)
				_, _ = w.Write([]byte(`{
					"status":"ok",
					"data":{
						"complete":true,
						"stages":[
							{"id":6,"name":"Install","state":"FINISHED","type":"STAGE"},
							{"id":11,"name":"Parallel Tests","state":"FAILED","type":"PARALLEL_BLOCK","children":[
								{"id":15,"name":"Unit","state":"FINISHED","type":"PARALLEL"},
								{"id":16,"name":"Integration","state":"FAILED","type":"PARALLEL"},
								{"id":17,"name":"Lint","state":"FINISHED","type":"PARALLEL"}
							]},
							{"id":30,"name":"Bundle","state":"SKIPPED","type":"SKIPPED_STAGE"}
						]
					}
				}`))
			case "/job/webapp/42/execution/node/16/wfapi/describe/api/json":
				_, _ = w.Write([]byte(`{"id":"16","name":"Integration","status":"FAILED","execNode":"ci-agent-2","stageFlowNodes":[{"id":"99","name":"npm run test:int","status":"FAILED"}]}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		ctx := buildLoadContext(srv)
		job := &gojenkins.Job{Jenkins: ctx.Jenkins, Base: "/job/webapp", Raw: &gojenkins.JobResponse{Name: "webapp", URL: srv.URL + "/job/webapp/"}}
		run := gojenkins.PipelineRun{
			ID: "42", Name: "#42", Job: job, Base: "/job/webapp/42",
			Status: "FAILED", StartTime: base, EndTime: base + 12_000, Duration: 12_000,
			Stages: webappBuildStages(base, "Integration"),
		}
		m := NewModel(ctx, run, 120, 40)
		defer func() { _ = m.CleanUp() }()

		msg := m.fetchBuild()()
		_, _ = m.Update(msg)

		if overviewHits.Load() != 1 {
			t.Fatalf("overview hits = %d, want 1", overviewHits.Load())
		}
		if m.stageGraph.Source != stagegraph.SourcePluginGraph {
			t.Fatalf("stageGraph source = %q, want plugin-graph", m.stageGraph.Source)
		}
		if m.stageDetails.pipelineNode == nil || m.stageDetails.pipelineNode.ID != "16" {
			t.Fatalf("auto-focused stage details = %#v, want Integration (ID 16)", m.stageDetails.pipelineNode)
		}
		plain := ansi.Strip(m.View())
		for _, want := range []string{"STAGE", "Install", "Parallel Tests", "├─", "└─", "Unit", "Integration", "Lint", "Bundle", "npm run test:int"} {
			if !strings.Contains(plain, want) {
				t.Fatalf("timeline view missing %q:\n%s", want, plain)
			}
		}
	})

	t.Run("missing plugin caches unavailable across builds and resets on server switch", func(t *testing.T) {
		var probeHits atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/job/webapp/42/pipeline-overview/tree", "/job/webapp/42/pipeline-graph/tree",
				"/job/webapp/43/pipeline-overview/tree", "/job/webapp/43/pipeline-graph/tree":
				probeHits.Add(1)
				http.NotFound(w, r)
			case "/job/webapp//42/api/json", "/job/webapp//43/api/json":
				_, _ = w.Write([]byte(`{"number":42,"result":"SUCCESS"}`))
			case "/job/webapp/42/execution/node/6/wfapi/describe/api/json",
				"/job/webapp/43/execution/node/6/wfapi/describe/api/json":
				_, _ = w.Write([]byte(`{"id":"6","name":"Install","status":"SUCCESS"}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		ctx := buildLoadContext(srv)
		job := &gojenkins.Job{Jenkins: ctx.Jenkins, Base: "/job/webapp", Raw: &gojenkins.JobResponse{Name: "webapp", URL: srv.URL + "/job/webapp/"}}
		run42 := gojenkins.PipelineRun{
			ID: "42", Name: "#42", Job: job, Base: "/job/webapp/42",
			Status: "SUCCESS", StartTime: base, EndTime: base + 12_000, Duration: 12_000,
			Stages: webappBuildStages(base, ""),
		}

		m1 := NewModel(ctx, run42, 120, 40)
		defer func() { _ = m1.CleanUp() }()
		_, _ = m1.Update(m1.fetchBuild()())
		if probeHits.Load() != 2 {
			t.Fatalf("first build probe hits = %d, want 2 (overview + legacy 404)", probeHits.Load())
		}
		if m1.stageGraph.Source != stagegraph.SourceWFAPI {
			t.Fatalf("source = %q, want wfapi fallback", m1.stageGraph.Source)
		}

		// Second build on the same server must make ZERO graph probes.
		run43 := run42
		run43.ID, run43.Name, run43.Base = "43", "#43", "/job/webapp/43"
		m2 := NewModel(ctx, run43, 120, 40)
		defer func() { _ = m2.CleanUp() }()
		_, _ = m2.Update(m2.fetchBuild()())
		if probeHits.Load() != 2 {
			t.Fatalf("second build repeated graph probes: total hits = %d, want 2", probeHits.Load())
		}

		// Resetting capabilities on server switch allows probing again.
		ctx.ResetGraphCapabilities()
		m3 := NewModel(ctx, run43, 120, 40)
		defer func() { _ = m3.CleanUp() }()
		_, _ = m3.Update(m3.fetchBuild()())
		if probeHits.Load() != 4 {
			t.Fatalf("after server switch reset, probe hits = %d, want 4", probeHits.Load())
		}
	})

	t.Run("transient 500 falls back to wfapi without poisoning cache", func(t *testing.T) {
		var attempt atomic.Int32
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/job/webapp/42/pipeline-overview/tree":
				if attempt.Add(1) == 1 {
					http.Error(w, "temporary outage", http.StatusInternalServerError)
					return
				}
				_, _ = w.Write([]byte(`{"data":{"complete":true,"stages":[{"id":6,"name":"Install","state":"FINISHED","type":"STAGE"}]}}`))
			case "/job/webapp//42/api/json":
				_, _ = w.Write([]byte(`{"number":42,"result":"SUCCESS"}`))
			case "/job/webapp/42/execution/node/6/wfapi/describe/api/json":
				_, _ = w.Write([]byte(`{"id":"6","name":"Install","status":"SUCCESS"}`))
			default:
				http.NotFound(w, r)
			}
		}))
		defer srv.Close()

		ctx := buildLoadContext(srv)
		job := &gojenkins.Job{Jenkins: ctx.Jenkins, Base: "/job/webapp", Raw: &gojenkins.JobResponse{Name: "webapp", URL: srv.URL + "/job/webapp/"}}
		run := gojenkins.PipelineRun{
			ID: "42", Name: "#42", Job: job, Base: "/job/webapp/42",
			Status: "SUCCESS", StartTime: base, Duration: 2100,
			Stages: []gojenkins.PipelineNode{{ID: "6", Name: "Install", Status: "SUCCESS", StartTime: base, Duration: 2100}},
		}

		m1 := NewModel(ctx, run, 120, 40)
		defer func() { _ = m1.CleanUp() }()
		_, _ = m1.Update(m1.fetchBuild()())
		if m1.stageGraph.Source != stagegraph.SourceWFAPI {
			t.Fatalf("500 build source = %q, want wfapi", m1.stageGraph.Source)
		}

		m2 := NewModel(ctx, run, 120, 40)
		defer func() { _ = m2.CleanUp() }()
		_, _ = m2.Update(m2.fetchBuild()())
		if m2.stageGraph.Source != stagegraph.SourcePluginGraph {
			t.Fatalf("recovery build source = %q, want plugin-graph", m2.stageGraph.Source)
		}
	})
}

func TestBuildPageNavigationSkipsEmptyWrapperAndLoadsParallelBranchDetails(t *testing.T) {
	base := int64(1_700_000_000_000)
	var requestedNodes []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/job/webapp//5/api/json":
			_, _ = w.Write([]byte(`{"number":5,"result":"SUCCESS"}`))
		case strings.HasPrefix(r.URL.Path, "/job/webapp/5/execution/node/"):
			parts := strings.Split(r.URL.Path, "/")
			nodeID := parts[6]
			requestedNodes = append(requestedNodes, nodeID)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": nodeID, "name": "Stage-" + nodeID, "status": "SUCCESS",
				"stageFlowNodes": []map[string]any{{"id": "step-" + nodeID, "name": "sh step " + nodeID, "status": "SUCCESS"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := buildLoadContext(srv)
	job := &gojenkins.Job{Jenkins: ctx.Jenkins, Base: "/job/webapp", Raw: &gojenkins.JobResponse{Name: "webapp", URL: srv.URL + "/job/webapp/"}}
	run := gojenkins.PipelineRun{
		ID: "5", Name: "#5", Job: job, Base: "/job/webapp/5",
		Status: "SUCCESS", StartTime: base, EndTime: base + 12_900, Duration: 12_900,
		Stages: webappBuildStages(base, ""),
	}

	m := NewModel(ctx, run, 120, 40)
	defer func() { _ = m.CleanUp() }()
	_, _ = m.Update(m.fetchBuild()())

	// Expected navigable order skips empty wrapper Parallel Tests (ID 11):
	// Install (6) -> Unit (15) -> Integration (16) -> Lint (17) -> Bundle (30)
	wantIDs := []string{"6", "15", "16", "17", "30"}
	if got := m.focusedStageID(); got != wantIDs[0] {
		t.Fatalf("initial focused stage ID = %q, want %q", got, wantIDs[0])
	}

	for _, want := range wantIDs[1:] {
		_, cmd := m.Update(nextStageKey)
		if got := m.focusedStageID(); got != want {
			t.Fatalf("after NextStage focused ID = %q, want %q (wrapper 11 must be skipped)", got, want)
		}
		for _, msg := range messages(cmd) {
			if _, ok := msg.(NodeDownloadedMsg); ok {
				_, _ = m.Update(msg)
			}
		}
		if m.stageDetails.pipelineNode == nil || m.stageDetails.pipelineNode.ID != want {
			t.Fatalf("StageDetails node = %#v, want ID %q", m.stageDetails.pipelineNode, want)
		}
	}

	// PrevStage steps back from Bundle (30) directly to Lint (17).
	_, _ = m.Update(prevStageKey)
	if got := m.focusedStageID(); got != "17" {
		t.Fatalf("after PrevStage focused ID = %q, want 17", got)
	}
	for _, id := range requestedNodes {
		if id == "11" {
			t.Fatalf("wrapper stage 11 was unexpectedly fetched via GetNode: %v", requestedNodes)
		}
	}
}

func TestBuildPageTimelineNarrowWidthAndVerticalScrollHaveNoOverflow(t *testing.T) {
	base := int64(1_700_000_000_000)
	stages := make([]gojenkins.PipelineNode, 0, 22)
	stages = append(stages, gojenkins.PipelineNode{ID: "1", Name: "Setup", Status: "SUCCESS", StartTime: base, Duration: 1000})
	stages = append(stages, gojenkins.PipelineNode{ID: "2", Name: "Large Parallel Matrix Group Name", Status: "SUCCESS", StartTime: base + 1000, Duration: 100})
	for i := range 20 {
		stages = append(stages, gojenkins.PipelineNode{
			ID: fmt.Sprint(10 + i), Name: fmt.Sprintf("Matrix Branch With Very Long Name %02d", i+1),
			Status: "SUCCESS", StartTime: base + 1100 + int64(i)*5, Duration: int64(2000 + i*250),
		})
	}

	cfg := config.GetDefaultConfig()
	run := gojenkins.PipelineRun{
		ID: "9", Name: "#9", Status: "SUCCESS", StartTime: base, EndTime: base + 10_000, Duration: 10_000,
		Stages: stages,
	}
	for _, width := range []int{32, 50, 80, 120} {
		m := NewModel(&appcontext.ProgramContext{Config: cfg}, run, width, 30)
		m.loading = false
		m.build = &gojenkins.Build{Raw: &gojenkins.BuildResponse{}}
		m.rebuildStageGraph("", false)
		m.updateStageViewLayout()
		view := ansi.Strip(m.renderStages())
		for lineNo, line := range strings.Split(view, "\n") {
			if w := ansi.StringWidth(line); w > m.timelineContentWidth() {
				t.Fatalf("width=%d line %d overflowed content width %d (got %d): %q", width, lineNo, m.timelineContentWidth(), w, line)
			}
		}
		_ = m.CleanUp()
	}

	// Navigating down a 20-branch matrix scrolls stageView vertically.
	m := NewModel(&appcontext.ProgramContext{Config: cfg}, run, 120, 30)
	defer func() { _ = m.CleanUp() }()
	m.loading = false
	m.build = &gojenkins.Build{Raw: &gojenkins.BuildResponse{}}
	m.rebuildStageGraph("", false)
	m.updateStageViewLayout()
	for range 15 {
		_, _ = m.Update(nextStageKey)
	}
	if m.stageView.YOffset() == 0 {
		t.Fatal("navigating across a 20-branch parallel matrix did not scroll the timeline viewport")
	}
}

func TestBuildPageLivePollRefreshesGraphTreeAndCleanupCancelsInFlightRefresh(t *testing.T) {
	base := int64(1_700_000_000_000)
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/job/webapp/8/pipeline-overview/tree" {
			select {
			case <-started:
			default:
				close(started)
			}
			<-r.Context().Done()
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	ctx := buildLoadContext(srv)
	job := &gojenkins.Job{Jenkins: ctx.Jenkins, Base: "/job/webapp", Raw: &gojenkins.JobResponse{Name: "webapp", URL: srv.URL + "/job/webapp/"}}
	run := gojenkins.PipelineRun{
		ID: "8", Name: "#8", Job: job, Base: "/job/webapp/8",
		Status: "IN_PROGRESS", StartTime: base, Duration: 3000,
		Stages: []gojenkins.PipelineNode{{ID: "6", Name: "Install", Status: "IN_PROGRESS", StartTime: base, Duration: 3000}},
	}
	m := NewModel(ctx, run, 120, 40)
	m.loading = false
	m.build = &gojenkins.Build{Raw: &gojenkins.BuildResponse{}}
	m.rebuildStageGraph("", false)

	// Poll snapshot on a running build starts a cancellable graph tree refresh.
	cmd := m.ApplyPollSnapshot(poll.Snapshot{
		Target: poll.Target{Kind: poll.TargetExactRun, JobURL: job.Raw.URL, BuildNumber: 8},
		Runs: []poll.RunSnapshot{{
			BuildNumber: 8, Name: "#8", Status: buildstatus.StatusInProgress, Activity: buildstatus.ActivityRunning,
			StartTimeMillis: base, DurationMillis: 4000,
			Stages: []poll.StageSnapshot{
				{ID: "6", Name: "Install", Status: buildstatus.StatusSuccess, StartTimeMillis: base, DurationMillis: 2000},
				{ID: "15", Name: "Unit", Status: buildstatus.StatusInProgress, StartTimeMillis: base + 2000, DurationMillis: 2000},
				{ID: "16", Name: "Lint", Status: buildstatus.StatusInProgress, StartTimeMillis: base + 2010, DurationMillis: 1990},
			},
		}},
	})
	if cmd == nil {
		t.Fatal("ApplyPollSnapshot returned nil command")
	}

	// Verify live marker "now" is rendered on the running timeline.
	if plain := ansi.Strip(m.renderStages()); !strings.Contains(plain, "now") {
		t.Fatalf("running timeline missing 'now' marker:\n%s", plain)
	}

	// Trigger the graph refresh directly and verify CleanUp cancels it and drops stale messages.
	refreshCmd := m.refreshGraphTree()
	if refreshCmd == nil {
		t.Fatal("refreshGraphTree returned nil")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- refreshCmd() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("graph refresh request did not start")
	}
	gen := m.graphGeneration
	_ = m.CleanUp()

	select {
	case msg := <-done:
		fetched, ok := msg.(GraphTreeFetchedMsg)
		if !ok || fetched.Error == nil {
			t.Fatalf("cancelled refresh = %#v, want cancelled GraphTreeFetchedMsg", msg)
		}
		// Stale generation message must be dropped cleanly.
		if _, cmd := m.Update(GraphTreeFetchedMsg{
			model: m, Generation: gen,
			Tree: &stagegraph.PluginTree{Stages: []stagegraph.StageNode{{ID: "99", Name: "Stale"}}},
		}); cmd != nil || m.pluginTree != nil {
			t.Fatal("stale GraphTreeFetchedMsg mutated cleaned-up page")
		}
	case <-time.After(time.Second):
		t.Fatal("CleanUp did not cancel in-flight graph tree refresh")
	}
}

func TestBuildPageQueuedAndNoStageDataPlaceholdersUnchanged(t *testing.T) {
	cfg := config.GetDefaultConfig()
	styles.SwitchTheme(styles.Github)

	queued := NewModel(&appcontext.ProgramContext{Config: cfg}, gojenkins.PipelineRun{ID: "1", Status: "QUEUED"}, 100, 30)
	queued.loading = false
	if got := ansi.Strip(queued.renderBuildFlow()); !strings.Contains(got, "Build in queue...") {
		t.Fatalf("queued build flow = %q, want 'Build in queue...'", got)
	}

	noStages := NewModel(&appcontext.ProgramContext{Config: cfg}, gojenkins.PipelineRun{ID: "2", Status: "FAILED", EndTime: 1000}, 100, 30)
	noStages.loading = false
	got := ansi.Strip(noStages.renderBuildFlow())
	if !strings.Contains(got, "No stage data") || !strings.Contains(got, "check console log") {
		t.Fatalf("terminal zero-stage flow = %q, want No stage data placeholder", got)
	}
}

func TestBuildPageTimelineSeparatesParallelBarsAndIndentsIconsUnderTree(t *testing.T) {
	base := int64(1_700_000_000_000)
	cfg := config.GetDefaultConfig()
	run := gojenkins.PipelineRun{
		ID: "7", Name: "#7", Status: "SUCCESS", StartTime: base, EndTime: base + 12_000, Duration: 12_000,
		Stages: webappBuildStages(base, ""),
	}

	m := NewModel(&appcontext.ProgramContext{Config: cfg}, run, 120, 40)
	defer func() { _ = m.CleanUp() }()
	m.loading = false
	m.build = &gojenkins.Build{Raw: &gojenkins.BuildResponse{}}
	m.rebuildStageGraph("", false)
	m.updateStageViewLayout()

	plain := ansi.Strip(m.renderStages())
	if strings.Contains(plain, "█") {
		t.Fatalf("timeline should not use full-height block █ that merges adjacent parallel bars:\n%s", plain)
	}
	if !strings.Contains(plain, "▄") {
		t.Fatalf("timeline missing half-block bar ▄:\n%s", plain)
	}
	if !strings.Contains(plain, "┬") {
		t.Fatalf("timeline missing header divider tick marks:\n%s", plain)
	}
	for _, line := range strings.Split(plain, "\n") {
		if strings.Contains(line, "Unit") {
			if strings.Index(line, "├─") > strings.Index(line, styles.SuccessIcon) {
				t.Fatalf("parallel branch should place tree connector before status icon, got: %q", line)
			}
		}
	}

	// Verify rendered viewport never exceeds terminal width.
	for _, line := range strings.Split(ansi.Strip(m.stageView.View()), "\n") {
		if w := ansi.StringWidth(line); w > 120 {
			t.Fatalf("stageView line width %d exceeds terminal width 120: %q", w, line)
		}
	}
}
