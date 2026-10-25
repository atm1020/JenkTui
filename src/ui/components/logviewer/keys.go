package logviewer

import (
	"slices"
	"strings"

	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/key"
)

type KeyMap struct {
	Up         key.Binding
	Down       key.Binding
	Right      key.Binding
	Left       key.Binding
	PageUp     key.Binding
	PageDown   key.Binding
	GoToTop    key.Binding
	GoToBottom key.Binding

	Search key.Binding
	Enter  key.Binding

	Next key.Binding
	Prev key.Binding

	Wrap key.Binding

	ComponentUp   key.Binding
	ComponentDown key.Binding

	VisualMode     key.Binding
	Yank           key.Binding
	ExitVisualMode key.Binding
}

func DefaultKeyMap() KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys("k", "up"),
			key.WithHelp("k/up", "scroll up"),
		),
		Down: key.NewBinding(
			key.WithKeys("j", "down"),
			key.WithHelp("j/up", "scroll down"),
		),
		PageDown: key.NewBinding(
			key.WithKeys("}"),
			key.WithHelp("}", "page up"),
		),
		PageUp: key.NewBinding(
			key.WithKeys("{"),
			key.WithHelp("{", "page down"),
		),
		GoToTop: key.NewBinding(
			key.WithKeys("gg", "home"),
			key.WithHelp("gg", "go to top"),
		),
		GoToBottom: key.NewBinding(
			key.WithKeys("G", "end"),
			key.WithHelp("G", "go to bottom"),
		),
		Right: key.NewBinding(
			key.WithKeys("l", "right"),
			key.WithHelp("l/right", "scroll right"),
		),
		Left: key.NewBinding(
			key.WithKeys("h", "left"),
			key.WithHelp("h/left", "scroll left"),
		),
		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search in logs"),
		),
		Enter: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "run search"),
		),

		ComponentUp: key.NewBinding(
			key.WithKeys("ctrl+k"),
			key.WithHelp("ctrl+k", "component up"),
		),
		ComponentDown: key.NewBinding(
			key.WithKeys("ctrl+j"),
			key.WithHelp("ctrl+j", "component down"),
		),
		Next: key.NewBinding(
			key.WithKeys("n"),
			key.WithHelp("n", "next match"),
		),
		Prev: key.NewBinding(
			key.WithKeys("p"),
			key.WithHelp("p", "prev match"),
		),
		Wrap: key.NewBinding(
			key.WithKeys("w"),
			key.WithHelp("w", "toggle line wrap"),
		),
		VisualMode: key.NewBinding(
			key.WithKeys("v"),
			key.WithHelp("v", "visual select"),
		),
		Yank: key.NewBinding(
			key.WithKeys("y"),
			key.WithHelp("y", "yank selection"),
		),
		ExitVisualMode: key.NewBinding(
			key.WithKeys("esc"),
			key.WithHelp("esc", "exit visual mode"),
		),
	}
}

func UserKeyMap(conf context.LogviewerKeyMapConfig) KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
			key.WithHelp(conf.Up.HelpText(), "scroll up"),
		),
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
			key.WithHelp(conf.Down.HelpText(), "scroll down"),
		),
		PageDown: key.NewBinding(
			key.WithKeys(conf.PageDown...),
			key.WithHelp(conf.PageDown.HelpText(), "page down"),
		),
		PageUp: key.NewBinding(
			key.WithKeys(conf.PageUp...),
			key.WithHelp(conf.PageUp.HelpText(), "page up"),
		),
		Right: key.NewBinding(
			key.WithKeys(conf.Right...),
			key.WithHelp(conf.Right.HelpText(), "scroll right"),
		),
		Left: key.NewBinding(
			key.WithKeys(conf.Left...),
			key.WithHelp(conf.Left.HelpText(), "scroll left"),
		),
		GoToTop: key.NewBinding(
			key.WithKeys(conf.GoToTop...),
			key.WithHelp(conf.GoToTop.HelpText(), "go to top"),
		),
		GoToBottom: key.NewBinding(
			key.WithKeys(conf.GoToBottom...),
			key.WithHelp(conf.GoToBottom.HelpText(), "go to bottom"),
		),
		Search: key.NewBinding(
			key.WithKeys(conf.Search...),
			key.WithHelp(conf.Search.HelpText(), "search in logs"),
		),
		Enter: key.NewBinding(
			key.WithKeys(conf.Enter...),
			key.WithHelp(conf.Enter.HelpText(), "run search"),
		),
		ComponentUp: key.NewBinding(
			key.WithKeys(conf.ComponentUp...),
			key.WithHelp(conf.ComponentUp.HelpText(), "component up"),
		),
		ComponentDown: key.NewBinding(
			key.WithKeys(conf.ComponentDown...),
			key.WithHelp(conf.ComponentDown.HelpText(), "component down"),
		),
		Next: key.NewBinding(
			key.WithKeys(conf.Next...),
			key.WithHelp(conf.Next.HelpText(), "next match"),
		),
		Prev: key.NewBinding(
			key.WithKeys(conf.Prev...),
			key.WithHelp(conf.Prev.HelpText(), "prev match"),
		),
		Wrap: key.NewBinding(
			key.WithKeys(conf.Wrap...),
			key.WithHelp(conf.Wrap.HelpText(), "toggle line wrap"),
		),
		VisualMode: key.NewBinding(
			key.WithKeys(conf.VisualMode...),
			key.WithHelp(conf.VisualMode.HelpText(), "visual select"),
		),
		Yank: key.NewBinding(
			key.WithKeys(conf.Yank...),
			key.WithHelp(conf.Yank.HelpText(), "yank selection"),
		),
		ExitVisualMode: key.NewBinding(
			key.WithKeys(conf.ExitVisualMode...),
			key.WithHelp(conf.ExitVisualMode.HelpText(), "exit visual mode"),
		),
	}
}

func (km KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{km.Search, km.Next, km.Prev, km.PageUp, km.PageDown}
}

func (km KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{km.Up, km.Down, km.PageUp, km.PageDown, km.GoToTop, km.GoToBottom, km.Left, km.Right, km.Wrap},
		{km.VisualMode, km.Yank, km.ExitVisualMode},
	}
}

// matchesKey reports whether the keys typed so far match the binding exactly.
// It takes the typed string rather than a tea.KeyMsg so a multi-key sequence
// (see isSequencePrefix) can be matched once its final key arrives.
func matchesKey(typed string, b key.Binding) bool {
	if !b.Enabled() {
		return false
	}
	return slices.Contains(b.Keys(), typed)
}

// isSequencePrefix reports whether typed begins, but does not yet complete, one
// of the binding's multi-key sequences. Bubble Tea delivers one key per message
// and key.Matches compares whole strings, so vim's "gg" needs its first press
// remembered until the second arrives.
//
// Only a repeated single rune ("gg", "zz") counts as a sequence; named keys are
// left to whole-string matching, so a half-typed "en" is never mistaken for the
// start of "end".
func isSequencePrefix(typed string, b key.Binding) bool {
	if !b.Enabled() {
		return false
	}
	for _, k := range b.Keys() {
		if isRepeatedRune(k) && len(typed) < len(k) && strings.HasPrefix(k, typed) {
			return true
		}
	}
	return false
}

func isRepeatedRune(k string) bool {
	runes := []rune(k)
	if len(runes) < 2 {
		return false
	}
	for _, r := range runes[1:] {
		if r != runes[0] {
			return false
		}
	}
	return true
}
