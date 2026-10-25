package buildparams

import (
	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/key"
)

type KeyMap struct {
	Up           key.Binding
	Down         key.Binding
	Tab          key.Binding
	Toggle       key.Binding
	OpenPicker   key.Binding
	ToggleChoice key.Binding
	Reload       key.Binding
	Filter       key.Binding
	NextPage     key.Binding
	PrevPage     key.Binding
	GoToStart    key.Binding
	GoToEnd      key.Binding
	Confirm      key.Binding
	Close        key.Binding
	HelpToggle   key.Binding
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.OpenPicker, k.Confirm, k.Close, k.HelpToggle}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Tab, k.Toggle, k.OpenPicker},
		{k.ToggleChoice, k.Reload, k.Filter, k.Confirm, k.Close, k.HelpToggle},
	}
}

func UserKeyMap(conf context.BuildParamsDialogKeyMapConfig) KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
			key.WithHelp(conf.Up.HelpText(), "previous field"),
		),
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
			key.WithHelp(conf.Down.HelpText(), "next field"),
		),
		Tab: key.NewBinding(
			key.WithKeys(conf.Tab...),
			key.WithHelp(conf.Tab.HelpText(), "next field"),
		),
		Toggle: key.NewBinding(
			key.WithKeys(conf.Toggle...),
			key.WithHelp(conf.Toggle.HelpText(), "toggle"),
		),
		OpenPicker: key.NewBinding(
			key.WithKeys(conf.OpenPicker...),
			key.WithHelp(conf.OpenPicker.HelpText(), "choose"),
		),
		ToggleChoice: key.NewBinding(
			key.WithKeys(conf.ToggleChoice...),
			key.WithHelp(conf.ToggleChoice.HelpText(), "select choice"),
		),
		Reload: key.NewBinding(
			key.WithKeys(conf.Reload...),
			key.WithHelp(conf.Reload.HelpText(), "reload choices"),
		),
		Filter: key.NewBinding(
			key.WithKeys(conf.Filter...),
			key.WithHelp(conf.Filter.HelpText(), "filter choices"),
		),
		NextPage: key.NewBinding(
			key.WithKeys(conf.NextPage...),
			key.WithHelp(conf.NextPage.HelpText(), "next page"),
		),
		PrevPage: key.NewBinding(
			key.WithKeys(conf.PrevPage...),
			key.WithHelp(conf.PrevPage.HelpText(), "previous page"),
		),
		GoToStart: key.NewBinding(
			key.WithKeys(conf.GoToStart...),
			key.WithHelp(conf.GoToStart.HelpText(), "first choice"),
		),
		GoToEnd: key.NewBinding(
			key.WithKeys(conf.GoToEnd...),
			key.WithHelp(conf.GoToEnd.HelpText(), "last choice"),
		),
		Confirm: key.NewBinding(
			key.WithKeys(conf.Confirm...),
			key.WithHelp(conf.Confirm.HelpText(), "build"),
		),
		Close: key.NewBinding(
			key.WithKeys(conf.Close...),
			key.WithHelp(conf.Close.HelpText(), "cancel"),
		),
		HelpToggle: key.NewBinding(
			key.WithKeys("?"),
			key.WithHelp("?", "help"),
		),
	}
}
