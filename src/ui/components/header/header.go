package header

import (
	"strings"

	"github.com/atm1020/jenktui/src/ui/util"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Model struct {
	wWidth, wHeight int
}

func NewModel() *Model {
	return &Model{}
}

func (h *Model) Init() tea.Cmd {
	return nil
}

func (h *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		h.wWidth = msg.Width
	}
	return h, nil
}

func (h *Model) View() string {
	title := " JenkTui "
	const (
		gap          = " "
		leftPadding  = 1
		rightPadding = 1
	)

	var b strings.Builder

	b.WriteString(util.Title(title, h.wWidth-lipgloss.Width(b.String())-leftPadding-rightPadding))
	b.WriteString(gap)
	b.WriteString(gap)

	remainingWidth := h.wWidth -
		lipgloss.Width(b.String()) -
		leftPadding -
		rightPadding

	if remainingWidth > 0 {
		b.WriteString(gap)
	}

	view := lipgloss.NewStyle().Padding(0, rightPadding, 1, leftPadding).Render(b.String())
	return view
}
