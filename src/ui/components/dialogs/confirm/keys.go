package confirm

import (
	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/key"
)

// KeyMap defines the keyboard bindings for the confirmation dialog.
type KeyMap struct {
	LeftRight  key.Binding
	EnterSpace key.Binding
	Yes        key.Binding
	No         key.Binding
	Tab        key.Binding
	Close      key.Binding
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.LeftRight, k.EnterSpace, k.Yes, k.No, k.Close}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.LeftRight, k.Tab, k.EnterSpace},
		{k.Yes, k.No, k.Close},
	}
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		LeftRight: key.NewBinding(
			key.WithKeys("h", "l"),
			key.WithHelp("h/l", "switch options"),
		),
		EnterSpace: key.NewBinding(
			key.WithKeys("enter", " "),
			key.WithHelp("enter/space", "confirm"),
		),
		Yes: key.NewBinding(
			key.WithKeys("y", "Y"),
			key.WithHelp("y/Y", "yes"),
		),
		No: key.NewBinding(
			key.WithKeys("n", "N"),
			key.WithHelp("n/N", "no"),
		),
		Tab: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch options"),
		),
		Close: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "cancel"),
		),
	}
}

func DefaultKeymap() KeyMap {
	return DefaultKeyMap()
}

// UserKeyMap creates a KeyMap from user configuration
func UserKeyMap(conf context.ConfirmDialogKeyMapConfig) KeyMap {
	return KeyMap{
		LeftRight: key.NewBinding(
			key.WithKeys(conf.LeftRight...),
			key.WithHelp(conf.LeftRight.HelpText(), "switch options"),
		),
		EnterSpace: key.NewBinding(
			key.WithKeys(conf.Confirm...),
			key.WithHelp(conf.Confirm.HelpText(), "confirm"),
		),
		Yes: key.NewBinding(
			key.WithKeys(conf.Yes...),
			key.WithHelp(conf.Yes.HelpText(), "yes"),
		),
		No: key.NewBinding(
			key.WithKeys(conf.No...),
			key.WithHelp(conf.No.HelpText(), "no"),
		),
		Tab: key.NewBinding(
			key.WithKeys(conf.Tab...),
			key.WithHelp(conf.Tab.HelpText(), "switch options"),
		),
		Close: key.NewBinding(
			key.WithKeys(conf.Close...),
			key.WithHelp(conf.Close.HelpText(), "cancel"),
		),
	}
}
