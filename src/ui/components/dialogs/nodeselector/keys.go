package nodeselector

import (
	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/key"
)

// NodeSelectorKeyMap defines the keybindings for the node selector dialog.
type NodeSelectorKeyMap struct {
	Select key.Binding
	Up     key.Binding
	Down   key.Binding
	Close  key.Binding
}

func (k NodeSelectorKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Select, k.Close}
}

func (k NodeSelectorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{k.ShortHelp()}
}

// UserKeyMap creates a NodeSelectorKeyMap from user configuration.
func UserKeyMap(conf context.NodeSelectorDialogKeyMapConfig) NodeSelectorKeyMap {
	return NodeSelectorKeyMap{
		Select: key.NewBinding(
			key.WithKeys(conf.Select...),
			key.WithHelp(conf.Select.HelpText(), "copy"),
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
