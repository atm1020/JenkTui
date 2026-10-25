package serverselector

import (
	"fmt"
	"strings"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// SelectedMsg requests that the application connect to Server.
type SelectedMsg struct{ Server context.ServerConfig }

// SetupMsg carries only identity, never the stored credential.
type SetupMsg struct{ ID string }

type Dialog struct {
	servers         []context.ServerConfig
	cursor          int
	wWidth, wHeight int
	keys            KeyMap
	help            help.Model
}

func New(servers []context.ServerConfig, width, height int, conf context.ServerSelectorDialogKeyMapConfig) *Dialog {
	h := help.New()
	h.SetWidth(width)
	return &Dialog{
		servers: servers,
		wWidth:  width,
		wHeight: height,
		keys:    UserKeyMap(conf),
		help:    h,
	}
}

func (d *Dialog) Init() tea.Cmd { return nil }

func (d *Dialog) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return d, d.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		d.wWidth, d.wHeight = msg.Width, msg.Height
		d.help.SetWidth(msg.Width)
	}
	return d, nil
}

func (d *Dialog) View() string {
	t := styles.GetTheme()

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Primary).
		Padding(0, 1).
		Render("Select Jenkins server")

	rawRows := make([]string, len(d.servers))
	for i, server := range d.servers {
		rawRows[i] = fmt.Sprintf("%s  %s", server.Name, server.JenkinsURL)
	}

	helpView := lipgloss.NewStyle().
		Foreground(t.Grey).
		Padding(0, 1).
		Render(d.help.ShortHelpView(d.keys.ShortHelp()))

	// Size everything to the widest section up front so the row highlight
	// (below) can span the dialog's full width instead of just its text.
	contentWidth := lipgloss.Width(title)
	for _, text := range rawRows {
		// Width() includes horizontal padding, so add 2 for the row's Padding(0, 1).
		if w := lipgloss.Width(text) + 2; w > contentWidth {
			contentWidth = w
		}
	}
	if w := lipgloss.Width(helpView); w > contentWidth {
		contentWidth = w
	}

	// Clamp into the shared dialog floor/ceiling so a short server list still
	// reads as the same popup size as the rest of the app, and a long one
	// never overflows the window.
	contentWidth = styles.ClampDialogWidth(contentWidth, d.wWidth)

	rows := make([]string, len(rawRows))
	for i, text := range rawRows {
		style := lipgloss.NewStyle().Width(contentWidth).Padding(0, 1)
		if i == d.cursor {
			style = style.Background(t.SelectedBackground).Foreground(t.FgBase)
		} else {
			style = style.Foreground(t.FgBase)
		}
		rows[i] = style.Render(text)
	}
	rowsBlock := strings.Join(rows, "\n")

	hLine := util.HorizontalLine(contentWidth, t.Border)

	content, w := util.JoinDialogParts(title, hLine, rowsBlock, hLine, helpView)

	border := t.S().DialogBorder
	return border.Width(w + border.GetHorizontalFrameSize()).Render(content)
}

func (d *Dialog) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl("Server Selector")
}

func (d *Dialog) CleanUp() tea.Cmd {
	return nil
}

func (d *Dialog) Dimensions() (int, int) {
	return lipgloss.Width(d.View()), lipgloss.Height(d.View())
}

func (d *Dialog) KeyMap() help.KeyMap { return d.keys }

func (d *Dialog) handleKeyPress(msg tea.KeyPressMsg) tea.Cmd {
	if key.Matches(msg, d.keys.Add) {
		return util.CmdHandler(SetupMsg{})
	}
	if key.Matches(msg, d.keys.Edit) && len(d.servers) > 0 {
		return util.CmdHandler(SetupMsg{ID: d.servers[d.cursor].ID})
	}
	if key.Matches(msg, d.keys.Up) && d.cursor > 0 {
		d.cursor--
	}
	if key.Matches(msg, d.keys.Down) && d.cursor < len(d.servers)-1 {
		d.cursor++
	}
	if key.Matches(msg, d.keys.Select) && len(d.servers) > 0 {
		server := d.servers[d.cursor]
		return func() tea.Msg { return SelectedMsg{Server: server} }
	}
	if key.Matches(msg, d.keys.Close) {
		return util.CmdHandler(util.CloseDialogMsg{})
	}
	return nil
}
