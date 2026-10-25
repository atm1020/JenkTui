package folder

import (
	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/key"
)

type KeyMap struct {
	Down     key.Binding
	Up       key.Binding
	Enter    key.Binding
	Favorite key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
		),
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
		),
		Favorite: key.NewBinding(
			key.WithKeys("F"),
		),
	}
}

func UserKeyMap(conf context.FolderKeyMapConfig) *KeyMap {
	return &KeyMap{
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
		),
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
		),
		Enter: key.NewBinding(
			key.WithKeys(conf.Enter...),
		),
		Favorite: key.NewBinding(
			key.WithKeys(conf.Favorite...),
			key.WithHelp(conf.Favorite.HelpText(), "toggle favorite"),
		),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{{}}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{}}
}
