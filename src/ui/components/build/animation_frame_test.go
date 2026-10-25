package build

import (
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/ui/animation"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

func TestBuildFrameAdvancesEverySpinnerAndSchedulesOneFrame(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	header := page.spinnerModel.View()
	step := page.stageDetails.spinnerModel.View()

	_, cmd := page.Update(page.clock.Frame())

	if page.spinnerModel.View() == header || page.stageDetails.spinnerModel.View() == step {
		t.Fatal("one frame did not advance the header and stage-details spinners together")
	}
	assertOneDelayedFrame(t, cmd)
}

func TestBuildPageSchedulesNoOtherAnimationTimers(t *testing.T) {
	page, start := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	if frames := countImmediateFrames(start); frames != 1 {
		t.Fatalf("loaded running build started %d frame chains, want 1", frames)
	}
	if cmd := page.stageDetails.handleNodeDownloaded(NodeDownloadedMsg{PipelineNode: *page.stageDetails.pipelineNode}); cmd != nil {
		t.Fatal("stage details scheduled its own timer for a downloaded node")
	}
	if _, cmd := page.Update(page.spinnerModel.Tick()); cmd != nil {
		t.Fatal("a spinner tick started an animation timer")
	}
}

func TestBuildLoadingClockContinuesIntoRunningBuild(t *testing.T) {
	page := NewModel(&appcontext.ProgramContext{Config: config.GetDefaultConfig()},
		gojenkins.PipelineRun{ID: "42", Status: "IN_PROGRESS"}, 100, 30)
	t.Cleanup(func() { _ = page.CleanUp() })
	page.entered = true
	if page.ensureAnimation() == nil || !page.clock.Running() {
		t.Fatal("loading page did not start its clock")
	}
	loading := page.clock.Frame()

	cmd := page.handleBuildFetchMsg(BuildFetchedMsg{
		model: page, PipelineRun: gojenkins.PipelineRun{ID: "42", Status: "IN_PROGRESS"},
		Build: gojenkins.Build{Raw: &gojenkins.BuildResponse{}},
	})

	if page.clock.Frame() != loading || !page.clock.Running() {
		t.Fatal("loaded running build replaced the loading clock instead of continuing it")
	}
	if frames := countImmediateFrames(cmd); frames != 0 {
		t.Fatalf("loaded running build started %d extra frame chains", frames)
	}
}

// A paused build is static. Resuming must start one new chain that the
// paused chain's pending frame cannot join (#108).
func TestBuildPauseStopsAndResumeRestartsOneChain(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	paused := page.clock.Frame()

	_ = page.ApplyPollSnapshot(exactRunSnapshot(page, buildstatus.StatusPausedPendingInput, buildstatus.ActivityPaused))
	if page.clock.Running() {
		t.Fatal("paused build kept its clock running")
	}
	assertFrameIgnored(t, page, paused)

	resume := page.ApplyPollSnapshot(exactRunSnapshot(page, buildstatus.StatusInProgress, buildstatus.ActivityRunning))
	if !page.clock.Running() || page.clock.Frame() == paused {
		t.Fatal("resumed build did not start a new clock run")
	}
	if frames := countImmediateFrames(resume); frames != 1 {
		t.Fatalf("resume started %d frame chains, want 1", frames)
	}
	assertFrameIgnored(t, page, paused)
	if _, cmd := page.Update(page.clock.Frame()); cmd == nil {
		t.Fatal("resumed frame did not continue its chain")
	}
	if again := page.ApplyPollSnapshot(exactRunSnapshot(page, buildstatus.StatusInProgress, buildstatus.ActivityRunning)); countImmediateFrames(again) != 0 {
		t.Fatal("a repeated running observation started another chain")
	}
}

func TestBuildFinishStopsTheClock(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	running := page.clock.Frame()

	_ = page.ApplyPollSnapshot(exactRunSnapshot(page, buildstatus.StatusSuccess, buildstatus.ActivityFinished))

	if page.clock.Running() {
		t.Fatal("finished build kept its clock running")
	}
	assertFrameIgnored(t, page, running)
}

func TestBuildCleanupStopsAndReentryRestartsOneChain(t *testing.T) {
	page, _ := newAnimationPage()
	stale := page.clock.Frame()

	_ = page.CleanUp()
	if page.clock.Running() {
		t.Fatal("CleanUp left the clock running")
	}
	assertFrameIgnored(t, page, stale)

	first := page.Init()
	t.Cleanup(func() { _ = page.CleanUp() })
	if frames := countImmediateFrames(first); frames != 1 {
		t.Fatalf("re-entry started %d frame chains, want 1", frames)
	}
	current := page.clock.Frame()
	if second := page.Init(); countImmediateFrames(second) != 0 || page.clock.Frame() != current {
		t.Fatal("a second Init started another chain")
	}
	assertFrameIgnored(t, page, stale)
}

func TestStaleBuildFrameDoesNotUpdateANewPage(t *testing.T) {
	first, _ := newAnimationPage()
	second, _ := newAnimationPage()
	t.Cleanup(func() {
		_ = first.CleanUp()
		_ = second.CleanUp()
	})

	assertFrameIgnored(t, second, first.clock.Frame())
	if _, cmd := second.Update(second.clock.Frame()); cmd == nil {
		t.Fatal("current page frame did not continue")
	}
}

func exactRunSnapshot(page *Model, status buildstatus.PipelineStatus, activity buildstatus.Activity) poll.Snapshot {
	stages := make([]poll.StageSnapshot, len(page.pipelineRun.Stages))
	for i, stage := range page.pipelineRun.Stages {
		stages[i] = poll.StageSnapshot{ID: stage.ID, Name: stage.Name, Status: buildstatus.PipelineStatus(stage.Status)}
	}
	stages[1].Status = status
	run := poll.RunSnapshot{BuildNumber: 42, Name: "#42", Status: status, Activity: activity, Stages: stages}
	if activity == buildstatus.ActivityFinished {
		run.EndTimeMillis = page.pipelineRun.StartTime + 200_000
	}
	return poll.Snapshot{
		Target: poll.Target{Kind: poll.TargetExactRun, JobURL: page.pipelineRun.Job.GetDetails().URL, BuildNumber: 42},
		Runs:   []poll.RunSnapshot{run},
	}
}

func assertFrameIgnored(t *testing.T, page *Model, frame animation.FrameMsg) {
	t.Helper()
	header := page.spinnerModel.View()
	step := page.stageDetails.spinnerModel.View()
	if _, cmd := page.Update(frame); cmd != nil {
		t.Fatal("ignored frame scheduled another frame")
	}
	if page.spinnerModel.View() != header || page.stageDetails.spinnerModel.View() != step {
		t.Fatal("ignored frame advanced a spinner")
	}
}

func assertOneDelayedFrame(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		t.Fatal("frame did not schedule a follow-up")
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	select {
	case msg := <-done:
		t.Fatalf("frame scheduled immediate %T; want one delayed frame", msg)
	case <-time.After(20 * time.Millisecond):
	}
}

// countImmediateFrames runs cmd's non-blocking leaves and counts the frame
// clocks they start. Started clocks deliver their first frame immediately;
// leaves that block (network, timers) are not frame starts.
func countImmediateFrames(cmd tea.Cmd) int {
	if cmd == nil {
		return 0
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-done:
	case <-time.After(20 * time.Millisecond):
		return 0
	}
	switch msg := msg.(type) {
	case animation.FrameMsg:
		return 1
	case tea.BatchMsg:
		n := 0
		for _, c := range msg {
			n += countImmediateFrames(c)
		}
		return n
	}
	return 0
}
