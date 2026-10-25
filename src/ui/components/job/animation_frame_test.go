package job

import (
	"strings"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/animation"

	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

func TestAnimationFrameAdvancesHeaderStageAndPulseOnce(t *testing.T) {
	page, start := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	if start == nil || !page.clock.Running() {
		t.Fatal("active job did not start one frame scheduler")
	}
	if _, ok := start().(animation.FrameMsg); !ok {
		t.Fatal("active job did not schedule one animation frame")
	}

	header := page.spinnerModel.View()
	stage := page.stageView.spinnerModel.View()
	pulse := page.stageView.pulseBar.View()
	before := page.View()
	views := 0
	_, cmd := page.Update(page.clock.Frame())
	views++
	after := page.View()
	if views != 1 {
		t.Fatalf("render passes = %d, want 1", views)
	}
	if after == before {
		t.Fatal("animation frame did not change the page")
	}
	if page.spinnerModel.View() == header || page.stageView.spinnerModel.View() == stage || page.stageView.pulseBar.View() == pulse {
		t.Fatal("animation frame did not advance header, stage, and pulse together")
	}
	if !strings.Contains(after, page.spinnerModel.View()) ||
		!strings.Contains(after, page.stageView.spinnerModel.View()) ||
		!strings.Contains(after, page.stageView.pulseBar.View()) {
		t.Fatal("animation frame did not render the advanced header, stage, and pulse")
	}
	if page.activeView != StageView || page.loading {
		t.Fatal("animation frame changed focus or loading state")
	}
	assertSingleDelayedFrame(t, cmd)
}

func TestAnimationFramesDoNotScheduleDuplicateRenders(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	page.clock = animation.NewFrameClock(time.Hour)
	_ = page.clock.Start()

	const frames = 5
	for range frames {
		_, cmd := page.Update(page.clock.Frame())
		_ = page.View()
		assertSingleDelayedFrame(t, cmd)
	}
}

func TestAnimationFramePreservesBuildListFocus(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	_, _ = page.Update(tea.KeyPressMsg{Code: 'B', Text: "B"})
	if page.activeView != JobListView {
		t.Fatal("build list did not take focus")
	}

	header := page.spinnerModel.View()
	stage := page.stageView.spinnerModel.View()
	pulse := page.stageView.pulseBar.View()
	_, cmd := page.Update(page.clock.Frame())
	if page.activeView != JobListView {
		t.Fatal("animation frame changed focus")
	}
	if cmd == nil || page.spinnerModel.View() == header || page.stageView.spinnerModel.View() == stage || page.stageView.pulseBar.View() == pulse {
		t.Fatal("animation frame stopped while the build list was focused")
	}
	_, _ = page.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	if page.activeView != StageView {
		t.Fatal("stages view did not regain focus")
	}
}

func TestSpinnerTickDoesNotStartABuildAnimationChain(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	header := page.spinnerModel.View()
	tick, ok := page.spinnerModel.Tick().(spinner.TickMsg)
	if !ok {
		t.Fatal("header spinner did not produce spinner.TickMsg")
	}
	if _, cmd := page.Update(tick); cmd != nil || page.spinnerModel.View() != header {
		t.Fatal("spinner tick advanced the build animation or started a chain")
	}
	if _, cmd := page.Update(page.clock.Frame()); cmd == nil {
		t.Fatal("frame scheduler stopped after a spinner tick")
	}
}

func TestInactiveJobDoesNotScheduleAnimation(t *testing.T) {
	cfg := config.GetDefaultConfig()
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins.invalid")
	job := &gojenkins.Job{
		Jenkins: jenkins,
		Raw:     &gojenkins.JobResponse{Name: "demo", URL: "http://jenkins.invalid/job/demo/"},
	}
	page := NewModel(&context.ProgramContext{Config: cfg, Jenkins: jenkins}, 100, 30, job)
	page.entered = true
	t.Cleanup(func() { _ = page.CleanUp() })

	_, cmd := page.Update(JobLoadedMsg{
		Job: job, PipelineRuns: []gojenkins.PipelineRun{*fakePipelineRun("1", 2, "SUCCESS")},
	})
	if cmd != nil || page.clock.Running() || page.animating {
		t.Fatal("inactive job scheduled animation")
	}
	assertFrameDoesNotAdvance(t, page)
}

func TestPausedCompletedAndCleanupStopAnimationScheduling(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	running := clonePipelineRuns(page.stageView.pipelineRuns)
	paused := page.clock.Frame()

	if cmd := page.applySnapshotData(withRunStatus(page.stageView.pipelineRuns, "PAUSED_PENDING_INPUT"), true); cmd != nil || page.animating {
		t.Fatal("paused job kept the frame scheduler alive")
	}
	assertFrameDoesNotAdvance(t, page)
	spinnerGlyph := strings.TrimSpace(ansi.Strip(page.spinnerModel.View()))
	if view := page.View(); spinnerGlyph != "" && strings.Contains(ansi.Strip(view), spinnerGlyph) {
		t.Fatal("paused job still shows the header spinner")
	}
	if _, cmd := page.Update(paused); cmd != nil {
		t.Fatal("stale paused frame continued scheduling")
	}

	resume := page.applySnapshotData(running, true)
	if resume == nil || !page.clock.Running() || page.clock.Frame() == paused {
		t.Fatal("running observation did not restart one new frame scheduler")
	}
	if _, cmd := page.Update(paused); cmd != nil {
		t.Fatal("stale paused frame updated the resumed page")
	}
	resumed, ok := resume().(animation.FrameMsg)
	if !ok {
		t.Fatal("resume did not schedule one animation frame")
	}
	if _, cmd := page.Update(resumed); cmd == nil {
		t.Fatal("resumed frame did not continue the new chain")
	}

	if cmd := page.applySnapshotData(finishedAnimationRuns(page), false); cmd != nil || page.animating || page.clock.Running() {
		t.Fatal("completed job kept scheduling animation")
	}
	assertFrameDoesNotAdvance(t, page)

	page, _ = newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	stale := page.clock.Frame()
	_ = page.CleanUp()
	assertFrameDoesNotAdvance(t, page)
	if _, cmd := page.Update(stale); cmd != nil || page.clock.Running() {
		t.Fatal("navigated-away frame continued scheduling")
	}
}

func TestReentryDoesNotDuplicateAnimationSchedulers(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	stale := page.clock.Frame()
	running := clonePipelineRuns(page.stageView.pipelineRuns)

	_ = page.CleanUp()
	_ = page.Init()
	if !page.clock.Running() || page.clock.Frame() == stale {
		t.Fatal("re-entry did not start a new run for the loading spinner")
	}
	if _, cmd := page.Update(stale); cmd != nil {
		t.Fatal("stale frame updated the re-entered page")
	}
	loading := page.clock.Frame()
	if _ = page.Init(); page.clock.Frame() != loading {
		t.Fatal("a second Init started another frame scheduler")
	}

	loaded := JobLoadedMsg{
		Generation: page.requestGeneration, Job: page.job, PipelineRuns: running, HasInProgressBuild: true,
	}
	_, first := page.Update(loaded)
	if first != nil || !page.clock.Running() || page.clock.Frame() != loading {
		t.Fatal("loaded page replaced the loading clock instead of continuing it")
	}
	_, second := page.Update(loaded)
	if second != nil || page.clock.Frame() != loading {
		t.Fatal("repeated load started another frame scheduler")
	}
	if cmd := page.applySnapshotData(running, true); cmd != nil || page.clock.Frame() != loading {
		t.Fatal("repeated running observation started another frame scheduler")
	}
	if _, cmd := page.Update(stale); cmd != nil {
		t.Fatal("stale pre-entry frame updated the new scheduler")
	}
	if _, cmd := page.Update(page.clock.Frame()); cmd == nil {
		t.Fatal("re-entered page did not continue its own frame")
	}
}

func TestLeftPageIgnoresLateStateChanges(t *testing.T) {
	page, _ := newAnimationPage()
	running := clonePipelineRuns(page.stageView.pipelineRuns)
	_ = page.CleanUp()

	_, cmd := page.Update(JobLoadedMsg{
		Generation: page.requestGeneration, Job: page.job, PipelineRuns: running, HasInProgressBuild: true,
	})
	if cmd != nil || page.clock.Running() || page.animating {
		t.Fatal("a late load restarted the clock on a page the router left")
	}
}

func TestStaleAnimationFrameDoesNotUpdateANewPage(t *testing.T) {
	first, _ := newAnimationPage()
	second, _ := newAnimationPage()
	t.Cleanup(func() {
		_ = first.CleanUp()
		_ = second.CleanUp()
	})
	stale := first.clock.Frame()
	header := second.spinnerModel.View()
	stage := second.stageView.spinnerModel.View()
	pulse := second.stageView.pulseBar.View()

	_, cmd := second.Update(stale)
	if cmd != nil {
		t.Fatal("stale frame from the previous page scheduled work")
	}
	if second.spinnerModel.View() != header || second.stageView.spinnerModel.View() != stage || second.stageView.pulseBar.View() != pulse {
		t.Fatal("stale frame updated the newly entered page")
	}
	if _, cmd := second.Update(second.clock.Frame()); cmd == nil {
		t.Fatal("current page frame did not continue")
	}
}

func assertFrameDoesNotAdvance(t *testing.T, page *Model) {
	t.Helper()
	header := page.spinnerModel.View()
	stage := page.stageView.spinnerModel.View()
	pulse := page.stageView.pulseBar.View()
	_, cmd := page.Update(page.clock.Frame())
	if cmd != nil {
		t.Fatal("animation frame scheduled another frame")
	}
	if page.spinnerModel.View() != header || page.stageView.spinnerModel.View() != stage || page.stageView.pulseBar.View() != pulse {
		t.Fatal("animation frame advanced a stopped page")
	}
}

func assertSingleDelayedFrame(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("animation frame did not schedule exactly one follow-up")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		t.Fatalf("animation frame scheduled immediate %T; want one delayed frame", msg)
	case <-time.After(20 * time.Millisecond):
	}
}

func clonePipelineRuns(runs []*gojenkins.PipelineRun) []gojenkins.PipelineRun {
	cloned := make([]gojenkins.PipelineRun, 0, len(runs))
	for _, run := range runs {
		if run == nil {
			continue
		}
		copyRun := *run
		copyRun.Stages = append([]gojenkins.PipelineNode(nil), run.Stages...)
		cloned = append(cloned, copyRun)
	}
	return cloned
}

func withRunStatus(runs []*gojenkins.PipelineRun, status string) []gojenkins.PipelineRun {
	cloned := clonePipelineRuns(runs)
	for i := range cloned {
		cloned[i].Status = status
		cloned[i].EndTime = 0
		for j := range cloned[i].Stages {
			cloned[i].Stages[j].Status = status
		}
	}
	return cloned
}
