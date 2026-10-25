package nodeselector

import (
	"fmt"
	"io"
	"strings"

	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const builtInDisplayName = "built-in"

// NodeItem is one executor and the stages that used it.
type NodeItem struct {
	Name   string
	Stages []string
}

func NewNodeItem(name string, stages []string) NodeItem {
	return NodeItem{Name: name, Stages: append([]string(nil), stages...)}
}

func (i NodeItem) Title() string { return i.DisplayName() }

func (i NodeItem) Description() string { return strings.Join(i.Stages, ", ") }

func (i NodeItem) FilterValue() string { return i.Name }

func (i NodeItem) DisplayName() string {
	if i.Name == "" {
		return builtInDisplayName
	}
	return i.Name
}

// NodeDelegate renders a node name and its stage usage on two lines.
type NodeDelegate struct {
	width               int
	horizontalLineWidth int
}

func (d NodeDelegate) Height() int  { return 2 }
func (d NodeDelegate) Spacing() int { return 0 }

func (d NodeDelegate) Update(tea.Msg, *list.Model) tea.Cmd { return nil }

func (d NodeDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	node, ok := item.(NodeItem)
	if !ok {
		return
	}

	rowWidth := max(1, min(d.width, d.horizontalLineWidth))
	name := truncateMiddle("  "+node.DisplayName(), rowWidth)
	name += strings.Repeat(" ", max(0, rowWidth-lipgloss.Width(name)))

	stageNames := strings.Join(node.Stages, ", ")
	stages := ansi.Truncate(fmt.Sprintf("    Stages: %s", stageNames), rowWidth, "…")
	stages += strings.Repeat(" ", max(0, rowWidth-lipgloss.Width(stages)))

	t := styles.GetTheme()
	nameStyle := lipgloss.NewStyle().Width(rowWidth).Foreground(t.FgBase)
	stageStyle := lipgloss.NewStyle().Width(rowWidth).Foreground(t.Grey).Faint(true)
	if index == m.Index() {
		nameStyle = nameStyle.Background(t.SelectedBackground).Foreground(t.FgBase)
		stageStyle = lipgloss.NewStyle().
			Width(rowWidth).
			Background(t.SelectedBackground).
			Foreground(t.FgBase)
	}

	_, _ = fmt.Fprint(w, lipgloss.JoinVertical(lipgloss.Left,
		nameStyle.Render(name),
		stageStyle.Render(stages),
	))
}

func truncateMiddle(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}

	const ellipsis = "…"
	if width <= lipgloss.Width(ellipsis) {
		return ansi.Truncate(ellipsis, width, "")
	}

	remaining := width - lipgloss.Width(ellipsis)
	headWidth := (remaining + 1) / 2
	tailWidth := remaining - headWidth
	stringWidth := lipgloss.Width(s)
	return ansi.Truncate(s, headWidth, "") + ellipsis + ansi.Cut(s, stringWidth-tailWidth, stringWidth)
}
