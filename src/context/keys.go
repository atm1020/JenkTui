package context

import (
	"encoding/json"
	"strings"
)

// KeyBinding holds pre-split key binding strings.
// Accepts both "ctrl+g" (string) and ["ctrl+g","g"] (array) in JSON.
type KeyBinding []string

func (kb *KeyBinding) UnmarshalJSON(data []byte) error {
	// Try array first
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		*kb = arr
		return nil
	}
	// Fall back to comma-separated string
	var s string
	if err := json.Unmarshal(data, &s); err != nil {
		return err
	}
	*kb = strings.Split(s, ",")
	return nil
}

func (kb KeyBinding) MarshalJSON() ([]byte, error) {
	return json.Marshal(strings.Join(kb, ","))
}

// HelpText returns a display string for help views (e.g. "up,k").
func (kb KeyBinding) HelpText() string {
	return strings.Join(kb, ",")
}

type KeyMap struct{}

// Global keymaps
type GlobalKeyMapConfig struct {
	Globalsearch   KeyBinding `json:"global_search"`
	Back           KeyBinding `json:"back"`
	Forward        KeyBinding `json:"forward"`
	Quit           KeyBinding `json:"quit"`
	HelpToggle     KeyBinding `json:"help_toggle"`
	Help           KeyBinding `json:"help"`
	ThemeSelector  KeyBinding `json:"theme_selector"`
	ServerSelector KeyBinding `json:"server_selector"`
	MyRuns         KeyBinding `json:"my_runs"`
	Favorites      KeyBinding `json:"favorites"`
}

// Dialog keymaps
type GlobalsearchDialogKeyMapConfig struct {
	Help        KeyBinding `json:"help"`
	Quit        KeyBinding `json:"quit"`
	ToggleFocus KeyBinding `json:"toggle_focus"`
	FocusUp     KeyBinding `json:"focus_up"`
	FocusDown   KeyBinding `json:"focus_down"`
	Search      KeyBinding `json:"search"`
	ClearInput  KeyBinding `json:"clear_input"`
	Down        KeyBinding `json:"down"`
	Up          KeyBinding `json:"up"`
	NextPage    KeyBinding `json:"next_page"`
	PrevPage    KeyBinding `json:"prev_page"`
	Select      KeyBinding `json:"select"`
	Filter      KeyBinding `json:"filter"`
}

type ThemeSelectorDialogKeyMapConfig struct {
	Select KeyBinding `json:"select"`
	Up     KeyBinding `json:"up"`
	Down   KeyBinding `json:"down"`
	Close  KeyBinding `json:"close"`
}

type NodeSelectorDialogKeyMapConfig struct {
	Select KeyBinding `json:"select"`
	Up     KeyBinding `json:"up"`
	Down   KeyBinding `json:"down"`
	Close  KeyBinding `json:"close"`
}

type ServerSetupDialogKeyMapConfig struct {
	Next     KeyBinding `json:"next"`
	Previous KeyBinding `json:"previous"`
	Submit   KeyBinding `json:"submit"`
	Advanced KeyBinding `json:"advanced"`
	Close    KeyBinding `json:"close"`
}

type ServerSelectorDialogKeyMapConfig struct {
	Add    KeyBinding `json:"add"`
	Edit   KeyBinding `json:"edit"`
	Select KeyBinding `json:"select"`
	Up     KeyBinding `json:"up"`
	Down   KeyBinding `json:"down"`
	Close  KeyBinding `json:"close"`
}

type ConfirmDialogKeyMapConfig struct {
	LeftRight KeyBinding `json:"left_right"`
	Confirm   KeyBinding `json:"confirm"`
	Yes       KeyBinding `json:"yes"`
	No        KeyBinding `json:"no"`
	Tab       KeyBinding `json:"tab"`
	Close     KeyBinding `json:"close"`
}

type ParamsViewDialogKeyMapConfig struct {
	Close KeyBinding `json:"close"`
}

type BuildParamsDialogKeyMapConfig struct {
	Up           KeyBinding `json:"up"`
	Down         KeyBinding `json:"down"`
	Tab          KeyBinding `json:"tab"`
	Toggle       KeyBinding `json:"toggle"`
	OpenPicker   KeyBinding `json:"open_picker"`
	ToggleChoice KeyBinding `json:"toggle_choice"`
	Reload       KeyBinding `json:"reload"`
	Filter       KeyBinding `json:"filter"`
	NextPage     KeyBinding `json:"next_page"`
	PrevPage     KeyBinding `json:"prev_page"`
	GoToStart    KeyBinding `json:"go_to_start"`
	GoToEnd      KeyBinding `json:"go_to_end"`
	Confirm      KeyBinding `json:"confirm"`
	Close        KeyBinding `json:"close"`
}

type ArtifactSelectorDialogKeyMapConfig struct {
	Up          KeyBinding `json:"up"`
	Down        KeyBinding `json:"down"`
	Toggle      KeyBinding `json:"toggle"`
	Download    KeyBinding `json:"download"`
	DownloadAll KeyBinding `json:"download_all"`
	Close       KeyBinding `json:"close"`
}

type DialogKeyMapConfig struct {
	Globalsearch     GlobalsearchDialogKeyMapConfig     `json:"global_search"`
	ThemeSelector    ThemeSelectorDialogKeyMapConfig    `json:"theme_selector"`
	NodeSelector     NodeSelectorDialogKeyMapConfig     `json:"node_selector"`
	ServerSelector   ServerSelectorDialogKeyMapConfig   `json:"server_selector"`
	ServerSetup      ServerSetupDialogKeyMapConfig      `json:"server_setup"`
	Confirm          ConfirmDialogKeyMapConfig          `json:"confirm"`
	ParamsView       ParamsViewDialogKeyMapConfig       `json:"params_view"`
	BuildParams      BuildParamsDialogKeyMapConfig      `json:"build_params"`
	ArtifactSelector ArtifactSelectorDialogKeyMapConfig `json:"artifact_selector"`
}

// Job component keymaps
type JobKeyMapConfig struct {
	Up                 KeyBinding `json:"up"`
	Down               KeyBinding `json:"down"`
	BuildsView         KeyBinding `json:"builds_view"`
	StagesView         KeyBinding `json:"stages_view"`
	Replay             KeyBinding `json:"replay"`
	Logs               KeyBinding `json:"logs"`
	OpenBuildBrowser   KeyBinding `json:"open_build_browser"`
	OpenJobBrowser     KeyBinding `json:"open_job_browser"`
	DownloadAttachment KeyBinding `json:"download_attachment"`
	OpenBuildView      KeyBinding `json:"open_build_view"`
	CancelBuild        KeyBinding `json:"cancel_build"`
	TriggerBuild       KeyBinding `json:"trigger_build"`
	Search             KeyBinding `json:"search"`
	Favorite           KeyBinding `json:"favorite"`
}

// Build component keymaps
type BuildKeyMapConfig struct {
	NextStage            KeyBinding `json:"next_stage"`
	PrevStage            KeyBinding `json:"prev_stage"`
	Up                   KeyBinding `json:"up"`
	Down                 KeyBinding `json:"down"`
	OpenStepLog          KeyBinding `json:"open_step_log"`
	OpenFullLog          KeyBinding `json:"open_full_log"`
	DownloadArtifacts    KeyBinding `json:"download_artifacts"`
	DownloadAllArtifacts KeyBinding `json:"download_all_artifacts"`
	CopyNode             KeyBinding `json:"copy_node"`
	OpenJob              KeyBinding `json:"open_job"`
	Replay               KeyBinding `json:"replay"`
	ShowParams           KeyBinding `json:"show_params"`
	OpenBuildBrowser     KeyBinding `json:"open_build_browser"`
	OpenJobBrowser       KeyBinding `json:"open_job_browser"`
	TriggerBuild         KeyBinding `json:"trigger_build"`
	CancelBuild          KeyBinding `json:"cancel_build"`
}

// Folder component keymaps
type FolderKeyMapConfig struct {
	Down     KeyBinding `json:"down"`
	Up       KeyBinding `json:"up"`
	Enter    KeyBinding `json:"enter"`
	Favorite KeyBinding `json:"favorite"`
}

// Log viewer keymaps shared by log viewing components.
type LogviewerKeyMapConfig struct {
	Up             KeyBinding `json:"up"`
	Down           KeyBinding `json:"down"`
	Right          KeyBinding `json:"right"`
	Left           KeyBinding `json:"left"`
	PageUp         KeyBinding `json:"page_up"`
	PageDown       KeyBinding `json:"page_down"`
	GoToTop        KeyBinding `json:"go_to_top"`
	GoToBottom     KeyBinding `json:"go_to_bottom"`
	Search         KeyBinding `json:"search"`
	Enter          KeyBinding `json:"enter"`
	ComponentUp    KeyBinding `json:"component_up"`
	ComponentDown  KeyBinding `json:"component_down"`
	Next           KeyBinding `json:"next"`
	Prev           KeyBinding `json:"prev"`
	Wrap           KeyBinding `json:"wrap"`
	VisualMode     KeyBinding `json:"visual_mode"`
	Yank           KeyBinding `json:"yank"`
	ExitVisualMode KeyBinding `json:"exit_visual_mode"`
}

// Scriptview component keymaps
type ScriptviewKeyMapConfig struct {
	Run   KeyBinding `json:"run"`
	Edit  KeyBinding `json:"edit"`
	Up    KeyBinding `json:"up"`
	Down  KeyBinding `json:"down"`
	Left  KeyBinding `json:"left"`
	Right KeyBinding `json:"right"`
}

// MyRuns component keymaps. Filter opens an inline name/build/status editor;
// NextField/Cancel only apply while it's open. SortCycle, Direction, and
// ResetFilter act immediately. StatusCycle focuses Status in that same editor.
type MyRunsKeyMapConfig struct {
	Down             KeyBinding `json:"down"`
	Up               KeyBinding `json:"up"`
	Enter            KeyBinding `json:"enter"`
	OpenJob          KeyBinding `json:"open_job"`
	Logs             KeyBinding `json:"logs"`
	OpenBuildBrowser KeyBinding `json:"open_build_browser"`
	OpenJobBrowser   KeyBinding `json:"open_job_browser"`
	Replay           KeyBinding `json:"replay"`
	TriggerBuild     KeyBinding `json:"trigger_build"`
	CancelBuild      KeyBinding `json:"cancel_build"`
	Delete           KeyBinding `json:"delete"`
	Filter           KeyBinding `json:"filter"`
	NextField        KeyBinding `json:"next_field"`
	Cancel           KeyBinding `json:"cancel"`
	SortCycle        KeyBinding `json:"sort_cycle"`
	Direction        KeyBinding `json:"direction"`
	StatusCycle      KeyBinding `json:"status_cycle"`
	ResetFilter      KeyBinding `json:"reset_filter"`
}

// Favorites component keymaps.
type FavoritesKeyMapConfig struct {
	Down           KeyBinding `json:"down"`
	Up             KeyBinding `json:"up"`
	Enter          KeyBinding `json:"enter"`
	Delete         KeyBinding `json:"delete"`
	JobsTab        KeyBinding `json:"jobs_tab"`
	FoldersTab     KeyBinding `json:"folders_tab"`
	MultibranchTab KeyBinding `json:"multibranch_tab"`
	OpenRuns       KeyBinding `json:"open_runs"`
	Search         KeyBinding `json:"search"`
}

type KeyMapConfig struct {
	Global     GlobalKeyMapConfig     `json:"global"`
	Dialog     DialogKeyMapConfig     `json:"dialogs"`
	Job        JobKeyMapConfig        `json:"job"`
	Build      BuildKeyMapConfig      `json:"build"`
	Folder     FolderKeyMapConfig     `json:"folder"`
	Logviewer  LogviewerKeyMapConfig  `json:"log_viewer"`
	Scriptview ScriptviewKeyMapConfig `json:"script_view"`
	MyRuns     MyRunsKeyMapConfig     `json:"my_runs"`
	Favorites  FavoritesKeyMapConfig  `json:"favorites"`
}
