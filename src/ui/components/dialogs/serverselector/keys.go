package serverselector

import (
	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/key"
)

// KeyMap defines the keybindings for the server selector dialog.
type KeyMap struct {
	Add    key.Binding
	Edit   key.Binding
	Select key.Binding
	Up     key.Binding
	Down   key.Binding
	Close  key.Binding
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select, k.Add, k.Edit, k.Close}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// UserKeyMap creates a KeyMap from user configuration.
func UserKeyMap(conf context.ServerSelectorDialogKeyMapConfig) KeyMap {
	return KeyMap{
		Add:  key.NewBinding(key.WithKeys(conf.Add...), key.WithHelp(conf.Add.HelpText(), "add")),
		Edit: key.NewBinding(key.WithKeys(conf.Edit...), key.WithHelp(conf.Edit.HelpText(), "edit")),
		Select: key.NewBinding(
			key.WithKeys(conf.Select...),
			key.WithHelp(conf.Select.HelpText(), "connect"),
		),
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
			key.WithHelp(conf.Up.HelpText(), "up"),
		),
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
			key.WithHelp(conf.Down.HelpText(), "down"),
		),
		Close: key.NewBinding(
			key.WithKeys(conf.Close...),
			key.WithHelp(conf.Close.HelpText(), "close"),
		),
	}
}
