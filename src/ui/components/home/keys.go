package home

import (
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/key"
)

type KeyMap struct {
	util.GlobalKeyMap
}

func UserKeyMap(globalKM util.GlobalKeyMap) KeyMap {
	return KeyMap{
		GlobalKeyMap: globalKM,
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{}
}
