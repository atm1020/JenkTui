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
	var details string
	if m.stageDetails != nil {
		details = m.stageDetails.render(m.stageDetails.spinnerModel.View())
	}
	m.updateStageViewLayout()
	return lipgloss.Place(m.wWidth, m.wHeight, lipgloss.Center, lipgloss.Top,
		lipgloss.JoinVertical(lipgloss.Left,
			lipgloss.JoinHorizontal(lipgloss.Center, m.renderBuildInfo(m.spinnerModel.View()), m.renderArtifacts()),
			m.stageView.View(),
			details,
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
	_ = page.View()
	c := &page.sections
	before := map[string]int{
		"artifacts": c.artifacts.Renders(), "flow": c.flow.Renders(), "header": c.details.header.Renders(),
		"info": c.info.Renders(),
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
	if c.artifacts.Renders() != before["artifacts"] || c.details.header.Renders() != before["header"] {
		t.Fatal("an animation frame re-rendered a static section")
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
	_, _ = page.Update(tea.WindowSizeMsg{Width: 160, Height: 30})
	_ = page.View()
	for i := range 4 {
		_, _ = page.Update(tea.KeyPressMsg{Code: 'j', Text: "j"})
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

	first := page.pulseBar.View()
	if !strings.Contains(page.renderStages(), first) {
		t.Fatal("running stage box does not show the pulse bar")
	}
	_, _ = page.Update(page.clock.Frame())
	if next := page.pulseBar.View(); next == first || !strings.Contains(page.renderStages(), next) {
		t.Fatal("animation frame did not advance the stage pulse")
	}
}
