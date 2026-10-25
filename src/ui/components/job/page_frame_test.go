package job

import (
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/ui/styles"

	"github.com/atm1020/gojenkins"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestPanelChromeMatchesStyleRender(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	t.Cleanup(func() { styles.SwitchTheme(styles.Github) })

	for _, theme := range []styles.ThemeType{styles.Github, styles.Dracula} {
		styles.SwitchTheme(theme)
		borders := styles.GetTheme().S()
		for _, style := range []lipgloss.Style{
			borders.DefaultBorder,
			borders.SelectedBorder,
			borders.DefaultBorder.BorderLeft(false),
			borders.SelectedBorder.BorderLeft(false),
		} {
			for _, width := range []int{8, 16, 22, 136} {
				content := lipgloss.NewStyle().Width(width).Height(3).Render("stage\nline")
				chrome, ok := newPanelChrome(style, width)
				if !ok {
					t.Fatalf("theme %s width %d: chrome was not captured", theme, width)
				}
				got, ok := chrome.apply(content, 3)
				if !ok {
					t.Fatalf("theme %s width %d: chrome rejected fixed-size content", theme, width)
				}
				want := style.Render(content)
				if got.text != want {
					t.Fatalf("theme %s width %d: framed panel diverged from lipgloss", theme, width)
				}
			}
		}
	}
}

func TestComposePageMatchesLipgloss(t *testing.T) {
	health := measureSection(lipgloss.NewStyle().Width(20).Height(4).Render("health\nscore"))
	job := measureSection(lipgloss.NewStyle().Width(12).Height(6).Render("builds"))
	stage := measureSection(lipgloss.NewStyle().Width(30).Height(6).Render("stages"))

	for _, window := range []struct{ w, h int }{{50, 12}, {80, 20}, {40, 8}, {30, 6}} {
		got := composePage(window.w, window.h, health, job, stage)
		want := lipgloss.Place(window.w, window.h, lipgloss.Top, lipgloss.Left,
			lipgloss.JoinVertical(lipgloss.Left, health.text,
				lipgloss.JoinHorizontal(lipgloss.Left, job.text, stage.text)))
		if got != want {
			t.Fatalf("compose diverged at %dx%d", window.w, window.h)
		}
	}
}

func TestCachedRenderMatchesDirectJobPage(t *testing.T) {
	w := newAnimationWorkload(t)
	assertCachedMatchesDirect(t, w.page)
	for range 3 {
		w.advance()
		assertCachedMatchesDirect(t, w.page)
	}

	_, _ = w.page.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	assertCachedMatchesDirect(t, w.page)
	_, _ = w.page.Update(tea.KeyPressMsg{Code: 'B', Text: "B"})
	assertCachedMatchesDirect(t, w.page)
	_, _ = w.page.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	assertCachedMatchesDirect(t, w.page)
	_, _ = w.page.Update(tea.WindowSizeMsg{Width: 100, Height: 32})
	assertCachedMatchesDirect(t, w.page)
	_, _ = w.page.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	assertCachedMatchesDirect(t, w.page)

	w.page.isFavorite = true
	assertCachedMatchesDirect(t, w.page)
	w.page.loading = true
	assertCachedMatchesDirect(t, w.page)
	w.page.loading = false
	assertCachedMatchesDirect(t, w.page)
	w.page.applySnapshotData(finishedAnimationRuns(w.page), false)
	assertCachedMatchesDirect(t, w.page)
	if w.page.frame.fallbacks != 0 {
		t.Fatalf("workload fell back to uncached render: %s", w.page.frame.fallbackReason)
	}
}

func TestRepeatedViewReusesCachedPage(t *testing.T) {
	page, _ := newAnimationPage()
	first := page.View()
	layouts := page.frame.layoutBuilds
	page.frame.stageContent = "POISON"
	second := page.View()
	if second != first {
		t.Fatal("unchanged page was rebuilt")
	}
	if page.frame.layoutBuilds != layouts {
		t.Fatal("repeated view recalculated layout")
	}
	if page.frame.hits < 2 {
		t.Fatalf("repeated view hits = %d, want the page cache", page.frame.hits)
	}
}

func TestAnimationReusesStableSections(t *testing.T) {
	w := newAnimationWorkload(t)
	if view := w.page.View(); w.page.frame.fallbacks != 0 {
		t.Fatalf("workload fell back before animation: %s\n%s", w.page.frame.fallbackReason, view)
	}
	layouts := w.page.frame.layoutBuilds
	chromes := w.page.frame.chromeBuilds
	poisonSection(&w.page.frame.job, "JOB_CACHE")
	poisonSection(&w.page.frame.health, "HEALTH_CACHE")
	poisonSection(&w.page.frame.stage, "STAGE_CACHE")

	w.advance()
	frame := w.page.View()
	if !strings.Contains(frame, "JOB_CACHE") {
		t.Fatal("animation frame rebuilt the job list")
	}
	if strings.Contains(frame, "HEALTH_CACHE") || strings.Contains(frame, "STAGE_CACHE") {
		t.Fatal("animation frame reused a stale health or stage panel")
	}
	if w.page.frame.layoutBuilds != layouts {
		t.Fatal("animation recalculated layout")
	}
	if w.page.frame.chromeBuilds != chromes {
		t.Fatal("animation rebuilt border chrome")
	}
}

func TestSelectionLoadingResizeAndPollInvalidateCachedSections(t *testing.T) {
	page, _ := newAnimationPage()
	page.View()

	poisonSection(&page.frame.job, "JOB_CACHE")
	poisonSection(&page.frame.health, "HEALTH_CACHE")
	_, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if view := page.View(); !strings.Contains(view, "JOB_CACHE") || !strings.Contains(view, "HEALTH_CACHE") {
		t.Fatal("stage selection rebuilt unrelated sections")
	}

	poisonSection(&page.frame.stage, "STAGE_CACHE")
	_, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if view := page.View(); strings.Contains(view, "STAGE_CACHE") {
		t.Fatal("stage selection reused a stale stage panel")
	}

	page.View()
	poisonSection(&page.frame.job, "JOB_CACHE")
	_, _ = page.Update(tea.KeyPressMsg{Code: 'B', Text: "B"})
	if view := page.View(); strings.Contains(view, "JOB_CACHE") {
		t.Fatal("focus change reused a panel framed with the previous border")
	}

	page.View()
	poisonSection(&page.frame.job, "JOB_CACHE")
	_, _ = page.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if view := page.View(); strings.Contains(view, "JOB_CACHE") {
		t.Fatal("build selection reused a stale job list")
	}

	_, _ = page.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	page.View()
	poisonSection(&page.frame.job, "JOB_CACHE")
	poisonSection(&page.frame.stage, "STAGE_CACHE")
	_, _ = page.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	resized := page.View()
	if strings.Contains(resized, "JOB_CACHE") || strings.Contains(resized, "STAGE_CACHE") {
		t.Fatal("resize reused sections framed for the old terminal")
	}

	page.View()
	poisonSection(&page.frame.job, "JOB_CACHE")
	poisonSection(&page.frame.health, "HEALTH_CACHE")
	page.applySnapshotData(finishedAnimationRuns(page), false)
	polled := page.View()
	if strings.Contains(polled, "JOB_CACHE") || strings.Contains(polled, "HEALTH_CACHE") {
		t.Fatal("polling reused the job list or in-progress health panel")
	}
	if !strings.Contains(ansi.Strip(polled), "Polled") {
		t.Fatal("polling did not render the updated stage")
	}

	page.View()
	poisonSection(&page.frame.job, "JOB_CACHE")
	page.loading = true
	loading := page.View()
	if strings.Contains(loading, "JOB_CACHE") || !strings.Contains(ansi.Strip(loading), "Loading...") {
		t.Fatal("loading reused the job list or hid the loading panels")
	}

	styles.SwitchTheme(styles.Dracula)
	t.Cleanup(func() { styles.SwitchTheme(styles.Github) })
	page.loading = false
	page.View()
	poisonSection(&page.frame.job, "JOB_CACHE")
	styles.SwitchTheme(styles.Github)
	if view := page.View(); strings.Contains(view, "JOB_CACHE") {
		t.Fatal("theme change reused sections framed for the previous theme")
	}
}

func assertCachedMatchesDirect(t *testing.T, page *Model) {
	t.Helper()
	cached := page.View()
	direct := page.renderJobPageUncached()
	if cached != direct {
		t.Fatalf("cached page diverged from direct render (fallback %q)\ncached width %d height %d\ndirect width %d height %d\nhealth %dx%d job %dx%d stage %dx%d active %d fav %v loading %v",
			page.frame.fallbackReason, lipgloss.Width(cached), lipgloss.Height(cached), lipgloss.Width(direct), lipgloss.Height(direct),
			page.frame.health.width, page.frame.health.height, page.frame.job.width, page.frame.job.height, page.frame.stage.width, page.frame.stage.height,
			page.activeView, page.isFavorite, page.loading)
	}
}

func poisonSection(section *framedSection, marker string) {
	if len(section.lines) == 0 {
		section.lines = []string{marker}
		section.height = 1
		section.text = marker
		return
	}
	section.lines[0] = marker
	section.text = strings.Join(section.lines, "\n")
}

func finishedAnimationRuns(page *Model) []gojenkins.PipelineRun {
	runs := make([]gojenkins.PipelineRun, 0, len(page.stageView.pipelineRuns))
	for _, run := range page.stageView.pipelineRuns {
		if run == nil {
			continue
		}
		copyRun := *run
		copyRun.Status = "SUCCESS"
		copyRun.EndTime = copyRun.StartTime + 1000
		stages := make([]gojenkins.PipelineNode, len(run.Stages))
		copy(stages, run.Stages)
		for i := range stages {
			stages[i].Status = "SUCCESS"
		}
		if len(stages) > 0 {
			stages[0].Name = "Polled"
		}
		copyRun.Stages = stages
		runs = append(runs, copyRun)
	}
	return runs
}
