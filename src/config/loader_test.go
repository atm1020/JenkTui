package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/context"
)

func kb(keys ...string) context.KeyBinding {
	return context.KeyBinding(keys)
}

func TestMergeKeyMapConfig(t *testing.T) {
	tests := []struct {
		name     string
		userCfg  context.KeyMapConfig
		expected context.KeyMapConfig
	}{
		{
			name: "Empty user config should get all defaults",
			userCfg: context.KeyMapConfig{
				Global: context.GlobalKeyMapConfig{},
				Dialog: context.DialogKeyMapConfig{
					Globalsearch:  context.GlobalsearchDialogKeyMapConfig{},
					ThemeSelector: context.ThemeSelectorDialogKeyMapConfig{},
				},
			},
			expected: getDefaultKeyMapConfig(),
		},
		{
			name: "Partial global config should merge with defaults",
			userCfg: context.KeyMapConfig{
				Global: context.GlobalKeyMapConfig{
					Globalsearch: kb("ctrl+v", "ctrl+p"),
					Help:         kb("?"),
				},
			},
			expected: func() context.KeyMapConfig {
				defaults := getDefaultKeyMapConfig()
				defaults.Global.Globalsearch = kb("ctrl+v", "ctrl+p")
				defaults.Global.Help = kb("?")
				return defaults
			}(),
		},
		{
			name: "Partial dialog config should merge with defaults",
			userCfg: context.KeyMapConfig{
				Dialog: context.DialogKeyMapConfig{
					Globalsearch: context.GlobalsearchDialogKeyMapConfig{
						Help: kb("ctrl+h"),
						Quit: kb("q"),
					},
				},
			},
			expected: func() context.KeyMapConfig {
				defaults := getDefaultKeyMapConfig()
				defaults.Dialog.Globalsearch.Help = kb("ctrl+h")
				defaults.Dialog.Globalsearch.Quit = kb("q")
				return defaults
			}(),
		},
		{
			name: "Full user config should remain unchanged",
			userCfg: func() context.KeyMapConfig {
				defaults := getDefaultKeyMapConfig()
				defaults.Global = context.GlobalKeyMapConfig{
					Globalsearch:   kb("ctrl+s"),
					Back:           kb("esc"),
					Forward:        kb("F2"),
					Quit:           kb("q"),
					HelpToggle:     kb("F1"),
					Help:           kb("h"),
					ThemeSelector:  kb("ctrl+t"),
					ServerSelector: kb("ctrl+o"),
					MyRuns:         kb("m"),
					Favorites:      kb("shift+f"),
				}
				defaults.Dialog.Globalsearch = context.GlobalsearchDialogKeyMapConfig{
					Help:        kb("?"),
					Quit:        kb("q"),
					ToggleFocus: kb("ctrl+tab"),
					FocusUp:     kb("up"),
					FocusDown:   kb("down"),
					Search:      kb("return"),
					ClearInput:  kb("ctrl+l"),
					Down:        kb("down"),
					Up:          kb("up"),
					NextPage:    kb("right"),
					PrevPage:    kb("left"),
					Select:      kb("space"),
					Filter:      kb("ctrl+f"),
				}
				return defaults
			}(),
			expected: func() context.KeyMapConfig {
				defaults := getDefaultKeyMapConfig()
				defaults.Global = context.GlobalKeyMapConfig{
					Globalsearch:   kb("ctrl+s"),
					Back:           kb("esc"),
					Forward:        kb("F2"),
					Quit:           kb("q"),
					HelpToggle:     kb("F1"),
					Help:           kb("h"),
					ThemeSelector:  kb("ctrl+t"),
					ServerSelector: kb("ctrl+o"),
					MyRuns:         kb("m"),
					Favorites:      kb("shift+f"),
				}
				defaults.Dialog.Globalsearch = context.GlobalsearchDialogKeyMapConfig{
					Help:        kb("?"),
					Quit:        kb("q"),
					ToggleFocus: kb("ctrl+tab"),
					FocusUp:     kb("up"),
					FocusDown:   kb("down"),
					Search:      kb("return"),
					ClearInput:  kb("ctrl+l"),
					Down:        kb("down"),
					Up:          kb("up"),
					NextPage:    kb("right"),
					PrevPage:    kb("left"),
					Select:      kb("space"),
					Filter:      kb("ctrl+f"),
				}
				return defaults
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userCfg := tt.userCfg
			defaults := getDefaultKeyMapConfig()
			mergeKeyMapConfig(&userCfg, defaults)

			if !reflect.DeepEqual(userCfg, tt.expected) {
				t.Errorf("merged config mismatch:\ngot:  %+v\nwant: %+v", userCfg, tt.expected)
			}
		})
	}
}

func TestMergeKeyMapConfigMigratesMyRunsOpenJobDefault(t *testing.T) {
	userCfg := context.KeyMapConfig{MyRuns: context.MyRunsKeyMapConfig{OpenJob: kb("o")}}
	defaults := getDefaultKeyMapConfig()

	mergeKeyMapConfig(&userCfg, defaults)

	if !reflect.DeepEqual(userCfg.MyRuns.OpenJob, kb("ctrl+j")) {
		t.Fatalf("MyRuns.OpenJob = %v, want migrated ctrl+j default", userCfg.MyRuns.OpenJob)
	}
	if !reflect.DeepEqual(userCfg.MyRuns.OpenBuildBrowser, kb("o")) {
		t.Fatalf("MyRuns.OpenBuildBrowser = %v, want o", userCfg.MyRuns.OpenBuildBrowser)
	}
	if !reflect.DeepEqual(userCfg.MyRuns.OpenJobBrowser, kb("O")) {
		t.Fatalf("MyRuns.OpenJobBrowser = %v, want O", userCfg.MyRuns.OpenJobBrowser)
	}
}

func TestMergeKeyMapConfigPreservesExplicitMyRunsOpenKeys(t *testing.T) {
	userCfg := context.KeyMapConfig{MyRuns: context.MyRunsKeyMapConfig{
		OpenJob: kb("o"), OpenBuildBrowser: kb("ctrl+o"),
	}}

	mergeKeyMapConfig(&userCfg, getDefaultKeyMapConfig())

	if !reflect.DeepEqual(userCfg.MyRuns.OpenJob, kb("o")) {
		t.Fatalf("MyRuns.OpenJob = %v, want explicit o override", userCfg.MyRuns.OpenJob)
	}
	if !reflect.DeepEqual(userCfg.MyRuns.OpenBuildBrowser, kb("ctrl+o")) {
		t.Fatalf("MyRuns.OpenBuildBrowser = %v, want explicit ctrl+o override", userCfg.MyRuns.OpenBuildBrowser)
	}
}

func TestGetDefaultConfig(t *testing.T) {
	cfg := GetDefaultConfig()

	if cfg.Theme != "GitHub Dark" {
		t.Errorf("Theme = %v, want GitHub Dark", cfg.Theme)
	}
	if cfg.Notification.DesktopEnabled {
		t.Error("Notification.DesktopEnabled = true, want false")
	}
	if cfg.Notification.SoundEnabled {
		t.Error("Notification.SoundEnabled = true, want false")
	}

	if len(cfg.KeyMapConfig.Global.Globalsearch) == 0 {
		t.Error("Global.Globalsearch should not be empty in default config")
	}
	if len(cfg.KeyMapConfig.Global.Quit) == 0 {
		t.Error("Global.Quit should not be empty in default config")
	}
	if len(cfg.KeyMapConfig.Dialog.Globalsearch.Help) == 0 {
		t.Error("Dialog.Globalsearch.Help should not be empty in default config")
	}
	if len(cfg.KeyMapConfig.Dialog.Globalsearch.Search) == 0 {
		t.Error("Dialog.Globalsearch.Search should not be empty in default config")
	}
	if len(cfg.KeyMapConfig.Dialog.NodeSelector.Select) == 0 {
		t.Error("Dialog.NodeSelector.Select should not be empty in default config")
	}
	if len(cfg.KeyMapConfig.Dialog.ArtifactSelector.DownloadAll) == 0 {
		t.Error("Dialog.ArtifactSelector.DownloadAll should not be empty in default config")
	}
	if len(cfg.KeyMapConfig.Build.CopyNode) == 0 {
		t.Error("Build.CopyNode should not be empty in default config")
	}
	if len(cfg.KeyMapConfig.Build.DownloadAllArtifacts) == 0 {
		t.Error("Build.DownloadAllArtifacts should not be empty in default config")
	}
	if !reflect.DeepEqual(cfg.KeyMapConfig.MyRuns.OpenJob, kb("ctrl+j")) {
		t.Errorf("MyRuns.OpenJob = %v, want ctrl+j", cfg.KeyMapConfig.MyRuns.OpenJob)
	}
	if !reflect.DeepEqual(cfg.KeyMapConfig.MyRuns.Logs, kb("L")) {
		t.Errorf("MyRuns.Logs = %v, want L", cfg.KeyMapConfig.MyRuns.Logs)
	}
	if !reflect.DeepEqual(cfg.KeyMapConfig.MyRuns.OpenBuildBrowser, kb("o")) {
		t.Errorf("MyRuns.OpenBuildBrowser = %v, want o", cfg.KeyMapConfig.MyRuns.OpenBuildBrowser)
	}
	if !reflect.DeepEqual(cfg.KeyMapConfig.MyRuns.OpenJobBrowser, kb("O")) {
		t.Errorf("MyRuns.OpenJobBrowser = %v, want O", cfg.KeyMapConfig.MyRuns.OpenJobBrowser)
	}
	if len(cfg.KeyMapConfig.Logviewer.VisualMode) == 0 {
		t.Error("Logviewer.VisualMode should not be empty in default config")
	}
	if len(cfg.KeyMapConfig.Logviewer.Yank) == 0 {
		t.Error("Logviewer.Yank should not be empty in default config")
	}
	if len(cfg.KeyMapConfig.Logviewer.ExitVisualMode) == 0 {
		t.Error("Logviewer.ExitVisualMode should not be empty in default config")
	}
}

func TestOpenActionDefaultKeysAreConsistent(t *testing.T) {
	keys := GetDefaultConfig().KeyMapConfig
	tests := []struct {
		name string
		want context.KeyBinding
		got  []context.KeyBinding
	}{
		{name: "open job", want: kb("ctrl+j"), got: []context.KeyBinding{keys.Build.OpenJob, keys.MyRuns.OpenJob}},
		{name: "open log", want: kb("L"), got: []context.KeyBinding{keys.Job.Logs, keys.Build.OpenFullLog, keys.MyRuns.Logs}},
		{name: "open build in browser", want: kb("o"), got: []context.KeyBinding{keys.Job.OpenBuildBrowser, keys.Build.OpenBuildBrowser, keys.MyRuns.OpenBuildBrowser}},
		{name: "open job in browser", want: kb("O"), got: []context.KeyBinding{keys.Job.OpenJobBrowser, keys.Build.OpenJobBrowser, keys.MyRuns.OpenJobBrowser}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for i, got := range tt.got {
				if !reflect.DeepEqual(got, tt.want) {
					t.Errorf("binding %d = %v, want %v", i, got, tt.want)
				}
			}
		})
	}
}

func TestDefaultConfigJSONUsesSnakeCaseCompoundKeys(t *testing.T) {
	data, err := json.Marshal(GetDefaultConfig())
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	encoded := string(data)

	for _, key := range []string{"global_search", "log_viewer", "script_view"} {
		if !strings.Contains(encoded, `"`+key+`"`) {
			t.Errorf("marshaled config does not contain %q", key)
		}
	}
	for _, key := range []string{"globalsearch", "logviewer", "scriptview"} {
		if strings.Contains(encoded, `"`+key+`"`) {
			t.Errorf("marshaled config contains non-snake-case key %q", key)
		}
	}
}

func TestLoadConfigPreservesNotificationSettings(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{
		"poll_interval": 3,
		"theme": "GitHub Dark",
		"notification": {
			"desktop_enabled": true,
			"sound_enabled": true
		}
	}`)
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if !cfg.Notification.DesktopEnabled {
		t.Error("Notification.DesktopEnabled = false, want true")
	}
	if !cfg.Notification.SoundEnabled {
		t.Error("Notification.SoundEnabled = false, want true")
	}
}

func TestLoadConfigAcceptsCustomTheme(t *testing.T) {
	dir := t.TempDir()
	themeDir := filepath.Join(dir, "themes")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	definition := `{
		"bg": "#101820",
		"accent": "#4da3ff",
		"bright_fg": "#f5f7fa",
		"fg": "#aab4c0",
		"green": "#72c472",
		"yellow": "#e0b84f",
		"red": "#ef6b73"
	}`
	if err := os.WriteFile(filepath.Join(themeDir, "ocean.json"), []byte(definition), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(`{"theme":"ocean"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Theme != "ocean" {
		t.Fatalf("Theme = %q, want ocean", cfg.Theme)
	}
}

func TestLoadConfigFallsBackForUnavailableTheme(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(`{"theme":"missing"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.Theme != "GitHub Dark" {
		t.Fatalf("Theme = %q, want GitHub Dark", cfg.Theme)
	}
}

func TestDefaultGlobalNavigationBindingsDoNotCollide(t *testing.T) {
	global := getDefaultKeyMapConfig().Global
	for _, forward := range global.Forward {
		for _, favorite := range global.Favorites {
			if forward == favorite {
				t.Fatalf("Forward and Favorites both use %q", forward)
			}
		}
	}
}

func TestGetDefaultServerConfig(t *testing.T) {
	cfg := GetDefaultServerConfig()

	if cfg.JenkinsURL != "" {
		t.Errorf("JenkinsURL = %v, want empty string", cfg.JenkinsURL)
	}
	if cfg.Username != "" {
		t.Errorf("Username = %v, want empty string", cfg.Username)
	}
	if cfg.APIToken != "" {
		t.Errorf("APIToken = %v, want empty string", cfg.APIToken)
	}
	if cfg.CACertPath != "" {
		t.Errorf("CACertPath = %v, want empty string", cfg.CACertPath)
	}
}

func TestServerConfigCACertificatePathIsOptional(t *testing.T) {
	data, err := json.Marshal(GetDefaultServerConfig())
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(data), `"ca_cert_path"`) {
		t.Fatalf("default server config contains optional ca_cert_path: %s", data)
	}
}

func TestLoadServerConfigsReadsCACertificatePath(t *testing.T) {
	dir := t.TempDir()
	data := []byte(`{
		"servers": [{
			"id": "internal",
			"name": "Internal Jenkins",
			"jenkins_url": "https://jenkins.example.com",
			"username": "user",
			"api_token": "token",
			"ca_cert_path": "/path/to/company-ca.pem"
		}]
	}`)
	if err := os.WriteFile(filepath.Join(dir, ServerConfigFileName), data, 0o600); err != nil {
		t.Fatal(err)
	}

	servers, err := LoadServerConfigs(dir)
	if err != nil {
		t.Fatalf("LoadServerConfigs() error = %v", err)
	}
	if got := servers[0].CACertPath; got != "/path/to/company-ca.pem" {
		t.Fatalf("CACertPath = %q, want %q", got, "/path/to/company-ca.pem")
	}
}

func TestMergeKeyMapConfigFillsBuildParamsPickerBindings(t *testing.T) {
	cfg := context.KeyMapConfig{
		Dialog: context.DialogKeyMapConfig{
			BuildParams: context.BuildParamsDialogKeyMapConfig{OpenPicker: kb("o")},
		},
	}
	defaults := getDefaultKeyMapConfig()
	mergeKeyMapConfig(&cfg, defaults)

	got := cfg.Dialog.BuildParams
	if !reflect.DeepEqual(got.OpenPicker, kb("o")) {
		t.Errorf("OpenPicker = %v, want the user's o", got.OpenPicker)
	}
	for name, pair := range map[string][2]context.KeyBinding{
		"ToggleChoice": {got.ToggleChoice, defaults.Dialog.BuildParams.ToggleChoice},
		"Reload":       {got.Reload, defaults.Dialog.BuildParams.Reload},
		"Filter":       {got.Filter, defaults.Dialog.BuildParams.Filter},
		"Close":        {got.Close, defaults.Dialog.BuildParams.Close},
	} {
		if len(pair[1]) == 0 {
			t.Errorf("default %s is empty", name)
		}
		if !reflect.DeepEqual(pair[0], pair[1]) {
			t.Errorf("%s = %v, want default %v", name, pair[0], pair[1])
		}
	}
}

func TestDefaultConfigJSONIncludesBuildParamsPickerKeys(t *testing.T) {
	data, err := json.Marshal(GetDefaultConfig().KeyMapConfig.Dialog.BuildParams)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, key := range []string{"open_picker", "toggle_choice", "reload", "filter", "next_page", "prev_page", "go_to_start", "go_to_end"} {
		if !strings.Contains(string(data), `"`+key+`"`) {
			t.Errorf("build_params JSON does not contain %q: %s", key, data)
		}
	}
}
