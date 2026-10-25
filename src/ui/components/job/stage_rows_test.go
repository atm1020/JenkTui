package job

import (
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/styles"

	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"

	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func stageKeys() StageViewKeyMap {
	return StageViewKeyMap{CommonKeyMap: GetCommonKeyMap()}
}

func poisonStageRow(t *testing.T, svm *StageViewModel, id, sentinel string) {
	t.Helper()
	entry := svm.stageRenderCache[id]
	if entry == nil || !entry.ready {
		t.Fatalf("row %s is not cached", id)
	}
	entry.Content = sentinel + "\n"
	entry.lines = []string{sentinel, ""}
	entry.widths = []int{ansi.StringWidth(sentinel), 0}
}

func advanceStageSpinner(svm *StageViewModel) string {
	before := svm.spinnerModel.View()
	svm.hasInProgress = true
	svm.spinnerModel, _ = svm.spinnerModel.Update(spinner.TickMsg{ID: svm.spinnerModel.ID()})
	return before
}

func advanceStagePulse(t *testing.T, svm *StageViewModel) string {
	t.Helper()
	svm.hasInProgress = true
	before := svm.pulseBar.View()
	msg := svm.pulseBar.Init()()
	if svm.pulseBar.Update(msg) == nil {
		t.Fatal("pulse step was rejected")
	}
	if svm.pulseBar.View() == before {
		t.Fatal("pulse frame did not change")
	}
	return before
}

func directStageView(svm *StageViewModel, width, height int) string {
	boxWidth := styles.StageBoxContentWidth(width)
	vp := viewport.New()
	vp.Style = svm.viewport.Style
	vp.SetWidth(width)
	vp.SetHeight(height)
	if len(svm.pipelineRuns) == 0 {
		vp.SetContent(lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, "No pipeline runs found"))
		return vp.View()
	}
	var b strings.Builder
	for idx, run := range svm.pipelineRuns {
		b.WriteString(svm.renderRun(run, idx, boxWidth))
		b.WriteByte('\n')
	}
	vp.SetContent(b.String())
	vp.SetYOffset(svm.viewport.YOffset())
	vp.SetXOffset(svm.viewport.XOffset())
	return vp.View()
}

func assertStageMatchesDirect(t *testing.T, svm *StageViewModel, width, height int) {
	t.Helper()
	got := svm.View(width, height)
	want := directStageView(svm, width, height)
	if got != want {
		t.Fatalf("stage view diverged from viewport render: got %dx%d want %dx%d",
			lipgloss.Width(got), lipgloss.Height(got), lipgloss.Width(want), lipgloss.Height(want))
	}
	if width > 0 && height > 0 && (lipgloss.Width(got) != width || lipgloss.Height(got) != height) {
		t.Fatalf("stage view dimensions = %dx%d, want %dx%d", lipgloss.Width(got), lipgloss.Height(got), width, height)
	}
}

func TestStageViewMatchesViewportAcrossNavigation(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	t.Cleanup(func() { styles.SwitchTheme(styles.Github) })

	runs := make([]gojenkins.PipelineRun, 8)
	for i := range runs {
		run := fakePipelineRun(string(rune('1'+i)), 4, "SUCCESS")
		run.ID = string(rune('a' + i))
		runs[i] = *run
	}
	runs[0].Status = "IN_PROGRESS"
	runs[0].Stages[1].Status = "IN_PROGRESS"
	svm := NewStageViewModel(80, 16, stageKeys())
	svm.pulseBar = animation.New(animation.Settings{Width: 14, FPS: 1000})
	svm.hasInProgress = true
	svm.SetPipelineRun(runs)

	assertStageMatchesDirect(t, svm, 80, 16)
	advanceStageSpinner(svm)
	assertStageMatchesDirect(t, svm, 80, 16)
	advanceStagePulse(t, svm)
	assertStageMatchesDirect(t, svm, 80, 16)

	_, _ = svm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	assertStageMatchesDirect(t, svm, 80, 16)
	_, _ = svm.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	assertStageMatchesDirect(t, svm, 80, 16)
	_, _ = svm.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	assertStageMatchesDirect(t, svm, 40, 12)
	_, _ = svm.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	assertStageMatchesDirect(t, svm, 100, 20)

	finished := append([]gojenkins.PipelineRun(nil), runs...)
	finished[0].Status = "SUCCESS"
	finished[0].Stages[1].Status = "SUCCESS"
	svm.SetPipelineRun(finished)
	assertStageMatchesDirect(t, svm, 100, 20)

	reordered := []gojenkins.PipelineRun{finished[2], finished[0], finished[1]}
	svm.SetPipelineRun(reordered)
	assertStageMatchesDirect(t, svm, 100, 20)

	svm.SetBorderStyle(styles.GetTheme().S().DefaultBorder)
	assertStageMatchesDirect(t, svm, 100, 20)
}

func TestStaticRowsReusedAcrossAnimationFrames(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	svm := newCacheTestModel(
		fakePipelineRun("1", 3, "IN_PROGRESS"),
		fakePipelineRun("2", 3, "SUCCESS"),
	)
	svm.pulseBar = animation.New(animation.Settings{Width: 14, FPS: 1000})
	svm.hasInProgress = true
	svm.View(120, 40)
	poisonStageRow(t, svm, "2", cacheSentinel)
	renders := svm.rowRenders

	oldSpinner := advanceStageSpinner(svm)
	oldPulse := advanceStagePulse(t, svm)
	view := svm.View(120, 40)

	if !strings.Contains(view, cacheSentinel) {
		t.Fatal("static row was rebuilt for an animation frame")
	}
	if svm.rowRenders != renders+1 {
		t.Fatalf("animation rebuilt %d rows, want 1", svm.rowRenders-renders)
	}
	if strings.Contains(view, oldSpinner) || strings.Contains(view, oldPulse) {
		t.Fatal("animation frame reused a stale spinner or pulse")
	}
	if !strings.Contains(view, svm.spinnerModel.View()) || !strings.Contains(view, svm.pulseBar.View()) {
		t.Fatal("animation frame is missing the current spinner or pulse")
	}
}

func TestSelectionAndRunningRowsInvalidateIndependently(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	svm := NewStageViewModel(120, 40, stageKeys())
	svm.pulseBar = animation.New(animation.Settings{Width: 14, FPS: 1000})
	svm.hasInProgress = true
	svm.SetPipelineRun([]gojenkins.PipelineRun{
		*fakePipelineRun("1", 3, "IN_PROGRESS"),
		*fakePipelineRun("2", 3, "SUCCESS"),
		*fakePipelineRun("3", 3, "SUCCESS"),
	})
	svm.View(120, 40)
	poisonStageRow(t, svm, "1", "ROW1")
	poisonStageRow(t, svm, "2", "ROW2")
	poisonStageRow(t, svm, "3", "ROW3")
	renders := svm.rowRenders

	_, _ = svm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	view := svm.View(120, 40)
	if svm.rowRenders != renders+2 {
		t.Fatalf("selection rebuilt %d rows, want the selected and previously selected rows", svm.rowRenders-renders)
	}
	if strings.Contains(view, "ROW1") || strings.Contains(view, "ROW2") {
		t.Fatal("selection change reused a stale selected row")
	}
	if !strings.Contains(view, "ROW3") {
		t.Fatal("selection change rebuilt an unaffected row")
	}
	if svm.GetCurrentSelectedRun().ID != "2" {
		t.Fatalf("selected run = %s, want 2", svm.GetCurrentSelectedRun().ID)
	}

	poisonStageRow(t, svm, "1", "RUNNING")
	renders = svm.rowRenders
	advanceStageSpinner(svm)
	view = svm.View(120, 40)
	if strings.Contains(view, "RUNNING") {
		t.Fatal("spinner advance reused the unselected running row")
	}
	if svm.rowRenders != renders+1 {
		t.Fatalf("spinner advance rebuilt %d rows, want 1", svm.rowRenders-renders)
	}
	if !strings.Contains(view, "ROW3") {
		t.Fatal("spinner advance rebuilt a static row")
	}
}

func TestScrollResizePollAndReorderInvalidateAffectedRows(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	t.Cleanup(func() { styles.SwitchTheme(styles.Github) })

	runs := make([]gojenkins.PipelineRun, 6)
	for i := range runs {
		runs[i] = *fakePipelineRun(string(rune('1'+i)), 3, "SUCCESS")
		runs[i].ID = string(rune('1' + i))
	}
	svm := NewStageViewModel(80, 10, stageKeys())
	svm.SetPipelineRun(runs)
	_ = svm.View(80, 8)
	renders := svm.rowRenders
	poisonStageRow(t, svm, "2", "ROW2")
	hidden := svm.View(80, 8)
	if strings.Contains(hidden, "ROW2") {
		t.Fatal("row 2 was visible before scrolling")
	}
	if svm.rowRenders != renders {
		t.Fatal("poisoned off-screen row was rebuilt")
	}

	_, _ = svm.Update(tea.KeyPressMsg{Code: tea.KeyPgDown})
	scrolled := svm.View(80, 8)
	if scrolled == hidden {
		t.Fatal("scrolling did not change the visible stage window")
	}
	if svm.rowRenders != renders {
		t.Fatal("scrolling rebuilt stage rows")
	}
	if !strings.Contains(scrolled, "ROW2") {
		t.Fatal("scrolling did not reveal the cached row that entered the window")
	}
	if svm.viewport.YOffset() == 0 {
		t.Fatal("page down did not move the stage viewport")
	}

	heightOnly := svm.View(80, 8)
	if lipgloss.Height(heightOnly) != 8 || lipgloss.Width(heightOnly) != 80 {
		t.Fatalf("resized stage view = %dx%d, want 80x8", lipgloss.Width(heightOnly), lipgloss.Height(heightOnly))
	}
	if svm.rowRenders != renders {
		t.Fatal("height-only resize rebuilt stage rows")
	}

	poisonStageRow(t, svm, "1", "KEEP")
	poisonStageRow(t, svm, "2", "STALE")
	updated := append([]gojenkins.PipelineRun(nil), runs...)
	updated[1].Status = "FAILED"
	updated[1].Stages[0].Status = "FAILED"
	svm.SetPipelineRun(updated)
	svm.viewport.SetYOffset(0)
	polled := svm.View(80, 20)
	if !strings.Contains(polled, "KEEP") {
		t.Fatal("polling rebuilt a row whose visible data did not change")
	}
	if strings.Contains(polled, "STALE") {
		t.Fatal("polling reused a row whose status changed")
	}
	if !strings.Contains(ansi.Strip(polled), "Failed") {
		t.Fatal("polling did not render the updated stage status")
	}

	_, _ = svm.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	svm.View(80, 10)
	if svm.GetCurrentSelectedRun().ID != "2" {
		t.Fatalf("selected run = %s, want 2 before reorder", svm.GetCurrentSelectedRun().ID)
	}
	poisonStageRow(t, svm, "1", "ORDER1")
	poisonStageRow(t, svm, "3", "ORDER3")
	reordered := []gojenkins.PipelineRun{updated[2], updated[0], updated[1]}
	svm.SetPipelineRun(reordered)
	svm.viewport.SetYOffset(0)
	ordered := svm.View(80, 20)
	if svm.GetCurrentSelectedRun().ID != "2" {
		t.Fatalf("selected run = %s, want 2 after reorder", svm.GetCurrentSelectedRun().ID)
	}
	first, second := strings.Index(ordered, "ORDER3"), strings.Index(ordered, "ORDER1")
	if first < 0 || second < 0 || first > second {
		t.Fatal("reordering did not preserve cached rows in the new order")
	}

	styles.SwitchTheme(styles.Dracula)
	themed := svm.View(80, 10)
	if strings.Contains(themed, "ORDER1") || strings.Contains(themed, "ORDER3") {
		t.Fatal("theme change reused rows rendered for the previous theme")
	}
}

func TestPulseRebuildsOnlyTheRunningStageRow(t *testing.T) {
	w := newAnimationWorkload(t)
	page := w.page
	page.View()
	poisonStageRow(t, page.stageView, "119", cacheSentinel)
	renders := page.stageView.rowRenders

	w.advance()
	frame := page.View()
	if page.stageView.rowRenders != renders+1 {
		t.Fatalf("animation frame rebuilt %d rows, want 1", page.stageView.rowRenders-renders)
	}
	if !strings.Contains(frame, cacheSentinel) {
		t.Fatal("animation frame rebuilt a static row")
	}
	if !strings.Contains(frame, page.stageView.spinnerModel.View()) || !strings.Contains(frame, page.stageView.pulseBar.View()) {
		t.Fatal("animation frame did not render the current stage spinner and pulse")
	}
	if page.frame.fallbacks != 0 {
		t.Fatalf("stage animation fell back to uncached page render: %s", page.frame.fallbackReason)
	}
}
