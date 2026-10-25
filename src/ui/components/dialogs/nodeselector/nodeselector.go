package nodeselector

import (
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// Dialog lets the user choose which build executor name to copy.
type Dialog struct {
	wWidth, wHeight     int
	list                *list.Model
	keyMap              NodeSelectorKeyMap
	help                help.Model
	horizontalLineWidth int
}

func New(ctx *context.ProgramContext, nodes []NodeItem, width, height int) *Dialog {
	items := make([]list.Item, len(nodes))
	for i := range nodes {
		items[i] = nodes[i]
	}

	keyMap := UserKeyMap(ctx.Config.KeyMapConfig.Dialog.NodeSelector)
	h := help.New()
	dialog := &Dialog{keyMap: keyMap, help: h}
	dialog.list = initList(items, 1, 2)
	dialog.resize(width, height)
	return dialog
}

func (m *Dialog) Init() tea.Cmd {
	return nil
}

func (m *Dialog) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m, m.handleKeyPress(msg)
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
	}

	updated, cmd := m.list.Update(msg)
	m.list = &updated
	return m, cmd
}

func (m *Dialog) View() string {
	t := styles.GetTheme()
	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Primary).
		Padding(0, 1).
		Render("Select node to copy")
	helpView := lipgloss.NewStyle().
		Foreground(t.Grey).
		Padding(0, 1).
		Render(m.help.ShortHelpView(m.keyMap.ShortHelp()))
	line := util.HorizontalLine(m.horizontalLineWidth, t.Border)
	content, width := util.JoinDialogParts(title, line, m.list.View(), line, helpView)

	border := t.S().DialogBorder
	return border.Width(width + border.GetHorizontalFrameSize()).Render(content)
}

func (m *Dialog) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl("Node Selector")
}

func (m *Dialog) CleanUp() tea.Cmd {
	return nil
}

func (m *Dialog) Dimensions() (int, int) {
	view := m.View()
	return lipgloss.Width(view), lipgloss.Height(view)
}

func (m *Dialog) KeyMap() help.KeyMap {
	return m.keyMap
}

func (m *Dialog) handleKeyPress(msg tea.KeyPressMsg) tea.Cmd {
	if key.Matches(msg, m.keyMap.Close) {
		return util.CmdHandler(util.CloseDialogMsg{})
	}
	if key.Matches(msg, m.keyMap.Up) {
		m.list.CursorUp()
		return nil
	}
	if key.Matches(msg, m.keyMap.Down) {
		m.list.CursorDown()
		return nil
	}
	if !key.Matches(msg, m.keyMap.Select) {
		return nil
	}

	selected, ok := m.list.SelectedItem().(NodeItem)
	if !ok {
		return nil
	}
	message := "Copied node name"
	if selected.Name == "" {
		message = "Copied node name (built-in — empty)"
	}
	return tea.Batch(
		tea.SetClipboard(selected.Name),
		util.CmdHandler(util.FooterNotiMsg{Message: message, Type: util.NotiInfo}),
		util.CmdHandler(util.CloseDialogMsg{}),
	)
}

func (m *Dialog) resize(width, height int) {
	m.wWidth = max(1, width)
	m.wHeight = max(1, height)

	borderFrame := styles.GetTheme().S().DialogBorder.GetHorizontalFrameSize()
	maxContentWidth := max(1, m.wWidth-2*borderFrame)
	contentWidth := min(styles.ClampDialogWidth(m.wWidth*2/3, m.wWidth), maxContentWidth)
	m.horizontalLineWidth = contentWidth

	listHeight := max(2, len(m.list.Items())*2)
	listHeight = min(listHeight, max(2, m.wHeight-8))
	m.list.SetSize(contentWidth, listHeight)
	m.list.SetDelegate(NodeDelegate{width: contentWidth, horizontalLineWidth: contentWidth})
	m.help.SetWidth(contentWidth)
}

func initList(items []list.Item, width, height int) *list.Model {
	model := list.New(items, NodeDelegate{width: width, horizontalLineWidth: width}, width, height)
	model.SetShowHelp(false)
	model.SetShowTitle(false)
	model.SetShowStatusBar(false)
	model.SetShowPagination(false)
	model.SetFilteringEnabled(false)
	model.DisableQuitKeybindings()
	return &model
}
