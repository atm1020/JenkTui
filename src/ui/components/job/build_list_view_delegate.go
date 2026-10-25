package job

import (
	"fmt"
	"io"

	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	// "charm.land/lipgloss/v2"
)

type BuildListViewDelegate struct{}

func (d BuildListViewDelegate) Height() int { return 3 }

func (d BuildListViewDelegate) Spacing() int { return 0 }

func (d BuildListViewDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d BuildListViewDelegate) Render(w io.Writer, m list.Model, index int, ii list.Item) {
	i, ok := ii.(BuildListItem)
	if !ok {
		return
	}
	isSelected := index == m.Index()
	t := styles.GetTheme()
	var content string
	if isSelected {
		content = t.S().SelectedBorder.Render(i.BuildId)
		// content = fmt.Sprintf("#%d", i.Number)
		// content = t.S().SelectedBorder.Render()
	} else {
		content = t.S().DefaultBorder.Render(i.BuildId)
		// content = fmt.Sprintf("#%d", i.Number)
	}

	fmt.Fprint(w, lipgloss.Place(m.Width(), 1, lipgloss.Center, lipgloss.Center, content))
}
