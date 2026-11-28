package build

import (
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/key"
)

// KeyMap defines the keybindings for the build component (stages, steps, and inline log as one screen)
type KeyMap struct {
	util.GlobalKeyMap
	// Stage navigation
	NextStage key.Binding
	PrevStage key.Binding
	// Stage details & pane navigation
	Up                   key.Binding
	Down                 key.Binding
	SwitchPane           key.Binding
	FocusBuild           key.Binding
	ZoomLog              key.Binding
	LogScopeFull         key.Binding
	LogScopeStage        key.Binding
	LogScopeStep         key.Binding
	LogScopeOutside      key.Binding
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
	return []key.Binding{k.PrevStage, k.NextStage, k.Up, k.Down, k.SwitchPane, k.ZoomLog, k.OpenStepLog, k.OpenFullLog, k.DownloadArtifacts, k.DownloadAllArtifacts, k.CopyNode, k.OpenJob, k.Replay, k.ShowParams, k.OpenBuildBrowser, k.OpenJobBrowser, k.TriggerBuild, k.CancelBuild}
}

func (k *KeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.PrevStage, k.NextStage, k.SwitchPane, k.FocusBuild},
		{k.Up, k.Down, k.OpenStepLog, k.OpenFullLog, k.ZoomLog},
		{k.LogScopeFull, k.LogScopeStage, k.LogScopeStep, k.LogScopeOutside},
		{k.DownloadArtifacts, k.DownloadAllArtifacts, k.CopyNode},
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
		SwitchPane: key.NewBinding(
			key.WithKeys(conf.SwitchPane...),
			key.WithHelp(conf.SwitchPane.HelpText(), "switch pane"),
		),
		FocusBuild: key.NewBinding(
			key.WithKeys(conf.FocusBuild...),
			key.WithHelp(conf.FocusBuild.HelpText(), "focus steps"),
		),
		ZoomLog: key.NewBinding(
			key.WithKeys(conf.ZoomLog...),
			key.WithHelp(conf.ZoomLog.HelpText(), "zoom log"),
		),
		LogScopeFull: key.NewBinding(
			key.WithKeys(conf.LogScopeFull...),
			key.WithHelp(conf.LogScopeFull.HelpText(), "full log"),
		),
		LogScopeStage: key.NewBinding(
			key.WithKeys(conf.LogScopeStage...),
			key.WithHelp(conf.LogScopeStage.HelpText(), "stage log"),
		),
		LogScopeStep: key.NewBinding(
			key.WithKeys(conf.LogScopeStep...),
			key.WithHelp(conf.LogScopeStep.HelpText(), "step log"),
		),
		LogScopeOutside: key.NewBinding(
			key.WithKeys(conf.LogScopeOutside...),
			key.WithHelp(conf.LogScopeOutside.HelpText(), "outside-stage log"),
		),
		OpenStepLog: key.NewBinding(
			key.WithKeys(conf.OpenStepLog...),
			key.WithHelp(conf.OpenStepLog.HelpText(), "focus step log"),
		),
		OpenFullLog: key.NewBinding(
			key.WithKeys(conf.OpenFullLog...),
			key.WithHelp(conf.OpenFullLog.HelpText(), "toggle full log"),
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
