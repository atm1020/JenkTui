package globalsearch

import (
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
)

const (
	CharLimitGlobalSearch   = 32
	GlobalSearchDialogWidth = CharLimitGlobalSearch + 4
)

type FetchGlobalSearchResultsMsg struct {
	Query string
}

type SearchInputModel struct {
	textInput textinput.Model
	keyMap    SearchInputKeyMap
}

func NewSearchInputModel(keymapConf context.GlobalsearchDialogKeyMapConfig) *SearchInputModel {
	return &SearchInputModel{
		textInput: buildTextInputModel(),
		keyMap:    UserSearchInputKeyMap(keymapConf),
	}
}

func (m *SearchInputModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m *SearchInputModel) Update(msg tea.Msg) (*SearchInputModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m, m.handleKeyPressMsg(msg)
	}
	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return m, cmd
}

func (m *SearchInputModel) View() string {
	return m.textInput.View()
}

func (m *SearchInputModel) handleKeyPressMsg(msg tea.KeyMsg) tea.Cmd {
	log.Debug("SearchInputModel received", "msg", msg)
	if key.Matches(msg, m.keyMap.Search) {
		value := m.textInput.Value()
		m.textInput.CursorEnd()
		return util.CmdHandler(FetchGlobalSearchResultsMsg{
			Query: value,
		})
	}

	if key.Matches(msg, m.keyMap.ClearInput) {
		m.textInput.SetValue("")
		m.textInput.Focus()
	}

	var cmd tea.Cmd
	m.textInput, cmd = m.textInput.Update(msg)
	return cmd
}

func buildTextInputModel() textinput.Model {
	ti := textinput.New()
	ti.Placeholder = "Global Search..."
	ti.CharLimit = CharLimitGlobalSearch
	ti.SetWidth(GlobalSearchDialogWidth)
	// Paint the prompt, value, placeholder and trailing padding with the dialog
	// background so the input cells don't leak the terminal's default background.
	ti.Focus()
	return ti
}
