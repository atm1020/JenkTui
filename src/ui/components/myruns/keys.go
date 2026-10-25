package myruns

import (
	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/key"
)

type KeyMap struct {
	Down             key.Binding
	Up               key.Binding
	Enter            key.Binding
	OpenJob          key.Binding
	Logs             key.Binding
	OpenBuildBrowser key.Binding
	OpenJobBrowser   key.Binding
	Replay           key.Binding
	TriggerBuild     key.Binding
	CancelBuild      key.Binding
	Delete           key.Binding
	Filter           key.Binding
	NextField        key.Binding
	Cancel           key.Binding
	SortCycle        key.Binding
	Direction        key.Binding
	StatusCycle      key.Binding
	ResetFilter      key.Binding
}

func UserKeyMap(conf context.MyRunsKeyMapConfig) KeyMap {
	return KeyMap{
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
			key.WithHelp(conf.Down.HelpText(), "down"),
		),
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
			key.WithHelp(conf.Up.HelpText(), "up"),
		),
		Enter: key.NewBinding(
			key.WithKeys(conf.Enter...),
			key.WithHelp(conf.Enter.HelpText(), "open build"),
		),
		OpenJob: key.NewBinding(
			key.WithKeys(conf.OpenJob...),
			key.WithHelp(conf.OpenJob.HelpText(), "open job"),
		),
		Logs: key.NewBinding(
			key.WithKeys(conf.Logs...),
			key.WithHelp(conf.Logs.HelpText(), "view logs"),
		),
		OpenBuildBrowser: key.NewBinding(
			key.WithKeys(conf.OpenBuildBrowser...),
			key.WithHelp(conf.OpenBuildBrowser.HelpText(), "open build in browser"),
		),
		OpenJobBrowser: key.NewBinding(
			key.WithKeys(conf.OpenJobBrowser...),
			key.WithHelp(conf.OpenJobBrowser.HelpText(), "open job in browser"),
		),
		Replay: key.NewBinding(
			key.WithKeys(conf.Replay...),
			key.WithHelp(conf.Replay.HelpText(), "replay build"),
		),
		TriggerBuild: key.NewBinding(
			key.WithKeys(conf.TriggerBuild...),
			key.WithHelp(conf.TriggerBuild.HelpText(), "trigger build"),
		),
		CancelBuild: key.NewBinding(
			key.WithKeys(conf.CancelBuild...),
			key.WithHelp(conf.CancelBuild.HelpText(), "cancel build"),
		),
		Delete: key.NewBinding(
			key.WithKeys(conf.Delete...),
			key.WithHelp(conf.Delete.HelpText(), "delete"),
		),
		Filter: key.NewBinding(
			key.WithKeys(conf.Filter...),
			key.WithHelp(conf.Filter.HelpText(), "filter runs"),
		),
		NextField: key.NewBinding(
			key.WithKeys(conf.NextField...),
			key.WithHelp(conf.NextField.HelpText(), "next field"),
		),
		Cancel: key.NewBinding(
			key.WithKeys(conf.Cancel...),
			key.WithHelp(conf.Cancel.HelpText(), "cancel filter"),
		),
		SortCycle: key.NewBinding(
			key.WithKeys(conf.SortCycle...),
			key.WithHelp(conf.SortCycle.HelpText(), "cycle sort field"),
		),
		Direction: key.NewBinding(
			key.WithKeys(conf.Direction...),
			key.WithHelp(conf.Direction.HelpText(), "toggle direction"),
		),
		StatusCycle: key.NewBinding(
			key.WithKeys(conf.StatusCycle...),
			key.WithHelp(conf.StatusCycle.HelpText(), "focus status filter"),
		),
		ResetFilter: key.NewBinding(
			key.WithKeys(conf.ResetFilter...),
			key.WithHelp(conf.ResetFilter.HelpText(), "reset filter"),
		),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Enter, k.OpenJob, k.Logs, k.OpenBuildBrowser, k.OpenJobBrowser, k.Replay, k.TriggerBuild, k.CancelBuild, k.Delete, k.Filter, k.SortCycle, k.Direction}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.OpenJob},
		{k.Logs, k.OpenBuildBrowser, k.OpenJobBrowser},
		{k.Replay, k.TriggerBuild, k.CancelBuild, k.Delete},
		{k.Filter, k.NextField, k.Cancel},
		{k.SortCycle, k.Direction, k.StatusCycle, k.ResetFilter},
	}
}
