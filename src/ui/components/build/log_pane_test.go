package build

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/components/logviewer"
	"github.com/atm1020/jenktui/src/ui/util"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

func drainLogViewerCmds(t *testing.T, m *Model, cmd tea.Cmd) (lastFooterURL string) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		if cur == nil {
			continue
		}
		for _, msg := range messages(cur) {
			switch typed := msg.(type) {
			case util.FooterUrlMsg:
				lastFooterURL = typed.Url
			case StepLogFetchedMsg, FullLogDownloadedMsg, ScopedLogExtractedMsg, logviewer.FileReadyMsg, logviewer.ChunkLoadedMsg, logviewer.SearchCompletedMsg:
				_, next := m.Update(msg)
				queue = append(queue, next)
			}
		}
	}
	return lastFooterURL
}

func TestInlineLogProgressiveHTMLJumpAndFourScopes(t *testing.T) {
	var htmlHits, stepLogHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/webapp//16/api/json":
			_, _ = w.Write([]byte(`{"number":16,"result":"FAILURE"}`))
		case "/job/webapp/16/logText/progressiveHtml":
			htmlHits.Add(1)
			_, _ = w.Write([]byte(sampleParallelProgressiveHTML))
		case "/job/webapp/16/execution/node/22/wfapi/describe/api/json":
			_, _ = w.Write([]byte(`{
				"id":"22","name":"Integration","status":"FAILED","execNode":"agent-2",
				"stageFlowNodes":[
					{"id":"28","name":"Shell Script","status":"SUCCESS","startTimeMillis":2000,"durationMillis":6000}
				]
			}`))
		case "/job/webapp/16/execution/node/20/wfapi/describe/api/json":
			_, _ = w.Write([]byte(`{
				"id":"20","name":"Unit","status":"SUCCESS","execNode":"agent-1",
				"stageFlowNodes":[
					{"id":"26","name":"Shell Script","status":"SUCCESS","startTimeMillis":2000,"durationMillis":4000}
				]
			}`))
		case "/job/webapp/16/execution/node/26/wfapi/log/api/json",
			"/job/webapp/16/execution/node/28/wfapi/log/api/json":
			stepLogHits.Add(1)
			_, _ = w.Write([]byte(`{"nodeId":"28","nodeStatus":"SUCCESS","text":"fallback\n"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := buildLoadContext(srv)
	ctx.DataLogsDir = t.TempDir()
	ctx.CacheDir = t.TempDir()
	base := int64(1_700_000_000_000)
	job := &gojenkins.Job{Jenkins: ctx.Jenkins, Base: "/job/webapp", Raw: &gojenkins.JobResponse{Name: "webapp", URL: srv.URL + "/job/webapp/"}}
	run := gojenkins.PipelineRun{
		ID: "16", Name: "#16", Job: job, Base: "/job/webapp/16", Status: "FAILED",
		StartTime: base, EndTime: base + 10_000, Duration: 10_000,
		Stages: []gojenkins.PipelineNode{
			{ID: "6", Name: "Install", Status: "SUCCESS", StartTime: base, Duration: 2000},
			{ID: "20", Name: "Unit", Status: "SUCCESS", StartTime: base + 2000, Duration: 4000},
			{ID: "22", Name: "Integration", Status: "FAILED", StartTime: base + 2010, Duration: 6000},
		},
	}

	m := NewModel(ctx, run, 160, 42)
	defer func() { _ = m.CleanUp() }()

	_, cmd := m.Update(m.fetchBuild()())
	drainLogViewerCmds(t, m, cmd)

	// Single progressiveHtml request powers full log + all scoped views.
	if htmlHits.Load() != 1 {
		t.Fatalf("progressiveHtml hits = %d, want 1", htmlHits.Load())
	}
	if stepLogHits.Load() != 0 {
		t.Fatalf("wfapi/log hits = %d, want 0 when progressiveHtml indexed steps", stepLogHits.Load())
	}
	// Auto-focused failed stage Integration (ID 22) -> step 28 -> jumped to line 15 in full log!
	if m.logPane.viewer == nil {
		t.Fatal("full log viewer did not load")
	}
	if got := m.logPane.viewer.CursorLine(); got != 15 {
		t.Fatalf("initial full-log cursor line = %d, want 15 (+ integration-test-1)", got)
	}

	// Switch to previous stage Unit (ID 20) via 'h' -> full log immediately jumps to Unit's line!
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'h', Text: "h"})
	for _, msg := range messages(cmd) {
		if _, ok := msg.(NodeDownloadedMsg); ok {
			_, next := m.Update(msg)
			drainLogViewerCmds(t, m, next)
		}
	}
	if got := m.logPane.viewer.CursorLine(); got != 13 {
		t.Fatalf("after switching to Unit, full-log cursor line = %d, want 13 (+ unit-test-1)", got)
	}

	// Press '2' (Stage scope): shows ONLY Unit lines, cleanly de-interleaved from Integration!
	_, cmd = m.Update(tea.KeyPressMsg{Code: '2', Text: "2"})
	drainLogViewerCmds(t, m, cmd)
	stageView := ansi.Strip(m.View())
	if !strings.Contains(stageView, "Stage: Unit") || !strings.Contains(stageView, "+ unit-test-1") || !strings.Contains(stageView, "+ unit-done") {
		t.Fatalf("stage scope missing Unit lines:\n%s", stageView)
	}
	if strings.Contains(stageView, "+ integration-test-1") || strings.Contains(stageView, "NoSuchMethodError") {
		t.Fatalf("stage scope leaked Integration or Post-stage lines:\n%s", stageView)
	}

	// Press '3' (Step scope): shows ONLY step 26 lines with ZERO extra HTTP calls!
	_, cmd = m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	drainLogViewerCmds(t, m, cmd)
	stepView := ansi.Strip(m.View())
	if !strings.Contains(stepView, "+ unit-test-2") || strings.Contains(stepView, "+ integration-test-1") {
		t.Fatalf("step scope did not isolate step 26:\n%s", stepView)
	}
	if stepLogHits.Load() != 0 {
		t.Fatalf("step scope issued %d wfapi/log calls, want 0 (served from progressiveHtml index)", stepLogHits.Load())
	}

	// Press '4' (Outside stages scope): shows Pre-stage setup AND Post-stage NoSuchMethodError!
	_, cmd = m.Update(tea.KeyPressMsg{Code: '4', Text: "4"})
	drainLogViewerCmds(t, m, cmd)
	outsideView := ansi.Strip(m.View())
	for _, want := range []string{"Outside Stages", "Pre-stage", "Started by user admin", "Post-stage", "NoSuchMethodError", "Finished: FAILURE"} {
		if !strings.Contains(outsideView, want) {
			t.Fatalf("outside-stage scope missing %q:\n%s", want, outsideView)
		}
	}
	if strings.Contains(outsideView, "+ unit-test-1") || strings.Contains(outsideView, "+ integration-test-1") {
		t.Fatalf("outside-stage scope leaked stage lines:\n%s", outsideView)
	}

	// Press 'L' switches back to Full Console log, focuses Log pane, and sets footer to console.
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	footerURL := drainLogViewerCmds(t, m, cmd)
	if !strings.HasSuffix(footerURL, "console") {
		t.Fatalf("footer URL = %q, want console suffix in full log mode", footerURL)
	}
	if m.focusPane != focusLog || m.logPane.mode != logModeFull {
		t.Fatalf("after L: focusPane=%v mode=%v, want focusLog/logModeFull", m.focusPane, m.logPane.mode)
	}
	if htmlHits.Load() != 1 {
		t.Fatalf("re-opening terminal full log re-fetched progressiveHtml (%d hits)", htmlHits.Load())
	}

	// Press 'z' to zoom log pane full-screen; 'esc' leaves zoom.
	_, _ = m.Update(tea.KeyPressMsg{Code: 'z', Text: "z"})
	if !m.logPane.zoomed {
		t.Fatal("z did not zoom log pane")
	}
	if strings.Contains(ansi.Strip(m.View()), "Build #16") {
		t.Fatal("zoomed log should hide build summary bar")
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.logPane.zoomed {
		t.Fatal("esc did not leave zoomed log mode")
	}
}

func TestInlineLogFallsBackToConsoleTextAndStepAPIWhenProgressiveHTMLMissing(t *testing.T) {
	var step1Hits, step2Hits, fullHits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/demo//42/api/json":
			_, _ = w.Write([]byte(`{"number":42,"result":"SUCCESS"}`))
		case "/job/demo/42/execution/node/10/wfapi/describe/api/json":
			_, _ = w.Write([]byte(`{
				"id":"10","name":"Build","status":"SUCCESS","execNode":"agent-1",
				"stageFlowNodes":[
					{"id":"101","name":"Checkout step","status":"SUCCESS","startTimeMillis":1000,"durationMillis":500},
					{"id":"102","name":"Compile step","status":"SUCCESS","startTimeMillis":1500,"durationMillis":900},
					{"id":"103","name":"Empty wrapper","status":"SUCCESS","startTimeMillis":2400,"durationMillis":10}
				]
			}`))
		case "/job/demo/42/execution/node/101/wfapi/log/api/json":
			step1Hits.Add(1)
			_, _ = w.Write([]byte(`{"nodeId":"101","nodeStatus":"SUCCESS","length":20,"hasMore":false,"text":"checkout line alpha\n"}`))
		case "/job/demo/42/execution/node/102/wfapi/log/api/json":
			step2Hits.Add(1)
			_, _ = w.Write([]byte(`{"nodeId":"102","nodeStatus":"SUCCESS","length":20,"hasMore":false,"text":"compile line beta\n"}`))
		case "/job/demo/42/execution/node/103/wfapi/log/api/json":
			_, _ = w.Write([]byte(`{"nodeId":"103","nodeStatus":"SUCCESS","length":0,"hasMore":false,"text":""}`))
		case "/job/demo/42/consoleText/":
			fullHits.Add(1)
			_, _ = w.Write([]byte("Started by user admin\n[Pipeline] stage\n[Pipeline] { (Build)\ncheckout line alpha\ncompile line beta\n[Pipeline] // stage\nFinished: SUCCESS\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := buildLoadContext(srv)
	ctx.DataLogsDir = t.TempDir()
	ctx.CacheDir = t.TempDir()
	job := &gojenkins.Job{Jenkins: ctx.Jenkins, Base: "/job/demo", Raw: &gojenkins.JobResponse{Name: "demo", URL: srv.URL + "/job/demo/"}}
	run := gojenkins.PipelineRun{
		ID: "42", Name: "#42", Job: job, Base: "/job/demo/42", Status: "SUCCESS",
		StartTime: 1000, EndTime: 3000, Duration: 2000,
		Stages: []gojenkins.PipelineNode{{ID: "10", Name: "Build", Status: "SUCCESS", StartTime: 1000, Duration: 2000}},
	}

	m := NewModel(ctx, run, 140, 40)
	defer func() { _ = m.CleanUp() }()

	_, cmd := m.Update(m.fetchBuild()())
	drainLogViewerCmds(t, m, cmd)

	if fullHits.Load() != 1 {
		t.Fatalf("consoleText fallback hits = %d, want 1", fullHits.Load())
	}

	// Switch to Step mode ('3'): falls back to wfapi/log and caches terminal steps.
	_, cmd = m.Update(tea.KeyPressMsg{Code: '3', Text: "3"})
	drainLogViewerCmds(t, m, cmd)
	if step1Hits.Load() != 1 {
		t.Fatalf("step1 hits = %d, want 1", step1Hits.Load())
	}
	if got := ansi.Strip(m.View()); !strings.Contains(got, "checkout line alpha") {
		t.Fatalf("step 1 fallback missing text:\n%s", got)
	}

	// Move to step 2 and back to step 1: step 1 served from cache.
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	drainLogViewerCmds(t, m, cmd)
	if step2Hits.Load() != 1 {
		t.Fatalf("step2 hits = %d, want 1", step2Hits.Load())
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'k', Text: "k"})
	drainLogViewerCmds(t, m, cmd)
	if step1Hits.Load() != 1 {
		t.Fatalf("step1 hits after revisit = %d, want 1 (cached)", step1Hits.Load())
	}

	// Empty step renders a calm inline placeholder instead of an error dialog.
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	drainLogViewerCmds(t, m, cmd)
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
	drainLogViewerCmds(t, m, cmd)
	if got := ansi.Strip(m.View()); !strings.Contains(got, "No log output for this step") {
		t.Fatalf("empty step missing calm placeholder:\n%s", got)
	}
}

func TestInlineLogFocusSwitchSearchCaptureAndNewModelForLog(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/demo/api/json":
			_, _ = fmt.Fprintf(w, `{"name":"demo","url":"http://%s/job/demo/"}`, r.Host)
		case "/job/demo/7/wfapi/describe/api/json":
			_, _ = w.Write([]byte(`{"id":"7","name":"#7","status":"SUCCESS","stages":[{"id":"6","name":"Test","status":"SUCCESS"}]}`))
		case "/job/demo//7/api/json":
			_, _ = w.Write([]byte(`{"number":7,"result":"SUCCESS"}`))
		case "/job/demo/7/execution/node/6/wfapi/describe/api/json":
			_, _ = w.Write([]byte(`{"id":"6","name":"Test","status":"SUCCESS","stageFlowNodes":[{"id":"61","name":"go test","status":"SUCCESS"}]}`))
		case "/job/demo/7/consoleText/":
			_, _ = w.Write([]byte("line one\nERROR: boom here\nline three\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx := buildLoadContext(srv)
	ctx.CacheDir = t.TempDir()
	ctx.DataLogsDir = t.TempDir()

	m := NewModelForLogFromURL(ctx, srv.URL+"/job/demo/", "7", 140, 40)
	defer func() { _ = m.CleanUp() }()

	if !m.logPane.zoomed || m.focusPane != focusLog || m.logPane.mode != logModeFull {
		t.Fatalf("NewModelForLogFromURL state = zoomed:%v focus:%v mode:%v", m.logPane.zoomed, m.focusPane, m.logPane.mode)
	}

	initCmd := m.Init()
	var buildMsg tea.Msg
	for _, msg := range messages(initCmd) {
		switch msg.(type) {
		case BuildFetchedMsg:
			buildMsg = msg
		case FullLogDownloadedMsg:
			_, next := m.Update(msg)
			drainLogViewerCmds(t, m, next)
		}
	}
	if buildMsg != nil {
		_, next := m.Update(buildMsg)
		drainLogViewerCmds(t, m, next)
	}

	if got := ansi.Strip(m.View()); !strings.Contains(got, "ERROR: boom here") {
		t.Fatalf("zoomed full log view missing console output:\n%s", got)
	}

	// Search inside inline log captures input until Enter or Esc.
	_, _ = m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.IsCapturingInput() {
		t.Fatal("IsCapturingInput() = false while log search box is focused")
	}
	for _, r := range "boom" {
		_, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	drainLogViewerCmds(t, m, cmd)
	if m.IsCapturingInput() {
		t.Fatal("IsCapturingInput() remained true after search completed with matches")
	}
	if got := ansi.Strip(m.View()); !strings.Contains(got, "1/1") {
		t.Fatalf("search match counter missing from embedded log header:\n%s", got)
	}

	// Tab unzooms and returns focus to the Build (Stages/Steps) pane.
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.logPane.zoomed || m.focusPane != focusBuild {
		t.Fatalf("Tab from zoomed log: zoomed=%v focus=%v, want false/focusBuild", m.logPane.zoomed, m.focusPane)
	}

	// ctrl+h / Tab toggles cleanly between Build and Log panes.
	_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.focusPane != focusLog {
		t.Fatal("Tab did not focus Log pane")
	}
	_, _ = m.Update(tea.KeyPressMsg{Code: 'h', Mod: tea.ModCtrl})
	if m.focusPane != focusBuild {
		t.Fatal("ctrl+h did not return focus to Build pane")
	}
}

func TestInlineLogCancelsOnCleanupAndRejectsStaleGeneration(t *testing.T) {
	started := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer srv.Close()

	ctx := buildLoadContext(srv)
	ctx.DataLogsDir = t.TempDir()
	job := &gojenkins.Job{Jenkins: ctx.Jenkins, Base: "/job/demo", Raw: &gojenkins.JobResponse{Name: "demo", URL: srv.URL + "/job/demo/"}}
	m := NewModel(ctx, gojenkins.PipelineRun{ID: "42", Job: job, Base: "/job/demo/42", Status: "SUCCESS"}, 120, 40)
	m.loading = false

	cmd := m.loadStepLog("Build", gojenkins.PipelineNode{ID: "101", Name: "sh", Status: "SUCCESS"}, true)
	gen := m.logPane.generation
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("step log fetch did not start")
	}

	_ = m.CleanUp()
	select {
	case msg := <-done:
		fetched, ok := msg.(StepLogFetchedMsg)
		if !ok || fetched.Error == nil {
			t.Fatalf("cancelled fetch result = %#v, want StepLogFetchedMsg error", msg)
		}
		if _, next := m.Update(StepLogFetchedMsg{model: m, Generation: gen, NodeID: "101", FilePath: "/tmp/stale.log"}); next != nil || m.logPane.viewer != nil {
			t.Fatal("stale StepLogFetchedMsg mutated cleaned-up log pane")
		}
	case <-time.After(time.Second):
		t.Fatal("CleanUp did not cancel in-flight step log fetch")
	}
}

func TestUnifiedBuildLayoutNeverOverflowsAcrossWidths(t *testing.T) {
	cfg := config.GetDefaultConfig()
	run := gojenkins.PipelineRun{
		ID: "42", Name: "#42", Status: "SUCCESS", StartTime: 1_700_000_000_000, Duration: 5000,
		Stages: []gojenkins.PipelineNode{
			{ID: "6", Name: "Checkout Repository With Long Title", Status: "SUCCESS", StartTime: 1_700_000_000_000, Duration: 2000},
			{ID: "7", Name: "Run Integration Test Suite", Status: "SUCCESS", StartTime: 1_700_000_002_000, Duration: 3000},
		},
	}
	for _, width := range []int{40, 60, 80, 120, 160} {
		m := NewModel(&appcontext.ProgramContext{Config: cfg}, run, width, 30)
		m.loading = false
		m.build = &gojenkins.Build{Raw: &gojenkins.BuildResponse{}}
		m.rebuildStageGraph("", false)
		_, _ = m.Update(NodeDownloadedMsg{
			model: m, Generation: m.nodeGeneration,
			PipelineNode: gojenkins.PipelineNode{
				ID: "6", Name: "Checkout Repository With Long Title", Status: "SUCCESS",
				StageFlowNodes: []gojenkins.PipelineNode{{ID: "61", Name: "git checkout very-long-branch-name", Status: "SUCCESS", Duration: 1500}},
			},
		})
		_, _ = m.Update(tea.WindowSizeMsg{Width: width, Height: 30})
		plain := ansi.Strip(m.View())
		for lineNo, line := range strings.Split(plain, "\n") {
			if w := ansi.StringWidth(line); w > width {
				t.Fatalf("width=%d line %d overflowed (%d > %d): %q", width, lineNo, w, width, line)
			}
		}
		_ = m.CleanUp()
	}
}

func TestDownloadFullConsoleLogHonorsCancelledContext(t *testing.T) {
	requestReachedServer := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestReachedServer <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	jenkins := gojenkins.CreateJenkins(nil, srv.URL)
	job := &gojenkins.Job{Jenkins: jenkins, Base: "/job/demo", Raw: &gojenkins.JobResponse{URL: srv.URL + "/job/demo/"}}
	run := &gojenkins.PipelineRun{ID: "42", Base: "/job/demo/42", Job: job}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := downloadFullConsoleLog(ctx, run, t.TempDir()); !errors.Is(err, context.Canceled) {
		t.Fatalf("downloadFullConsoleLog error = %v, want context.Canceled", err)
	}
	select {
	case <-requestReachedServer:
		t.Fatal("cancelled request reached the server")
	default:
	}
}
