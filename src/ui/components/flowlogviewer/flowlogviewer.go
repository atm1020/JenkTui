package flowlogviewer

import (
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/components/logviewer"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
)

// Model wraps logviewer to implement JenkTuiModel interface
type Model struct {
	logviewer *logviewer.Model
	filePath  string
}

func NewModel(ctx *context.ProgramContext, width, height int, filePath string) *Model {
	return &Model{
		logviewer: logviewer.NewModel(ctx, width, height, filePath),
		filePath:  filePath,
	}
}

func (m *Model) Init() tea.Cmd {
	return m.logviewer.Init()
}

func (m *Model) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	var cmd tea.Cmd
	m.logviewer, cmd = m.logviewer.Update(msg)
	return m, cmd
}

func (m *Model) View() string {
	return m.logviewer.View()
}

func (m *Model) ID() string {
	return "flowlog:" + m.filePath
}

func (m *Model) UpdateFooter() tea.Cmd {
	return nil
}

func (m *Model) CleanUp() tea.Cmd {
	return m.logviewer.CleanUp()
}

func (m *Model) KeyMap() help.KeyMap {
	return m.logviewer.KeyMap()
}

// IsCapturingInput delegates to the log viewer so the app shell withholds global
// letter shortcuts while its search box is focused.
func (m *Model) IsCapturingInput() bool {
	return m.logviewer.IsCapturingInput()
}
