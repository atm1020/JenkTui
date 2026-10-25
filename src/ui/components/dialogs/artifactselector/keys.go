package artifactselector

import (
	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/key"
)

// KeyMap defines the keyboard bindings for the artifact selector dialog.
type KeyMap struct {
	Up          key.Binding
	Down        key.Binding
	Toggle      key.Binding
	Download    key.Binding
	DownloadAll key.Binding
	Close       key.Binding
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Up, k.Down, k.Toggle, k.Download, k.DownloadAll, k.Close}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Toggle},
		{k.Download, k.DownloadAll, k.Close},
	}
}

// UserKeyMap creates a KeyMap from user configuration.
func UserKeyMap(conf context.ArtifactSelectorDialogKeyMapConfig) KeyMap {
	return KeyMap{
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
			key.WithHelp(conf.Up.HelpText(), "up"),
		),
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
			key.WithHelp(conf.Down.HelpText(), "down"),
		),
		Toggle: key.NewBinding(
			key.WithKeys(conf.Toggle...),
			key.WithHelp(conf.Toggle.HelpText(), "toggle"),
		),
		Download: key.NewBinding(
			key.WithKeys(conf.Download...),
			key.WithHelp(conf.Download.HelpText(), "selected"),
		),
		DownloadAll: key.NewBinding(
			key.WithKeys(conf.DownloadAll...),
			key.WithHelp(conf.DownloadAll.HelpText(), "all"),
		),
		Close: key.NewBinding(
			key.WithKeys(conf.Close...),
			key.WithHelp(conf.Close.HelpText(), "close"),
		),
	}
}
