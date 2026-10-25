package serversetup

import (
	"charm.land/bubbles/v2/key"
	"github.com/atm1020/jenktui/src/context"
)

type KeyMap struct{ Next, Previous, Submit, Advanced, Close key.Binding }

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Next, k.Previous, k.Submit, k.Advanced, k.Close}
}
func (k KeyMap) FullHelp() [][]key.Binding { return [][]key.Binding{k.ShortHelp()} }
func UserKeyMap(c context.ServerSetupDialogKeyMapConfig) KeyMap {
	binding := func(keys context.KeyBinding, text string) key.Binding {
		return key.NewBinding(key.WithKeys(keys...), key.WithHelp(keys.HelpText(), text))
	}
	return KeyMap{Next: binding(c.Next, "next"), Previous: binding(c.Previous, "previous"), Submit: binding(c.Submit, "test & save"), Advanced: binding(c.Advanced, "advanced"), Close: binding(c.Close, "cancel")}
}
