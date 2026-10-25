package util

import (
	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
)

type GlobalKeyMap struct {
	Quit           key.Binding
	HelpToggle     key.Binding
	Help           key.Binding
	Back           key.Binding
	Forward        key.Binding
	GlobalSearch   key.Binding
	ThemeSelector  key.Binding
	ServerSelector key.Binding
	MyRuns         key.Binding
	Favorites      key.Binding
}

func UserKeyMap(conf context.KeyMapConfig) GlobalKeyMap {
	g := conf.Global
	return GlobalKeyMap{
		Quit: key.NewBinding(
			key.WithKeys(g.Quit...),
			key.WithHelp(g.Quit.HelpText(), "quit"),
		),
		HelpToggle: key.NewBinding(
			key.WithKeys(g.HelpToggle...),
			key.WithHelp(g.HelpToggle.HelpText(), "help"),
		),
		Help: key.NewBinding(
			key.WithKeys(g.Help...),
			key.WithHelp(g.Help.HelpText(), "more"),
		),
		Back: key.NewBinding(
			key.WithKeys(g.Back...),
			key.WithHelp(g.Back.HelpText(), "back"),
		),
		Forward: key.NewBinding(
			key.WithKeys(g.Forward...),
			key.WithHelp(g.Forward.HelpText(), "forward"),
		),
		GlobalSearch: key.NewBinding(
			key.WithKeys(g.Globalsearch...),
			key.WithHelp(g.Globalsearch.HelpText(), "global search"),
		),
		ThemeSelector: key.NewBinding(
			key.WithKeys(g.ThemeSelector...),
			key.WithHelp(g.ThemeSelector.HelpText(), "change theme"),
		),
		ServerSelector: key.NewBinding(
			key.WithKeys(g.ServerSelector...),
			key.WithHelp(g.ServerSelector.HelpText(), "change server"),
		),
		MyRuns: key.NewBinding(
			key.WithKeys(g.MyRuns...),
			key.WithHelp(g.MyRuns.HelpText(), "my runs"),
		),
		Favorites: key.NewBinding(
			key.WithKeys(g.Favorites...),
			key.WithHelp(g.Favorites.HelpText(), "favorites"),
		),
	}
}

func (k GlobalKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Quit, k.HelpToggle, k.Help,
	}
}

func (k GlobalKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit, k.HelpToggle, k.Help},
		{k.Back, k.Forward, k.GlobalSearch},
		{k.ThemeSelector, k.ServerSelector, k.MyRuns, k.Favorites},
	}
}

// LocalWithHint appends a hint binding (e.g. ctrl+g "more") to the local KeyMap.
type LocalWithHint struct {
	Local help.KeyMap
	Hint  key.Binding
}

func (l LocalWithHint) ShortHelp() []key.Binding {
	return append(l.Local.ShortHelp(), l.Hint)
}

func (l LocalWithHint) FullHelp() [][]key.Binding {
	return append(l.Local.FullHelp(), []key.Binding{l.Hint})
}

// MergedViewKeyMap combines two local KeyMaps (e.g. a parent view + sub-component).
type MergedViewKeyMap struct {
	Primary   help.KeyMap
	Secondary help.KeyMap
}

func (m MergedViewKeyMap) ShortHelp() []key.Binding {
	return append(m.Primary.ShortHelp(), m.Secondary.ShortHelp()...)
}

func (m MergedViewKeyMap) FullHelp() [][]key.Binding {
	return append(m.Primary.FullHelp(), m.Secondary.FullHelp()...)
}

// MergedKeyMap combines a local KeyMap with GlobalKeyMap for the full help level.
type MergedKeyMap struct {
	Local  help.KeyMap
	Global GlobalKeyMap
}

func (m MergedKeyMap) ShortHelp() []key.Binding {
	return m.Local.ShortHelp()
}

func (m MergedKeyMap) FullHelp() [][]key.Binding {
	return append(m.Local.FullHelp(), m.Global.FullHelp()...)
}
