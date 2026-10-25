package themeselector

import (
	"fmt"
	"io"
	"strings"

	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type ThemeItem struct {
	name      string
	themeType styles.ThemeType
	isCurrent bool
}

func NewThemeItem(themeType styles.ThemeType, isCurrent bool) ThemeItem {
	return ThemeItem{
		name:      themeType.String(),
		themeType: themeType,
		isCurrent: isCurrent,
	}
}

func (i ThemeItem) Title() string {
	indicator := "○"
	if i.isCurrent {
		indicator = "●"
	}
	return fmt.Sprintf("%s %s", indicator, i.name)
}

func (i ThemeItem) Description() string {
	return ""
}

// FilterValue returns the theme name for filtering
func (i ThemeItem) FilterValue() string {
	return i.name
}

// ThemeDelegate is the custom delegate for rendering theme items
type ThemeDelegate struct {
	width               int
	horizontalLineWidth int
}

func (d ThemeDelegate) Height() int  { return 1 }
func (d ThemeDelegate) Spacing() int { return 0 }

func (d ThemeDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d ThemeDelegate) Render(w io.Writer, m list.Model, index int, ii list.Item) {
	i, ok := ii.(ThemeItem)
	if !ok {
		return
	}

	isSelected := index == m.Index()
	t := styles.GetTheme()

	// Build the display text
	indicator := "○"
	if i.isCurrent {
		indicator = "●"
	}
	text := fmt.Sprintf("  %s %s", indicator, i.name)
	text += strings.Repeat(" ", max(0, d.horizontalLineWidth-lipgloss.Width(text)))

	// Apply styling based on selection state
	var content string
	if isSelected {
		content = lipgloss.NewStyle().
			Background(t.SelectedBackground).
			Foreground(t.FgBase).
			Width(d.width).
			Padding(0, 1).
			Render(text)
	} else {
		content = lipgloss.NewStyle().
			Foreground(t.FgBase).
			Width(d.width).
			Padding(0, 1).
			Render(text)
	}

	fmt.Fprint(w, content)
}
