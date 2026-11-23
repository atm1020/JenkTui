package commandpalette

import (
	"charm.land/bubbles/v2/key"
	"github.com/atm1020/jenktui/src/context"
)

type keyMap struct{ selectItem, up, down, close key.Binding }

func newKeyMap(conf context.CommandPaletteDialogKeyMapConfig) keyMap {
	binding := func(keys context.KeyBinding, label string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keys.HelpText(), label))
	}
	return keyMap{
		selectItem: binding(conf.Select, "run"),
		up:         binding(conf.Up, "up"),
		down:       binding(conf.Down, "down"),
		close:      binding(conf.Close, "cancel"),
	}
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.selectItem, k.close, k.up, k.down}
}

func (k keyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }
