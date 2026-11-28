package build

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/ui/styles"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// directView is the page drawn straight through lipgloss, with no section
// cache. Every cached view must match it byte for byte.
func directView(m *Model) string {
	header := m.spinnerModel.View()
	if m.logPane != nil && m.logPane.zoomed {
		return lipgloss.Place(m.wWidth, m.wHeight, lipgloss.Left, lipgloss.Top, m.renderLogPanel(header))
	}
	stepsW, logW := m.bottomPaneWidths()
	var bottom string
	switch {
	case stepsW > 0 && logW > 0:
		var steps string
		if m.stageDetails != nil {
			steps = m.stageDetails.render(m.stageDetails.spinnerModel.View())
		}
		bottom = lipgloss.JoinHorizontal(lipgloss.Top, steps, m.renderLogPanel(header))
	case logW > 0:
		bottom = m.renderLogPanel(header)
	case stepsW > 0 && m.stageDetails != nil:
		bottom = m.stageDetails.render(m.stageDetails.spinnerModel.View())
	}
	return lipgloss.Place(m.wWidth, m.wHeight, lipgloss.Left, lipgloss.Top,
		lipgloss.JoinVertical(lipgloss.Left,
			m.renderBuildInfo(header),
			m.renderStagesPanel(),
			bottom,
		),
	)
}

func assertViewMatchesDirect(t *testing.T, m *Model, context string) string {
	t.Helper()
	got := m.View()
	if want := directView(m); got != want {
		t.Fatalf("%s: cached view differs from the direct render\n got:\n%s\nwant:\n%s", context, got, want)
	}
	return got
}

func TestSectionsMatchDirectRenderForEveryFrame(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	_ = page.View()

	// 8 header frames and 7 step frames realign after 56.
	seen := map[string]bool{}
	for i := range 56 {
		_, _ = page.Update(page.clock.Frame())
		seen[assertViewMatchesDirect(t, page, fmt.Sprint("frame ", i))] = true
	}
	if len(seen) != 56 {
		t.Fatalf("56 frames rendered %d distinct pages, want 56", len(seen))
	}
}

func TestFrameRerendersOnlyAnimatedSections(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	// Mark inline log idle (as when already loaded or empty) to verify static log
	// sections do not re-render on build animation frames.
	page.logPane.loading = false
	page.sections.invalidate()
	_ = page.View()
	c := &page.sections
	before := map[string]int{
		"flow": c.flow.Renders(), "header": c.details.header.Renders(),
		"info": c.info.Renders(), "log": c.log.Renders(),
	}
	steps := make([]int, len(c.details.steps))
	for i := range c.details.steps {
		steps[i] = c.details.steps[i].Renders()
	}

	for range 3 {
		_, _ = page.Update(page.clock.Frame())
		_ = page.View()
	}

	if !c.details.chromeOK || c.details.syncedLines == 0 {
		t.Fatal("stage details fell back to the lipgloss render")
	}
	if c.details.header.Renders() != before["header"] || c.log.Renders() != before["log"] {
		t.Fatalf("an animation frame re-rendered a static section: header=%d->%d log=%d->%d",
			before["header"], c.details.header.Renders(), before["log"], c.log.Renders())
	}
	if !page.hasRunningStage() {
		t.Fatal("animation page has no running stage")
	}
	if c.flow.Renders() != before["flow"]+3 {
		t.Fatalf("stage flow rendered %d times in 3 frames, want 3", c.flow.Renders()-before["flow"])
	}
	if c.info.Renders() != before["info"]+3 {
		t.Fatalf("build info rendered %d times in 3 frames, want 3", c.info.Renders()-before["info"])
	}
	for i, node := range page.stageDetails.pipelineNode.StageFlowNodes {
		want := steps[i]
		if buildstatus.PipelineStatus(node.Status).IsExecuting() {
			want += 3
		}
		if got := c.details.steps[i].Renders(); got != want {
			t.Fatalf("step %d (%s) rendered %d times, want %d", i, node.Status, got-steps[i], want-steps[i])
		}
	}
}

func TestSectionsRefreshOnPageChanges(t *testing.T) {
	changes := []struct {
		name  string
		apply func(*Model)
	}{
		{"resize", func(m *Model) { _, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 40}) }},
		{"stage selection", func(m *Model) { _, _ = m.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}) }},
		{"step selection", func(m *Model) { _, _ = m.Update(tea.KeyPressMsg{Code: 'j', Text: "j"}) }},
		{"theme", func(*Model) { styles.SwitchTheme(styles.Dracula) }},
		{"poll pause", func(m *Model) {
			_ = m.ApplyPollSnapshot(exactRunSnapshot(m, buildstatus.StatusPausedPendingInput, buildstatus.ActivityPaused))
		}},
		{"poll finish", func(m *Model) {
			_ = m.ApplyPollSnapshot(exactRunSnapshot(m, buildstatus.StatusSuccess, buildstatus.ActivityFinished))
		}},
	}
	for _, change := range changes {
		t.Run(change.name, func(t *testing.T) {
			page, _ := newAnimationPage()
			t.Cleanup(func() {
				_ = page.CleanUp()
				styles.SwitchTheme(styles.Github)
			})
			_, _ = page.Update(page.clock.Frame())
			before := page.View()

			change.apply(page)
			after := assertViewMatchesDirect(t, page, "after "+change.name)

			if after == before {
				t.Fatalf("%s did not change the page, so the test proves nothing", change.name)
			}
		})
	}
}

func TestSectionsScrollWithTheStepList(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	// Add enough steps to exceed the compact steps pane height and force scrolling.
	for i := 5; i < 16; i++ {
		page.stageDetails.pipelineNode.StageFlowNodes = append(page.stageDetails.pipelineNode.StageFlowNodes,
			page.stageDetails.pipelineNode.StageFlowNodes[0])
		page.stageDetails.pipelineNode.StageFlowNodes[i].ID = fmt.Sprint(100 + i)
		page.stageDetails.pipelineNode.StageFlowNodes[i].Name = fmt.Sprintf("sh: make step-%d", i+1)
	}
	page.stageDetails.selectedFlowNodeIdx = 0
	page.stageDetails.viewport.SetYOffset(0)
	_, _ = page.Update(tea.WindowSizeMsg{Width: 160, Height: 24})
	_ = page.View()
	for i := range 10 {
		_, _ = page.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
		page.logPane.cancelInFlight()
		page.logPane.loading = false
		page.sections.invalidate()
		assertViewMatchesDirect(t, page, fmt.Sprint("after step move ", i))
		_, _ = page.Update(page.clock.Frame())
		assertViewMatchesDirect(t, page, fmt.Sprint("frame after step move ", i))
	}
	if page.stageDetails.viewport.YOffset() == 0 {
		t.Fatal("step moves did not scroll the pane, so the test proves nothing")
	}
}

func TestSectionsCacheAStaticPage(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })
	_ = page.ApplyPollSnapshot(exactRunSnapshot(page, buildstatus.StatusSuccess, buildstatus.ActivityFinished))
	page.stageDetails.pipelineNode.StageFlowNodes = nil
	page.sections.invalidate()

	first := assertViewMatchesDirect(t, page, "finished build")
	renders := page.sections.info.Renders()
	if page.View() != first || page.sections.info.Renders() != renders {
		t.Fatal("a static page was rendered again between views")
	}
	if !strings.Contains(first, "Pipeline Steps not available") {
		t.Fatal("finished build without steps did not show the missing-steps notice")
	}
}

func TestRunningStageBoxShowsMovingPulse(t *testing.T) {
	page, _ := newAnimationPage()
	t.Cleanup(func() { _ = page.CleanUp() })

	firstPulse := runningBarPattern(page.pulseBar.Width(), []rune(page.pulseBar.View()))
	firstStage := page.renderStages()
	if !strings.Contains(firstStage, firstPulse) {
		t.Fatal("running stage bar does not show the separated pulse pattern")
	}
	_, _ = page.Update(page.clock.Frame())
	nextPulse := runningBarPattern(page.pulseBar.Width(), []rune(page.pulseBar.View()))
	nextStage := page.renderStages()
	if nextPulse == firstPulse || !strings.Contains(nextStage, nextPulse) {
		t.Fatal("animation frame did not advance the stage pulse")
	}
}
