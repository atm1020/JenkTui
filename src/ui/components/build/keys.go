package build

import (
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/key"
)

// KeyMap defines the keybindings for the build component (stage view + stage details as one screen)
type KeyMap struct {
	util.GlobalKeyMap
	// Stage navigation
	NextStage key.Binding
	PrevStage key.Binding
	// Stage details navigation
	Up                   key.Binding
	Down                 key.Binding
	OpenStepLog          key.Binding
	OpenFullLog          key.Binding
	DownloadArtifacts    key.Binding
	DownloadAllArtifacts key.Binding
	CopyNode             key.Binding
	OpenJob              key.Binding
	Replay               key.Binding
	ShowParams           key.Binding
	OpenBuildBrowser     key.Binding
	OpenJobBrowser       key.Binding
	TriggerBuild         key.Binding
	CancelBuild          key.Binding
}

func (k *KeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.PrevStage, k.NextStage, k.Up, k.Down, k.OpenStepLog, k.OpenFullLog, k.DownloadArtifacts, k.DownloadAllArtifacts, k.CopyNode, k.OpenJob, k.Replay, k.ShowParams, k.OpenBuildBrowser, k.OpenJobBrowser, k.TriggerBuild, k.CancelBuild}
}

func (k *KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.PrevStage, k.NextStage},
		{k.Up, k.Down, k.OpenStepLog},
		{k.OpenFullLog, k.DownloadArtifacts, k.DownloadAllArtifacts, k.CopyNode},
		{k.OpenJob, k.Replay, k.ShowParams},
		{k.TriggerBuild, k.CancelBuild},
		{k.OpenBuildBrowser, k.OpenJobBrowser},
	}
}

// UserKeyMap creates a KeyMap from user configuration
func UserKeyMap(conf context.BuildKeyMapConfig) *KeyMap {
	return &KeyMap{
		NextStage: key.NewBinding(
			key.WithKeys(conf.NextStage...),
			key.WithHelp(conf.NextStage.HelpText(), "next stage"),
		),
		PrevStage: key.NewBinding(
			key.WithKeys(conf.PrevStage...),
			key.WithHelp(conf.PrevStage.HelpText(), "prev stage"),
		),
		Up: key.NewBinding(
			key.WithKeys(conf.Up...),
			key.WithHelp(conf.Up.HelpText(), "up"),
		),
		Down: key.NewBinding(
			key.WithKeys(conf.Down...),
			key.WithHelp(conf.Down.HelpText(), "down"),
		),
		OpenStepLog: key.NewBinding(
			key.WithKeys(conf.OpenStepLog...),
			key.WithHelp(conf.OpenStepLog.HelpText(), "open step log"),
		),
		OpenFullLog: key.NewBinding(
			key.WithKeys(conf.OpenFullLog...),
			key.WithHelp(conf.OpenFullLog.HelpText(), "open full log"),
		),
		DownloadArtifacts: key.NewBinding(
			key.WithKeys(conf.DownloadArtifacts...),
			key.WithHelp(conf.DownloadArtifacts.HelpText(), "choose artifacts"),
		),
		DownloadAllArtifacts: key.NewBinding(
			key.WithKeys(conf.DownloadAllArtifacts...),
			key.WithHelp(conf.DownloadAllArtifacts.HelpText(), "download all artifacts"),
		),
		CopyNode: key.NewBinding(
			key.WithKeys(conf.CopyNode...),
			key.WithHelp(conf.CopyNode.HelpText(), "copy node"),
		),
		OpenJob: key.NewBinding(
			key.WithKeys(conf.OpenJob...),
			key.WithHelp(conf.OpenJob.HelpText(), "open job"),
		),
		Replay: key.NewBinding(
			key.WithKeys(conf.Replay...),
			key.WithHelp(conf.Replay.HelpText(), "replay job"),
		),
		ShowParams: key.NewBinding(
			key.WithKeys(conf.ShowParams...),
			key.WithHelp(conf.ShowParams.HelpText(), "show params"),
		),
		OpenBuildBrowser: key.NewBinding(
			key.WithKeys(conf.OpenBuildBrowser...),
			key.WithHelp(conf.OpenBuildBrowser.HelpText(), "open build in browser"),
		),
		OpenJobBrowser: key.NewBinding(
			key.WithKeys(conf.OpenJobBrowser...),
			key.WithHelp(conf.OpenJobBrowser.HelpText(), "open job in browser"),
		),
		TriggerBuild: key.NewBinding(
			key.WithKeys(conf.TriggerBuild...),
			key.WithHelp(conf.TriggerBuild.HelpText(), "trigger build"),
		),
		CancelBuild: key.NewBinding(
			key.WithKeys(conf.CancelBuild...),
			key.WithHelp(conf.CancelBuild.HelpText(), "cancel build"),
		),
	}
}
