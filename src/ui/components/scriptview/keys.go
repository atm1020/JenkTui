package scriptview

import (
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/key"
)

type KeyMap struct {
	util.GlobalKeyMap
	Run   key.Binding
	Edit  key.Binding
	Up    key.Binding
	Down  key.Binding
	Left  key.Binding
	Right key.Binding
}

func UserKeyMap(globalKM util.GlobalKeyMap, conf context.ScriptviewKeyMapConfig) KeyMap {
	return KeyMap{
		GlobalKeyMap: globalKM,
		Run: key.NewBinding(
			key.WithKeys(conf.Run...),
			key.WithHelp(conf.Run.HelpText(), "run script"),
		),
		Edit: key.NewBinding(
			key.WithKeys(conf.Edit...),
			key.WithHelp(conf.Edit.HelpText(), "edit script"),
		),
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
			key.WithHelp(conf.Up.HelpText(), "scroll up"),
		),
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
			key.WithHelp(conf.Down.HelpText(), "scroll down"),
		),
		Left: key.NewBinding(
			key.WithKeys(conf.Left...),
			key.WithHelp(conf.Left.HelpText(), "scroll left"),
		),
		Right: key.NewBinding(
			key.WithKeys(conf.Right...),
			key.WithHelp(conf.Right.HelpText(), "scroll right"),
		),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Run, k.Edit},
		{k.Up, k.Down, k.Left, k.Right},
	}
}
