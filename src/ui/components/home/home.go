package home

import (
	"github.com/atm1020/jenktui/src/constants"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"
	"github.com/atm1020/jenktui/src/version"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Model struct {
	ctx     *context.ProgramContext
	wWidth  int
	wHeight int
	keyMap  KeyMap
}

func NewModel(ctx *context.ProgramContext) *Model {
	return &Model{
		ctx:    ctx,
		keyMap: UserKeyMap(util.UserKeyMap(ctx.Config.KeyMapConfig)),
	}
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.wWidth, m.wHeight = msg.Width, msg.Height
	}
	return m, nil
}

func (m *Model) View() string {
	t := styles.GetTheme()
	content := t.S().Base.Render(constants.ASCII) + "\n"

	// Base carries BgBase, so every cell (icons, labels, values) is painted
	// with the app background. Leaving cells with the terminal default leaks
	// through as a lighter rectangle in emulators whose default background
	// differs from BgBase (e.g. Neovim's terminal using its Normal highlight).
	base := t.S().Base
	labelStyle := base.Foreground(t.SubtleText)
	valueStyle := base.Foreground(t.Secondary)

	var infoLines []string

	if m.ctx.ServerConfig != nil && m.ctx.ServerConfig.JenkinsURL != "" {
		serverIcon := base.Foreground(t.Success).Render(styles.ConnectedServerIcon)
		infoLines = append(infoLines, lipgloss.JoinHorizontal(lipgloss.Left,
			serverIcon,
			labelStyle.Render("Server    "),
			valueStyle.Render(m.ctx.ServerConfig.JenkinsURL),
		))
	}

	if m.ctx.Jenkins != nil && m.ctx.Jenkins.Version != "" {
		jenkinsIcon := base.Foreground(t.Primary).Render(styles.JenkinsIcon)
		infoLines = append(infoLines, lipgloss.JoinHorizontal(lipgloss.Left,
			jenkinsIcon,
			labelStyle.Render("Jenkins   "),
			valueStyle.Render(m.ctx.Jenkins.Version),
		))
	}

	terminalIcon := base.Foreground(t.Warning).Render(styles.TerminalIcon)
	infoLines = append(infoLines, lipgloss.JoinHorizontal(lipgloss.Left,
		terminalIcon,
		labelStyle.Render("JenkTui   "),
		valueStyle.Render(version.Version),
	))

	// Pad every line to the widest line's width and fill with BgBase so the
	// alignment gaps and right padding don't leak the terminal default.
	body, _ := util.JoinDialogParts(infoLines...)

	infoBox := base.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.SubtleText).
		Padding(0, 2).
		Render(body)

	content += infoBox

	return lipgloss.Place(m.wWidth,
		m.wHeight,
		lipgloss.Center,
		lipgloss.Center,
		content,
	)
}

func (m *Model) ID() string {
	return "home"
}

func (m *Model) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl("Home")
}

func (m *Model) CleanUp() tea.Cmd {
	return nil
}

func (m *Model) KeyMap() help.KeyMap {
	return m.keyMap
}
