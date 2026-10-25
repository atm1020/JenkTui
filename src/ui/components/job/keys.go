package job

import (
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/key"
)

type CommonKeyMap struct {
	util.GlobalKeyMap
	Up                 key.Binding
	Down               key.Binding
	BuildsView         key.Binding
	StagesView         key.Binding
	Replay             key.Binding
	Logs               key.Binding
	OpenBuildBrowser   key.Binding
	OpenJobBrowser     key.Binding
	DownloadAttachment key.Binding
	OpenBuildView      key.Binding
	CancelBuild        key.Binding
	TriggerBuild       key.Binding
	Favorite           key.Binding
}

func GetCommonKeyMap() *CommonKeyMap {
	return &CommonKeyMap{
		Up: key.NewBinding(
			key.WithKeys("up", "k"),
			key.WithHelp("↑/k", "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys("down", "j"),
			key.WithHelp("↓/j", "move down"),
		),

		BuildsView: key.NewBinding(
			key.WithKeys("B"),
			key.WithHelp("B/shift+b", "builds"),
		),

		StagesView: key.NewBinding(
			key.WithKeys("S"),
			key.WithHelp("S/shift+s", "stages"),
		),

		Replay: key.NewBinding(
			key.WithKeys("r"),
			key.WithHelp("r", "replay job"),
		),
		Logs: key.NewBinding(
			key.WithKeys("L"),
			key.WithHelp("L", "view logs"),
		),
		OpenBuildBrowser: key.NewBinding(
			key.WithKeys("o"),
			key.WithHelp("o", "open build in browser"),
		),
		OpenJobBrowser: key.NewBinding(
			key.WithKeys("O"),
			key.WithHelp("O", "open job in browser"),
		),
		DownloadAttachment: key.NewBinding(
			key.WithKeys("A"),
			key.WithKeys("A", "download attachment"),
		),
		OpenBuildView: key.NewBinding(
			key.WithKeys("enter"),
			key.WithHelp("enter", "open build view"),
		),
		CancelBuild: key.NewBinding(
			key.WithKeys("x"),
			key.WithHelp("x", "cancel build"),
		),
		TriggerBuild: key.NewBinding(
			key.WithKeys("b"),
			key.WithHelp("b", "trigger build"),
		),
		Favorite: key.NewBinding(
			key.WithKeys("F"),
			key.WithHelp("F", "toggle favorite"),
		),
	}
}

func UserCommonKeyMap(conf context.JobKeyMapConfig) *CommonKeyMap {
	return &CommonKeyMap{
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
			key.WithHelp(conf.Up.HelpText(), "move up"),
		),
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
			key.WithHelp(conf.Down.HelpText(), "move down"),
		),
		BuildsView: key.NewBinding(
			key.WithKeys(conf.BuildsView...),
			key.WithHelp(conf.BuildsView.HelpText(), "builds"),
		),
		StagesView: key.NewBinding(
			key.WithKeys(conf.StagesView...),
			key.WithHelp(conf.StagesView.HelpText(), "stages"),
		),
		Replay: key.NewBinding(
			key.WithKeys(conf.Replay...),
			key.WithHelp(conf.Replay.HelpText(), "replay job"),
		),
		Logs: key.NewBinding(
			key.WithKeys(conf.Logs...),
			key.WithHelp(conf.Logs.HelpText(), "view logs"),
		),
		OpenBuildBrowser: key.NewBinding(
			key.WithKeys(conf.OpenBuildBrowser...),
			key.WithHelp(conf.OpenBuildBrowser.HelpText(), "open build in browser"),
		),
		OpenJobBrowser: key.NewBinding(
			key.WithKeys(conf.OpenJobBrowser...),
			key.WithHelp(conf.OpenJobBrowser.HelpText(), "open job in browser"),
		),
		DownloadAttachment: key.NewBinding(
			key.WithKeys(conf.DownloadAttachment...),
			key.WithHelp(conf.DownloadAttachment.HelpText(), "download attachment"),
		),
		OpenBuildView: key.NewBinding(
			key.WithKeys(conf.OpenBuildView...),
			key.WithHelp(conf.OpenBuildView.HelpText(), "open build view"),
		),
		CancelBuild: key.NewBinding(
			key.WithKeys(conf.CancelBuild...),
			key.WithHelp(conf.CancelBuild.HelpText(), "cancel build"),
		),
		TriggerBuild: key.NewBinding(
			key.WithKeys(conf.TriggerBuild...),
			key.WithHelp(conf.TriggerBuild.HelpText(), "trigger build"),
		),
		Favorite: key.NewBinding(
			key.WithKeys(conf.Favorite...),
			key.WithHelp(conf.Favorite.HelpText(), "toggle favorite"),
		),
	}
}

type StageViewKeyMap struct {
	util.GlobalKeyMap
	*CommonKeyMap
}

func GetStageViewKeyMap() StageViewKeyMap {
	return StageViewKeyMap{
		CommonKeyMap: GetCommonKeyMap(),
	}
}

func UserStageViewKeyMap(globalKM util.GlobalKeyMap, conf context.JobKeyMapConfig) StageViewKeyMap {
	return StageViewKeyMap{
		GlobalKeyMap: globalKM,
		CommonKeyMap: UserCommonKeyMap(conf),
	}
}

func (sk StageViewKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{}
}

func (sk StageViewKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{sk.Up, sk.Down},
		{sk.Replay, sk.Logs, sk.CancelBuild, sk.TriggerBuild},
		{sk.BuildsView, sk.OpenBuildView, sk.Favorite},
		{sk.OpenBuildBrowser, sk.OpenJobBrowser},
	}
}

type BuildListViewKeyMap struct {
	util.GlobalKeyMap
	*CommonKeyMap

	Search key.Binding
}

func GetBuildListViewKeyMap() BuildListViewKeyMap {
	return BuildListViewKeyMap{
		CommonKeyMap: GetCommonKeyMap(),

		Search: key.NewBinding(
			key.WithKeys("/"),
			key.WithHelp("/", "search builds"),
		),
	}
}

func UserBuildListViewKeyMap(globalKM util.GlobalKeyMap, conf context.JobKeyMapConfig) BuildListViewKeyMap {
	return BuildListViewKeyMap{
		GlobalKeyMap: globalKM,
		CommonKeyMap: UserCommonKeyMap(conf),
		Search: key.NewBinding(
			key.WithKeys(conf.Search...),
			key.WithHelp(conf.Search.HelpText(), "search builds"),
		),
	}
}

func (bl BuildListViewKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{}
}

func (bl BuildListViewKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{bl.StagesView, bl.Search},
		{bl.OpenBuildBrowser, bl.OpenJobBrowser},
	}
}
