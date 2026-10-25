package favorites

import (
	"github.com/atm1020/jenktui/src/context"

	"charm.land/bubbles/v2/key"
)

type KeyMap struct {
	Down           key.Binding
	Up             key.Binding
	Enter          key.Binding
	Delete         key.Binding
	JobsTab        key.Binding
	FoldersTab     key.Binding
	MultibranchTab key.Binding
	OpenRuns       key.Binding
	Search         key.Binding
}

func UserKeyMap(conf context.FavoritesKeyMapConfig) KeyMap {
	return KeyMap{
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
			key.WithHelp(conf.Down.HelpText(), "down"),
		),
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
			key.WithHelp(conf.Up.HelpText(), "up"),
		),
		Enter: key.NewBinding(
			key.WithKeys(conf.Enter...),
			key.WithHelp(conf.Enter.HelpText(), "open"),
		),
		Delete: key.NewBinding(
			key.WithKeys(conf.Delete...),
			key.WithHelp(conf.Delete.HelpText(), "remove favorite"),
		),
		JobsTab: key.NewBinding(
			key.WithKeys(conf.JobsTab...),
			key.WithHelp(conf.JobsTab.HelpText(), "jobs tab"),
		),
		FoldersTab: key.NewBinding(
			key.WithKeys(conf.FoldersTab...),
			key.WithHelp(conf.FoldersTab.HelpText(), "folders tab"),
		),
		MultibranchTab: key.NewBinding(
			key.WithKeys(conf.MultibranchTab...),
			key.WithHelp(conf.MultibranchTab.HelpText(), "multibranch tab"),
		),
		OpenRuns: key.NewBinding(
			key.WithKeys(conf.OpenRuns...),
			key.WithHelp(conf.OpenRuns.HelpText(), "view run history"),
		),
		Search: key.NewBinding(
			key.WithKeys(conf.Search...),
			key.WithHelp(conf.Search.HelpText(), "search"),
		),
	}
}

func (k KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Enter, k.Search, k.JobsTab, k.FoldersTab, k.MultibranchTab, k.OpenRuns, k.Delete}
}

func (k KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Up, k.Down, k.Enter, k.Search},
		{k.JobsTab, k.FoldersTab, k.MultibranchTab},
		{k.OpenRuns, k.Delete},
	}
}
