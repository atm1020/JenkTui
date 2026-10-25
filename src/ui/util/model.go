package util

import (
	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
)

type CommonModel interface {
	Init() tea.Cmd
	View() string
}

type Model interface {
	CommonModel
	Update(tea.Msg) (Model, tea.Cmd)
}

// JenkTuiDialogModel is a dialog the app shell owns. Implementations declare
// the methods in this order. The shell calls CleanUp whenever the dialog is
// replaced or dismissed; it must release owned resources and be idempotent.
type JenkTuiDialogModel interface {
	Init() tea.Cmd
	Update(tea.Msg) (JenkTuiDialogModel, tea.Cmd)
	View() string
	UpdateFooter() tea.Cmd
	CleanUp() tea.Cmd
	Dimensions() (int, int)
	KeyMap() help.KeyMap
}
type JenkTuiModel interface {
	CommonModel
	ID() string
	Update(tea.Msg) (JenkTuiModel, tea.Cmd)
	UpdateFooter() tea.Cmd
	CleanUp() tea.Cmd
	KeyMap() help.KeyMap
}
