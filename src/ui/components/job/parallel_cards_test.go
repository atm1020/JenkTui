package job

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

func parallelWebappRun(id string, status string, branchStatuses [3]string) gojenkins.PipelineRun {
	base := int64(1_700_000_000_000)
	end := base + 12_900
	if status == "IN_PROGRESS" {
		end = 0
	}
	return gojenkins.PipelineRun{
		ID:        id,
		Name:      "#" + id,
		Status:    status,
		StartTime: base,
		EndTime:   end,
		Duration:  12_900,
		Stages: []gojenkins.PipelineNode{
			{ID: "6", Name: "Install", Status: "SUCCESS", StartTime: base + 100, Duration: 2100},
			{ID: "11", Name: "Parallel Tests", Status: "SUCCESS", StartTime: base + 2250, Duration: 350},
			{ID: "15", Name: "Unit", Status: branchStatuses[0], StartTime: base + 2600, Duration: 4200},
			{ID: "16", Name: "Integration", Status: branchStatuses[1], StartTime: base + 2615, Duration: 6200},
			{ID: "17", Name: "Lint", Status: branchStatuses[2], StartTime: base + 2620, Duration: 3200},
			{ID: "30", Name: "Bundle", Status: "SUCCESS", StartTime: base + 8900, Duration: 3100},
		},
	}
}

func TestStageViewRendersParallelGroupCardWithFixedHeightAndNoSprawl(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	run := parallelWebappRun("5", "SUCCESS", [3]string{"SUCCESS", "SUCCESS", "SUCCESS"})
	svm := NewStageViewModel(140, 20, stageKeys())
	svm.SetPipelineRun([]gojenkins.PipelineRun{run})

	rendered := svm.renderRun(&run, 0, StageBoxWidth)
	if h := lipgloss.Height(rendered); h != StageBoxHeight {
		t.Fatalf("parallel run row height = %d, want fixed StageBoxHeight %d", h, StageBoxHeight)
	}

	plain := ansi.Strip(rendered)
	for _, want := range []string{"Install", "Parallel Tests", "├─", "└─", "Unit", "Integration", "Lint", "Bundle"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("parallel run row missing %q:\n%s", want, plain)
		}
	}

	// Collapsing 4 stages (wrapper + 3 branches) into one 30-col card must be
	// significantly narrower than rendering 6 separate 22-col boxes (132+ cols).
	if w := lipgloss.Width(rendered); w >= 24+6*StageBoxWidth {
		t.Fatalf("parallel row width = %d, expected compact Parallel Group Card (< %d)", w, 24+6*StageBoxWidth)
	}

	// Fast viewport windowing must still match direct rendering byte-for-byte.
	assertStageMatchesDirect(t, svm, 140, 20)
}

func TestParallelGroupCardOverflowPrioritizesUrgentBranchesAndKeepsFixedHeight(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	base := int64(1_700_000_000_000)
	run := gojenkins.PipelineRun{
		ID: "12", Name: "#12", Status: "IN_PROGRESS", StartTime: base, Duration: 8000,
		Stages: []gojenkins.PipelineNode{
			{ID: "1", Name: "Matrix", Status: "IN_PROGRESS", StartTime: base, Duration: 100},
			{ID: "2", Name: "OK-Alpha", Status: "SUCCESS", StartTime: base + 100, Duration: 2000},
			{ID: "3", Name: "OK-Beta", Status: "SUCCESS", StartTime: base + 105, Duration: 2000},
			{ID: "4", Name: "Paused-Br", Status: "PAUSED_PENDING_INPUT", StartTime: base + 110, Duration: 3000},
			{ID: "5", Name: "Run-Br", Status: "IN_PROGRESS", StartTime: base + 115, Duration: 4000},
			{ID: "6", Name: "OK-Gamma", Status: "SUCCESS", StartTime: base + 120, Duration: 2000},
			{ID: "7", Name: "Fail-Br", Status: "FAILED", StartTime: base + 125, Duration: 1500},
			{ID: "8", Name: "OK-Delta", Status: "SUCCESS", StartTime: base + 130, Duration: 2000},
		},
	}

	svm := NewStageViewModel(140, 20, stageKeys())
	svm.SetPipelineRun([]gojenkins.PipelineRun{run})
	rendered := svm.renderRun(&run, 0, StageBoxWidth)
	if h := lipgloss.Height(rendered); h != StageBoxHeight {
		t.Fatalf("overflow parallel card height = %d, want StageBoxHeight %d", h, StageBoxHeight)
	}

	plain := ansi.Strip(rendered)
	for _, want := range []string{"Fail-Br", "Run-Br", "Paused-Br", "+3 more"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("overflow card missing prioritized %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "OK-Delta") {
		t.Fatalf("low-priority overflow branch OK-Delta should have been hidden:\n%s", plain)
	}
	// Border color must reflect worst status (Failed), and card must not paint BgBase.
	tTheme := styles.GetTheme()
	wantTopBorder := lipgloss.NewStyle().Foreground(tTheme.StageFailedBorder).Render("╭" + strings.Repeat("─", parallelCardWidth(StageBoxWidth)-2) + "╮")
	if !strings.Contains(rendered, wantTopBorder) {
		t.Fatalf("parallel card with failed branch does not use StageFailedBorder color")
	}
	if strings.Contains(rendered, "48;2;13;17;23m") {
		t.Fatalf("parallel card painted BgBase instead of relying on OSC 11")
	}
}

func TestParallelGroupCardAnimatesRunningBranchAndInvalidatesOnStartTimeChange(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	runningRun := parallelWebappRun("10", "IN_PROGRESS", [3]string{"SUCCESS", "IN_PROGRESS", "SUCCESS"})
	staticRun := parallelWebappRun("9", "SUCCESS", [3]string{"SUCCESS", "SUCCESS", "SUCCESS"})

	svm := NewStageViewModel(140, 24, stageKeys())
	svm.pulseBar = animation.New(animation.Settings{Width: 14, FPS: 1000})
	svm.hasInProgress = true
	svm.SetPipelineRun([]gojenkins.PipelineRun{runningRun, staticRun})

	before := svm.View(140, 24)
	poisonStageRow(t, svm, "9", cacheSentinel)
	renders := svm.rowRenders

	// Advance spinner: only the running parallel row should rebuild, static row stays cached.
	oldSpinner := advanceStageSpinner(svm)
	after := svm.View(140, 24)
	if svm.rowRenders != renders+1 {
		t.Fatalf("spinner advance rebuilt %d rows, want 1", svm.rowRenders-renders)
	}
	if !strings.Contains(after, cacheSentinel) {
		t.Fatal("static parallel row was needlessly rebuilt on animation frame")
	}
	if after == before || !strings.Contains(after, svm.spinnerModel.View()) {
		t.Fatalf("running parallel card did not update spinner glyph (old=%q new=%q)", oldSpinner, svm.spinnerModel.View())
	}

	// Changing only stage StartTime (turning sequential stages into parallel)
	// must invalidate the row cache cleanly.
	base := int64(1_700_000_000_000)
	seqRun := gojenkins.PipelineRun{
		ID: "20", Name: "#20", Status: "SUCCESS", StartTime: base, EndTime: base + 8000, Duration: 8000,
		Stages: []gojenkins.PipelineNode{
			{ID: "1", Name: "Unit", Status: "SUCCESS", StartTime: base, Duration: 3000},
			{ID: "2", Name: "Lint", Status: "SUCCESS", StartTime: base + 3100, Duration: 3000},
		},
	}
	svm2 := NewStageViewModel(140, 20, stageKeys())
	svm2.SetPipelineRun([]gojenkins.PipelineRun{seqRun})
	firstView := ansi.Strip(svm2.View(140, 20))
	if strings.Contains(firstView, "├─") {
		t.Fatalf("sequential stages unexpectedly rendered as parallel card:\n%s", firstView)
	}
	poisonStageRow(t, svm2, "20", cacheSentinel)

	// Update StartTime of Lint so it overlaps Unit without changing name/status/duration.
	seqRun.Stages[1].StartTime = base + 50
	svm2.SetPipelineRun([]gojenkins.PipelineRun{seqRun})
	secondView := svm2.View(140, 20)
	if strings.Contains(secondView, cacheSentinel) {
		t.Fatal("row cache was not invalidated when stage StartTime changed")
	}
	if !strings.Contains(ansi.Strip(secondView), "├─") {
		t.Fatalf("updated overlapping stages did not re-render as Parallel Group Card:\n%s", ansi.Strip(secondView))
	}
}

func TestJobPageUsesOnlyWFAPIRunsAndNeverRequestsPluginGraph(t *testing.T) {
	base := int64(1_700_000_000_000)
	var graphRequests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "pipeline-overview") || strings.Contains(r.URL.Path, "pipeline-graph") {
			graphRequests.Add(1)
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/job/webapp/api/json":
			_, _ = fmt.Fprintf(w, `{"_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","name":"webapp","url":"http://%s/job/webapp/","color":"blue"}`, r.Host)
		case "/job/webapp/wfapi/runs/api/json":
			_, _ = fmt.Fprintf(w, `[{"id":"5","name":"#5","status":"SUCCESS","startTimeMillis":%d,"endTimeMillis":%d,"durationMillis":12900,"stages":[
				{"id":"6","name":"Install","status":"SUCCESS","startTimeMillis":%d,"durationMillis":2100},
				{"id":"11","name":"Parallel Tests","status":"SUCCESS","startTimeMillis":%d,"durationMillis":350},
				{"id":"15","name":"Unit","status":"SUCCESS","startTimeMillis":%d,"durationMillis":4200},
				{"id":"16","name":"Integration","status":"SUCCESS","startTimeMillis":%d,"durationMillis":6200},
				{"id":"17","name":"Lint","status":"SUCCESS","startTimeMillis":%d,"durationMillis":3200},
				{"id":"30","name":"Bundle","status":"SUCCESS","startTimeMillis":%d,"durationMillis":3100}
			]}]`, base, base+12900, base+100, base+2250, base+2600, base+2615, base+2620, base+8900)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	cfg := config.GetDefaultConfig()
	jenkins := gojenkins.CreateJenkins(srv.Client(), srv.URL)
	ctx := &appcontext.ProgramContext{Config: cfg, Jenkins: jenkins}
	m := NewModelFromUrl(ctx, 160, 40, srv.URL+"/job/webapp/")
	defer func() { _ = m.CleanUp() }()

	_, _ = m.Update(m.fetchJob()())
	view := ansi.Strip(m.View())
	if graphRequests.Load() != 0 {
		t.Fatalf("job page issued %d plugin-graph requests, want 0", graphRequests.Load())
	}
	for _, want := range []string{"Parallel Tests", "├─", "└─", "Unit", "Integration", "Lint"} {
		if !strings.Contains(view, want) {
			t.Fatalf("job page missing %q:\n%s", want, view)
		}
	}
}

func TestStageViewRendersLiveJenkinsWFAPIParallelQuirksAsRunningCard(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	base := int64(1_795_818_302_065)

	// Phase A: exact Jenkins wfapi wire shape while Unit, Integration, and Lint
	// are all running concurrently (negative durations on Integration and Lint).
	phaseA := gojenkins.PipelineRun{
		ID: "29", Name: "#29", Status: "IN_PROGRESS",
		StartTime: base, EndTime: base + 5604, Duration: 5604,
		Stages: []gojenkins.PipelineNode{
			{ID: "6", Name: "Install", Status: "SUCCESS", StartTime: base + 163, Duration: 2202},
			{ID: "12", Name: "Parallel Tests", Status: "SUCCESS", StartTime: base + 2383, Duration: 84},
			{ID: "20", Name: "Unit", Status: "IN_PROGRESS", StartTime: base + 2442, Duration: 3161},
			{ID: "22", Name: "Integration", Status: "SUCCESS", StartTime: base + 2455, Duration: -13},
			{ID: "24", Name: "Lint", Status: "SUCCESS", StartTime: base + 2467, Duration: -12},
		},
	}

	svm := NewStageViewModel(140, 20, stageKeys())
	svm.SetPipelineRun([]gojenkins.PipelineRun{phaseA})
	rendered := svm.renderRun(&phaseA, 0, StageBoxWidth)
	plain := ansi.Strip(rendered)

	for _, want := range []string{"Install", "Parallel Tests", "├─", "└─", "Unit", "Integration", "Lint", "3s"} {
		if !strings.Contains(plain, want) {
			t.Fatalf("live Phase A parallel run missing %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "0ms") {
		t.Fatalf("live Phase A parallel branches should show elapsed time instead of 0ms:\n%s", plain)
	}
	if w := lipgloss.Width(rendered); w >= 24+5*StageBoxWidth {
		t.Fatalf("live Phase A row width = %d, expected collapsed Parallel Group Card (< %d)", w, 24+5*StageBoxWidth)
	}

	// Phase C: Unit and Lint done, Integration still running while Jenkins reports
	// every stage as SUCCESS with Integration dur=12ms (end == Lint.start).
	phaseC := gojenkins.PipelineRun{
		ID: "29", Name: "#29", Status: "IN_PROGRESS",
		StartTime: base, EndTime: base + 8356, Duration: 8356,
		Stages: []gojenkins.PipelineNode{
			{ID: "6", Name: "Install", Status: "SUCCESS", StartTime: base + 163, Duration: 2202},
			{ID: "12", Name: "Parallel Tests", Status: "SUCCESS", StartTime: base + 2383, Duration: 72},
			{ID: "20", Name: "Unit", Status: "SUCCESS", StartTime: base + 2442, Duration: 4472},
			{ID: "22", Name: "Integration", Status: "SUCCESS", StartTime: base + 2455, Duration: 12},
			{ID: "24", Name: "Lint", Status: "SUCCESS", StartTime: base + 2467, Duration: 3597},
		},
	}

	svm.SetPipelineRun([]gojenkins.PipelineRun{phaseC})
	renderedC := svm.renderRun(&phaseC, 0, StageBoxWidth)
	plainC := ansi.Strip(renderedC)
	if strings.Contains(plainC, "12ms") {
		t.Fatalf("live Phase C Integration branch should show live elapsed time (~5s), not 12ms:\n%s", plainC)
	}
	tTheme := styles.GetTheme()
	wantRunningBorder := lipgloss.NewStyle().Foreground(tTheme.StageRunningBorder).Render("╭" + strings.Repeat("─", parallelCardWidth(StageBoxWidth)-2) + "╮")
	if !strings.Contains(renderedC, wantRunningBorder) {
		t.Fatalf("live Phase C parallel card should use StageRunningBorder while Integration is running")
	}
}
