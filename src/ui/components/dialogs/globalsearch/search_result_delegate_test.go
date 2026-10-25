package globalsearch

import (
	"bytes"
	"image/color"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

func TestSearchDelegate_DimsNonJobURLs(t *testing.T) {
	items := []list.Item{
		NewSearchResultItem(&gojenkins.Suggestion{Name: "pipeline", URL: "/job/pipeline/"}),
		NewSearchResultItem(&gojenkins.Suggestion{Name: "Built-In Node", URL: "/computer/(built-in)/"}),
	}
	d := SearchDelegate{wWidth: 60}
	l := list.New(items, d, 60, 5)

	render := func(idx int) string {
		t.Helper()
		var buf bytes.Buffer
		d.Render(&buf, l, idx, items[idx])
		return buf.String()
	}

	jobRow := render(0)
	nodeRow := render(1)

	if !strings.Contains(ansi.Strip(jobRow), "pipeline") {
		t.Fatalf("job row missing name: %q", jobRow)
	}
	if !strings.Contains(ansi.Strip(nodeRow), "Built-In Node") {
		t.Fatalf("node row missing name: %q", nodeRow)
	}

	dimSeq := foregroundSeq(t, styles.GetTheme().SubtleText)
	if jobTitle := strings.Split(jobRow, "\n")[0]; strings.Contains(jobTitle, dimSeq) {
		t.Errorf("job title line should not use SubtleText: %q", jobTitle)
	}
	if !strings.Contains(nodeRow, dimSeq) {
		t.Errorf("unselected node row should use SubtleText: %q", nodeRow)
	}

	l.Select(1)
	selectedNode := render(1)
	if !strings.Contains(selectedNode, dimSeq) {
		t.Errorf("selected node row should stay dimmed: %q", selectedNode)
	}
}

func foregroundSeq(t *testing.T, c color.Color) string {
	t.Helper()
	sample := lipgloss.NewStyle().Foreground(c).Render("#")
	idx := strings.Index(sample, "#")
	if idx <= 0 {
		t.Fatalf("could not locate color sequence in sample %q", sample)
	}
	// Strip the terminating "m" so the sequence still matches when lipgloss
	// combines it with a background into one SGR (38;2;...;48;2;...m).
	return strings.TrimSuffix(sample[:idx], "m")
}

func renderRow(t *testing.T, d SearchDelegate, items []list.Item, selected, idx int) []string {
	t.Helper()
	l := list.New(items, d, d.wWidth, 10)
	l.Select(selected)
	var buf bytes.Buffer
	d.Render(&buf, l, idx, items[idx])
	lines := strings.Split(buf.String(), "\n")
	if len(lines) != d.Height() {
		t.Fatalf("row has %d lines, want %d: %q", len(lines), d.Height(), buf.String())
	}
	return lines
}

func TestSearchDelegate_StacksLeafOverBreadcrumb(t *testing.T) {
	items := []list.Item{
		NewSearchResultItem(&gojenkins.Suggestion{Name: "Ops » Monitoring » long-running-scan", URL: "/job/Ops/job/Monitoring/job/long-running-scan/"}),
	}
	d := SearchDelegate{wWidth: 60}
	lines := renderRow(t, d, items, 0, 0)

	title, path := ansi.Strip(lines[0]), ansi.Strip(lines[1])
	if !strings.Contains(title, "long-running-scan") || strings.Contains(title, "Monitoring") {
		t.Errorf("title line should hold only the leaf: %q", title)
	}
	if strings.TrimSpace(path) != "Ops » Monitoring" {
		t.Errorf("path line = %q, want breadcrumb", path)
	}
	if strings.Contains(title+path, "/job/") {
		t.Errorf("URL should not be rendered: %q / %q", title, path)
	}
	if lipgloss.Width(title[:strings.Index(title, "long")]) != lipgloss.Width(path[:strings.Index(path, "Ops")]) {
		t.Errorf("breadcrumb should align under the leaf:\n%q\n%q", title, path)
	}
}

func TestSearchDelegate_PlaceholderWithoutBreadcrumb(t *testing.T) {
	items := []list.Item{
		NewSearchResultItem(&gojenkins.Suggestion{Name: "pipeline", Group: "Items", URL: "/job/pipeline/"}),
		NewSearchResultItem(&gojenkins.Suggestion{Name: "Built-In Node", Group: "Nodes", URL: "/computer/(built-in)/"}),
	}
	d := SearchDelegate{wWidth: 60}

	if got := strings.TrimSpace(ansi.Strip(renderRow(t, d, items, 0, 0)[1])); got != "(root)" {
		t.Errorf("root job path line = %q, want (root)", got)
	}
	if got := strings.TrimSpace(ansi.Strip(renderRow(t, d, items, 0, 1)[1])); got != "Nodes" {
		t.Errorf("node path line = %q, want group label", got)
	}
}

func TestSearchDelegate_SelectionPaintsBothLines(t *testing.T) {
	items := []list.Item{
		NewSearchResultItem(&gojenkins.Suggestion{Name: "Ops » scan", URL: "/job/Ops/job/scan/"}),
	}
	bg := lipgloss.NewStyle().Background(styles.GetTheme().SelectedBackground).Render("#")
	bgSeq := bg[strings.Index(bg, "[")+1 : strings.Index(bg, "m")]
	for i, line := range renderRow(t, SearchDelegate{wWidth: 40}, items, 0, 0) {
		if !strings.Contains(line, bgSeq) {
			t.Errorf("selected line %d missing background: %q", i, line)
		}
	}
}

func TestSearchDelegate_TruncatesToWidth(t *testing.T) {
	items := []list.Item{
		NewSearchResultItem(&gojenkins.Suggestion{
			Name: "Platform » Infrastructure » Team-A » Nightly » a-very-long-job-name-that-overflows",
			URL:  "/job/Platform/job/Infrastructure/job/Team-A/job/Nightly/job/a-very-long-job-name-that-overflows/",
		}),
	}
	d := SearchDelegate{wWidth: 30}
	lines := renderRow(t, d, items, 0, 0)
	for i, line := range lines {
		if w := lipgloss.Width(line); w != d.wWidth {
			t.Errorf("line %d width = %d, want %d: %q", i, w, d.wWidth, ansi.Strip(line))
		}
	}
	title, path := ansi.Strip(lines[0]), ansi.Strip(lines[1])
	if !strings.HasSuffix(strings.TrimSpace(title), "…") || !strings.Contains(title, "a-very") {
		t.Errorf("leaf should be cut from the right: %q", title)
	}
	path = strings.TrimSpace(path)
	if !strings.HasPrefix(path, "…") || !strings.HasSuffix(path, "Team-A » Nightly") {
		t.Errorf("breadcrumb should be cut from the left: %q", path)
	}
}

func TestSearchResultModel_ResizeUpdatesDelegateWidth(t *testing.T) {
	items := []list.Item{
		NewSearchResultItem(&gojenkins.Suggestion{Name: "Ops » scan", URL: "/job/Ops/job/scan/"}),
	}
	m := &SearchResultModel{list: buildListModel(40, 10)}
	m.list.SetItems(items)
	m.handleWindowSize(tea.WindowSizeMsg{Width: 70, Height: 10})

	for _, line := range strings.Split(m.list.View(), "\n")[:2] {
		if w := lipgloss.Width(line); w != 70 {
			t.Errorf("row width after resize = %d, want 70: %q", w, ansi.Strip(line))
		}
	}
}
