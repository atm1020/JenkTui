package themeselector

import (
	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/key"
)

// ThemeSelectorKeyMap defines the keybindings for the theme selector dialog
type ThemeSelectorKeyMap struct {
	Select key.Binding
	Up     key.Binding
	Down   key.Binding
	Close  key.Binding
}

// DefaultThemeSelectorKeyMap returns the default keybindings for the theme selector
func DefaultThemeSelectorKeyMap() ThemeSelectorKeyMap {
	return ThemeSelectorKeyMap{
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select theme"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "move down"),
		),
		Close: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "close"),
		),
	}
}

// ShortHelp returns keybindings to be shown in the mini help view
func (k ThemeSelectorKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		key.NewBinding(
			key.WithKeys("up", "down"),
			key.WithHelp("↑↓", "navigate"),
		),
		k.Select,
		k.Close,
	}
}

// FullHelp returns keybindings for the expanded help view
func (k ThemeSelectorKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Select, k.Close},
	}
}

// UserKeyMap creates a ThemeSelectorKeyMap from user configuration
func UserKeyMap(conf context.ThemeSelectorDialogKeyMapConfig) ThemeSelectorKeyMap {
	return ThemeSelectorKeyMap{
		Select: key.NewBinding(
			key.WithKeys(conf.Select...),
			key.WithHelp(conf.Select.HelpText(), "select theme"),
		),
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
			key.WithHelp(conf.Up.HelpText(), "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
			key.WithHelp(conf.Down.HelpText(), "move down"),
		),
		Close: key.NewBinding(
			key.WithKeys(conf.Close...),
			key.WithHelp(conf.Close.HelpText(), "close"),
		),
	}
}
