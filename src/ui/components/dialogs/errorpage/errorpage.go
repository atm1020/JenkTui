package errorpage

import (
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Model struct {
	err    error
	tittle string

	wWidth  int
	wHeight int
}

func NewModel(err error, title string, width, height int) *Model {
	dialogWidth := getDialogWidth(width)
	dialogHeight := getDialogheight(height)

	return &Model{
		err:     err,
		tittle:  title,
		wWidth:  dialogWidth,
		wHeight: dialogHeight,
	}
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if key.Matches(msg, key.NewBinding(key.WithKeys("esc"))) {
			return m, util.CmdHandler(util.CloseDialogMsg{})
		}
	case tea.WindowSizeMsg:
		m.wWidth, m.wHeight = getDialogWidth(msg.Width), getDialogheight(msg.Height)
	}
	return m, nil
}

func (m *Model) View() string {
	t := styles.GetTheme()

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Error).
		Padding(0, 0, 1, 0).
		Render("❌ " + m.tittle)

	errorMsg := lipgloss.NewStyle().
		Foreground(t.FgBase).
		Width(m.wWidth - 6).
		Render(m.err.Error())

	helpText := lipgloss.NewStyle().
		Foreground(t.Grey).
		Italic(true).
		Padding(1, 0, 0, 0).
		Render("Press ESC to close")

	content, w := util.JoinDialogParts(title, errorMsg, helpText)

	border := t.S().DialogBorder
	return border.Width(w + border.GetHorizontalFrameSize()).Render(content)
}

func (m *Model) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl("Error")
}

func (m *Model) CleanUp() tea.Cmd {
	return nil
}

func (m *Model) Dimensions() (int, int) {
	view := m.View()
	return lipgloss.Width(view), lipgloss.Height(view)
}

func (m *Model) KeyMap() help.KeyMap {
	return errorKeyMap{
		Close: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "close"),
		),
	}
}

type errorKeyMap struct {
	Close key.Binding
}

func (k errorKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Close}
}

func (k errorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Close}}
}

// Dialog size - about 1/3 of screen width, clamped to the shared dialog
// floor/ceiling so it reads as the same size system as the other popups.
func getDialogWidth(width int) int {
	return styles.ClampDialogWidth(width/3, width)
}

// Dialog size - about 1/4 of screen height, clamped to the shared dialog
// floor/ceiling.
func getDialogheight(height int) int {
	return styles.ClampDialogHeight(height/4, height)
}
