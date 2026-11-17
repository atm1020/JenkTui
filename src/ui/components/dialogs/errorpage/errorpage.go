package errorpage

import (
	"errors"
	"strings"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Model struct {
	message   string
	tittle    string
	body      string
	truncated bool
	copied    bool
	keys      errorKeyMap

	wWidth  int
	wHeight int
}

type htmlResponse interface {
	HTMLResponseBody() (body string, truncated bool)
}

func NewModel(err error, title string, width, height int, conf context.ErrorDialogKeyMapConfig) *Model {
	dialogWidth := getDialogWidth(width)
	dialogHeight := getDialogheight(height)

	m := &Model{
		message: err.Error(),
		tittle:  title,
		wWidth:  dialogWidth,
		wHeight: dialogHeight,
		keys: errorKeyMap{
			Close:    key.NewBinding(key.WithKeys(conf.Close...), key.WithHelp(conf.Close.HelpText(), "close")),
			CopyHTML: key.NewBinding(key.WithKeys(conf.CopyHTML...), key.WithHelp(conf.CopyHTML.HelpText(), "copy HTML")),
		},
	}
	var response htmlResponse
	if errors.As(err, &response) {
		m.body, m.truncated = response.HTMLResponseBody()
	}
	m.keys.CopyHTML.SetEnabled(m.body != "")
	return m
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Close):
			return m, util.CmdHandler(util.CloseDialogMsg{})
		case key.Matches(msg, m.keys.CopyHTML):
			m.copied = true
			return m, tea.SetClipboard(m.body)
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
		Render(m.message)

	helpText := lipgloss.NewStyle().
		Foreground(t.Grey).
		Italic(true).
		Padding(1, 0, 0, 0).
		Width(m.wWidth - 6).
		Render(m.hint())

	content, w := util.JoinDialogParts(title, errorMsg, helpText)

	border := t.S().DialogBorder
	return border.Width(w + border.GetHorizontalFrameSize()).Render(content)
}

func (m *Model) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl("Error")
}

func (m *Model) CleanUp() tea.Cmd {
	m.body = ""
	m.keys.CopyHTML.SetEnabled(false)
	return nil
}

func (m *Model) Dimensions() (int, int) {
	view := m.View()
	return lipgloss.Width(view), lipgloss.Height(view)
}

func (m *Model) KeyMap() help.KeyMap {
	return m.keys
}

func (m *Model) hint() string {
	var parts []string
	if m.body != "" {
		warning := "HTML may contain sensitive data. Review before sharing."
		if m.truncated {
			warning += " Response is truncated."
		}
		if m.copied {
			warning += " HTML sent to clipboard."
		}
		parts = append(parts, warning)
	}
	var bindings []string
	for _, binding := range m.keys.ShortHelp() {
		h := binding.Help()
		bindings = append(bindings, h.Key+" "+h.Desc)
	}
	return strings.Join(append(parts, strings.Join(bindings, " • ")), "\n")
}

type errorKeyMap struct {
	Close    key.Binding
	CopyHTML key.Binding
}

func (k errorKeyMap) ShortHelp() []key.Binding {
	bindings := []key.Binding{k.Close}
	if k.CopyHTML.Enabled() {
		bindings = append(bindings, k.CopyHTML)
	}
	return bindings
}

func (k errorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
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
