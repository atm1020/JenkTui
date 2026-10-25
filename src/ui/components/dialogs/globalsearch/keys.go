package globalsearch

import (
	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/key"
)

type KeyMap struct {
	Quit        key.Binding
	Help        key.Binding
	FocusUp     key.Binding
	FocusDown   key.Binding
	ToggleFocus key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Help: key.NewBinding(
			key.WithKeys("ctrl+g"),
			key.WithHelp("ctrl+g", "more"),
		),
		Quit: key.NewBinding(
			key.WithKeys("ctrl+c"),
			key.WithHelp("ctrl+c", "quit"),
		),
		ToggleFocus: key.NewBinding(
			key.WithKeys("tab"),
			key.WithHelp("tab", "switch"),
		),

		FocusUp: key.NewBinding(
			key.WithKeys("ctrl+k"),
			key.WithHelp("ctrl+k", "focus up"),
		),
		FocusDown: key.NewBinding(
			key.WithKeys("ctrl+j"),
			key.WithHelp("ctrl+j", "focus down"),
		),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		k.Quit, k.ToggleFocus, k.Help,
	}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Quit, k.ToggleFocus, k.Help},
		{k.FocusUp, k.FocusDown},
	}
}

type SearchInputKeyMap struct {
	KeyMap
	Search     key.Binding
	ClearInput key.Binding
}

func (sk SearchInputKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		sk.Search, sk.ToggleFocus, sk.Quit, sk.Help,
	}
}

func (sk SearchInputKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{sk.Search, sk.ToggleFocus, sk.Quit, sk.Help},
		{sk.ClearInput, sk.FocusUp, sk.FocusDown},
	}
}

func GetSearchInputKeyMap() SearchInputKeyMap {
	return SearchInputKeyMap{
		Search: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "search"),
		),
		ClearInput: key.NewBinding(
			key.WithKeys("ctrl+r"),
			key.WithHelp("ctrl+r", "clear input"),
		),
		KeyMap: DefaultKeyMap(),
	}
}

type SearchResultKeyMap struct {
	Down     key.Binding
	Up       key.Binding
	NextPage key.Binding
	PrevPage key.Binding
	Select   key.Binding
	Filter   key.Binding
	KeyMap
}

func (sr SearchResultKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{
		sr.Down, sr.Up, sr.Filter, sr.ToggleFocus, sr.Select, sr.Help,
	}
}

func (sr SearchResultKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{sr.Down, sr.Up, sr.Filter, sr.ToggleFocus, sr.Select, sr.Help},
		{sr.NextPage, sr.PrevPage, sr.FocusUp, sr.FocusDown, sr.Quit},
	}
}

func GetSearchResultKeyMap() SearchResultKeyMap {
	return SearchResultKeyMap{
		Down: key.NewBinding(
			key.WithKeys("j"),
			key.WithHelp("j", "down"),
		),
		Up: key.NewBinding(
			key.WithKeys("k"),
			key.WithHelp("k", "up"),
		),
		NextPage: key.NewBinding(
			key.WithKeys("l"),
			key.WithHelp("l", "next page"),
		),
		PrevPage: key.NewBinding(
			key.WithKeys("h"),
			key.WithHelp("h", "previous page"),
		),
		Select: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "select"),
		),
		Filter: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "filter"),
		),
		KeyMap: DefaultKeyMap(),
	}
}

// UserKeyMap creates a KeyMap from user configuration
func UserKeyMap(conf context.GlobalsearchDialogKeyMapConfig) KeyMap {
	return KeyMap{
		Help: key.NewBinding(
			key.WithKeys(conf.Help...),
			key.WithHelp(conf.Help.HelpText(), "more"),
		),
		Quit: key.NewBinding(
			key.WithKeys(conf.Quit...),
			key.WithHelp(conf.Quit.HelpText(), "quit"),
		),
		ToggleFocus: key.NewBinding(
			key.WithKeys(conf.ToggleFocus...),
			key.WithHelp(conf.ToggleFocus.HelpText(), "switch"),
		),
		FocusUp: key.NewBinding(
			key.WithKeys(conf.FocusUp...),
			key.WithHelp(conf.FocusUp.HelpText(), "focus up"),
		),
		FocusDown: key.NewBinding(
			key.WithKeys(conf.FocusDown...),
			key.WithHelp(conf.FocusDown.HelpText(), "focus down"),
		),
	}
}

// UserSearchInputKeyMap creates a SearchInputKeyMap from user configuration
func UserSearchInputKeyMap(conf context.GlobalsearchDialogKeyMapConfig) SearchInputKeyMap {
	return SearchInputKeyMap{
		Search: key.NewBinding(
			key.WithKeys(conf.Search...),
			key.WithHelp(conf.Search.HelpText(), "search"),
		),
		ClearInput: key.NewBinding(
			key.WithKeys(conf.ClearInput...),
			key.WithHelp(conf.ClearInput.HelpText(), "clear input"),
		),
		KeyMap: UserKeyMap(conf),
	}
}

// UserSearchResultKeyMap creates a SearchResultKeyMap from user configuration
func UserSearchResultKeyMap(conf context.GlobalsearchDialogKeyMapConfig) SearchResultKeyMap {
	return SearchResultKeyMap{
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
			key.WithHelp(conf.Down.HelpText(), "down"),
		),
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
			key.WithHelp(conf.Up.HelpText(), "up"),
		),
		NextPage: key.NewBinding(
			key.WithKeys(conf.NextPage...),
			key.WithHelp(conf.NextPage.HelpText(), "next page"),
		),
		PrevPage: key.NewBinding(
			key.WithKeys(conf.PrevPage...),
			key.WithHelp(conf.PrevPage.HelpText(), "previous page"),
		),
		Select: key.NewBinding(
			key.WithKeys(conf.Select...),
			key.WithHelp(conf.Select.HelpText(), "select"),
		),
		Filter: key.NewBinding(
			key.WithKeys(conf.Filter...),
			key.WithHelp(conf.Filter.HelpText(), "filter"),
		),
		KeyMap: UserKeyMap(conf),
	}
}
