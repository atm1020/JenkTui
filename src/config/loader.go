package config

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/atm1020/jenktui/src/context"
	colortheme "github.com/atm1020/jenktui/src/theme"

	"charm.land/log/v2"
	"dario.cat/mergo"
)

const (
	ConfigFileName       = "config.json"
	ServerConfigFileName = "server.json"
)

// GetDefaultServerConfig returns an empty server configuration template
func GetDefaultServerConfig() *context.ServerConfig {
	return &context.ServerConfig{
		ID:         "jenkins",
		Name:       "Jenkins",
		JenkinsURL: "",
		Username:   "",
		APIToken:   "",
		CACertPath: "",
	}
}

// GetDefaultConfig returns the default configuration
func GetDefaultConfig() *context.Config {
	return &context.Config{
		PollInterval:                 3,
		PollMaxParallelJobs:          2,
		JenkinsRequestTimeoutSeconds: 15,
		Theme:                        "GitHub Dark",
		Notification: context.NotificationConfig{
			DesktopEnabled: false,
			SoundEnabled:   false,
		},
		KeyMapConfig: getDefaultKeyMapConfig(),
	}
}

// getDefaultKeyMapConfig returns the default keymap configuration
func getDefaultKeyMapConfig() context.KeyMapConfig {
	return context.KeyMapConfig{
		Global: context.GlobalKeyMapConfig{
			Globalsearch:   context.KeyBinding{"ctrl+p"},
			Back:           context.KeyBinding{"b"},
			Forward:        context.KeyBinding{"]"},
			Quit:           context.KeyBinding{"ctrl+c"},
			HelpToggle:     context.KeyBinding{"?"},
			Help:           context.KeyBinding{"ctrl+g"},
			ThemeSelector:  context.KeyBinding{"ctrl+t"},
			ServerSelector: context.KeyBinding{"ctrl+s"},
			MyRuns:         context.KeyBinding{"m"},
			Favorites:      context.KeyBinding{"f"},
		},
		Dialog: context.DialogKeyMapConfig{
			Globalsearch: context.GlobalsearchDialogKeyMapConfig{
				Help:        context.KeyBinding{"ctrl+g"},
				Quit:        context.KeyBinding{"ctrl+c"},
				ToggleFocus: context.KeyBinding{"tab"},
				FocusUp:     context.KeyBinding{"ctrl+k"},
				FocusDown:   context.KeyBinding{"ctrl+j"},
				Search:      context.KeyBinding{"enter"},
				ClearInput:  context.KeyBinding{"ctrl+r"},
				Down:        context.KeyBinding{"j"},
				Up:          context.KeyBinding{"k"},
				NextPage:    context.KeyBinding{"l"},
				PrevPage:    context.KeyBinding{"h"},
				Select:      context.KeyBinding{"enter"},
				Filter:      context.KeyBinding{"/"},
			},
			ThemeSelector: context.ThemeSelectorDialogKeyMapConfig{
				Select: context.KeyBinding{"enter"},
				Up:     context.KeyBinding{"up", "k"},
				Down:   context.KeyBinding{"down", "j"},
				Close:  context.KeyBinding{"ctrl+c"},
			},
			NodeSelector: context.NodeSelectorDialogKeyMapConfig{
				Select: context.KeyBinding{"enter"},
				Up:     context.KeyBinding{"up", "k"},
				Down:   context.KeyBinding{"down", "j"},
				Close:  context.KeyBinding{"ctrl+c"},
			},
			ServerSetup: context.ServerSetupDialogKeyMapConfig{
				Next:     context.KeyBinding{"tab"},
				Previous: context.KeyBinding{"shift+tab"},
				Submit:   context.KeyBinding{"enter"},
				Advanced: context.KeyBinding{"ctrl+a"},
				Close:    context.KeyBinding{"esc", "ctrl+c"},
			},
			ServerSelector: context.ServerSelectorDialogKeyMapConfig{
				Add:    context.KeyBinding{"a"},
				Edit:   context.KeyBinding{"e"},
				Select: context.KeyBinding{"enter"},
				Up:     context.KeyBinding{"up", "k"},
				Down:   context.KeyBinding{"down", "j"},
				Close:  context.KeyBinding{"ctrl+c"},
			},
			Confirm: context.ConfirmDialogKeyMapConfig{
				LeftRight: context.KeyBinding{"h", "l"},
				Confirm:   context.KeyBinding{"enter", " "},
				Yes:       context.KeyBinding{"y", "Y"},
				No:        context.KeyBinding{"n", "N"},
				Tab:       context.KeyBinding{"tab"},
				Close:     context.KeyBinding{"esc"},
			},
			ParamsView: context.ParamsViewDialogKeyMapConfig{
				Close: context.KeyBinding{"ctrl+c", "esc", "q"},
			},
			BuildParams: context.BuildParamsDialogKeyMapConfig{
				Up:           context.KeyBinding{"up", "shift+tab", "k"},
				Down:         context.KeyBinding{"down", "j"},
				Tab:          context.KeyBinding{"tab"},
				Toggle:       context.KeyBinding{"h", "l"},
				OpenPicker:   context.KeyBinding{"enter"},
				ToggleChoice: context.KeyBinding{"space"},
				Reload:       context.KeyBinding{"ctrl+r"},
				Filter:       context.KeyBinding{"/"},
				NextPage:     context.KeyBinding{"right", "pgdown"},
				PrevPage:     context.KeyBinding{"left", "pgup"},
				GoToStart:    context.KeyBinding{"home"},
				GoToEnd:      context.KeyBinding{"end"},
				Confirm:      context.KeyBinding{"ctrl+s"},
				Close:        context.KeyBinding{"esc", "ctrl+c"},
			},
			ArtifactSelector: context.ArtifactSelectorDialogKeyMapConfig{
				Up:          context.KeyBinding{"up", "k"},
				Down:        context.KeyBinding{"down", "j"},
				Toggle:      context.KeyBinding{"space"},
				Download:    context.KeyBinding{"enter"},
				DownloadAll: context.KeyBinding{"a"},
				Close:       context.KeyBinding{"esc", "ctrl+c"},
			},
		},
		Job: context.JobKeyMapConfig{
			Up:                 context.KeyBinding{"up", "k"},
			Down:               context.KeyBinding{"down", "j"},
			BuildsView:         context.KeyBinding{"B"},
			StagesView:         context.KeyBinding{"S"},
			Replay:             context.KeyBinding{"r"},
			Logs:               context.KeyBinding{"L"},
			OpenBuildBrowser:   context.KeyBinding{"o"},
			OpenJobBrowser:     context.KeyBinding{"O"},
			DownloadAttachment: context.KeyBinding{"A"},
			OpenBuildView:      context.KeyBinding{"enter"},
			CancelBuild:        context.KeyBinding{"x"},
			TriggerBuild:       context.KeyBinding{"ctrl+b"},
			Search:             context.KeyBinding{"/"},
			Favorite:           context.KeyBinding{"F"},
		},
		Folder: context.FolderKeyMapConfig{
			Down:     context.KeyBinding{"down", "j"},
			Up:       context.KeyBinding{"up", "k"},
			Enter:    context.KeyBinding{"enter"},
			Favorite: context.KeyBinding{"F"},
		},
		Logviewer: context.LogviewerKeyMapConfig{
			Up:             context.KeyBinding{"k", "up"},
			Down:           context.KeyBinding{"j", "down"},
			Right:          context.KeyBinding{"l", "right"},
			Left:           context.KeyBinding{"h", "left"},
			PageUp:         context.KeyBinding{"}"},
			PageDown:       context.KeyBinding{"{"},
			GoToTop:        context.KeyBinding{"gg", "home"},
			GoToBottom:     context.KeyBinding{"G", "end"},
			Search:         context.KeyBinding{"/"},
			Enter:          context.KeyBinding{"enter"},
			ComponentUp:    context.KeyBinding{"ctrl+k"},
			ComponentDown:  context.KeyBinding{"ctrl+j"},
			Next:           context.KeyBinding{"n"},
			Prev:           context.KeyBinding{"p"},
			Wrap:           context.KeyBinding{"w"},
			VisualMode:     context.KeyBinding{"v"},
			Yank:           context.KeyBinding{"y"},
			ExitVisualMode: context.KeyBinding{"esc"},
		},
		Scriptview: context.ScriptviewKeyMapConfig{
			Run:   context.KeyBinding{"r"},
			Edit:  context.KeyBinding{"e"},
			Up:    context.KeyBinding{"up", "k"},
			Down:  context.KeyBinding{"down", "j"},
			Left:  context.KeyBinding{"left", "h"},
			Right: context.KeyBinding{"right", "l"},
		},
		Build: context.BuildKeyMapConfig{
			NextStage:            context.KeyBinding{"l"},
			PrevStage:            context.KeyBinding{"h"},
			Up:                   context.KeyBinding{"k", "up"},
			Down:                 context.KeyBinding{"j", "down"},
			OpenStepLog:          context.KeyBinding{"ctrl+l"},
			OpenFullLog:          context.KeyBinding{"L"},
			DownloadArtifacts:    context.KeyBinding{"d"},
			DownloadAllArtifacts: context.KeyBinding{"D"},
			CopyNode:             context.KeyBinding{"n"},
			OpenJob:              context.KeyBinding{"ctrl+j"},
			Replay:               context.KeyBinding{"r"},
			ShowParams:           context.KeyBinding{"P"},
			OpenBuildBrowser:     context.KeyBinding{"o"},
			OpenJobBrowser:       context.KeyBinding{"O"},
			TriggerBuild:         context.KeyBinding{"ctrl+b"},
			CancelBuild:          context.KeyBinding{"x"},
		},
		MyRuns: context.MyRunsKeyMapConfig{
			Down:             context.KeyBinding{"down", "j"},
			Up:               context.KeyBinding{"up", "k"},
			Enter:            context.KeyBinding{"enter"},
			OpenJob:          context.KeyBinding{"ctrl+j"},
			Logs:             context.KeyBinding{"L"},
			OpenBuildBrowser: context.KeyBinding{"o"},
			OpenJobBrowser:   context.KeyBinding{"O"},
			Replay:           context.KeyBinding{"r"},
			TriggerBuild:     context.KeyBinding{"ctrl+b"},
			CancelBuild:      context.KeyBinding{"x"},
			Delete:           context.KeyBinding{"d"},
			Filter:           context.KeyBinding{"/"},
			NextField:        context.KeyBinding{"tab"},
			Cancel:           context.KeyBinding{"esc"},
			SortCycle:        context.KeyBinding{"s"},
			Direction:        context.KeyBinding{"S"},
			StatusCycle:      context.KeyBinding{"t"},
			ResetFilter:      context.KeyBinding{"R"},
		},
		Favorites: context.FavoritesKeyMapConfig{
			Down:           context.KeyBinding{"down", "j"},
			Up:             context.KeyBinding{"up", "k"},
			Enter:          context.KeyBinding{"enter"},
			Delete:         context.KeyBinding{"d"},
			JobsTab:        context.KeyBinding{"J"},
			FoldersTab:     context.KeyBinding{"F"},
			MultibranchTab: context.KeyBinding{"M"},
			OpenRuns:       context.KeyBinding{"r"},
			Search:         context.KeyBinding{"/"},
		},
	}
}

// LoadServerConfigs loads all Jenkins connections. The legacy single-server
// object is accepted so existing installations continue to work.
func LoadServerConfigs(configDir string) ([]context.ServerConfig, error) {
	configPath := filepath.Join(configDir, ServerConfigFileName)

	// Check if server config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		return nil, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read server config file %s: %w", configPath, err)
	}

	var db context.ServerDB
	if err := json.Unmarshal(data, &db); err != nil {
		return nil, fmt.Errorf("failed to parse server config file %s: %w\nPlease check the JSON syntax", configPath, err)
	}
	if db.Servers == nil { // legacy schema or an empty object
		var legacy context.ServerConfig
		if err := json.Unmarshal(data, &legacy); err != nil {
			return nil, fmt.Errorf("invalid server configuration")
		}
		if legacy.JenkinsURL != "" || legacy.Username != "" || legacy.APIToken != "" {
			db.Servers = []context.ServerConfig{legacy}
		}
	}
	seen := make(map[string]bool, len(db.Servers))
	for i := range db.Servers {
		cfg := &db.Servers[i]
		if cfg.ID == "" {
			cfg.ID = fmt.Sprintf("jenkins-%d", i+1)
		}
		if cfg.Name == "" {
			cfg.Name = cfg.ID
		}
		if seen[cfg.ID] {
			return nil, fmt.Errorf("duplicate Jenkins server id %q", cfg.ID)
		}
		seen[cfg.ID] = true
		if err := ValidateServerConfig(cfg); err != nil {
			return nil, fmt.Errorf("invalid server %q in %s: %w", cfg.Name, configPath, err)
		}
	}
	log.Info("Server configurations loaded", "count", len(db.Servers))
	return db.Servers, nil
}

// LoadServerConfig is retained for callers that only need the first server.
func LoadServerConfig(configDir string) (*context.ServerConfig, error) {
	servers, err := LoadServerConfigs(configDir)
	if err != nil {
		return nil, err
	}
	if len(servers) == 0 {
		return nil, fmt.Errorf("no Jenkins servers configured")
	}
	return &servers[0], nil
}

func LoadConfig(configDir string) (*context.Config, error) {
	configPath := filepath.Join(configDir, ConfigFileName)

	// Check if config file exists
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		log.Info("Config file not found, creating default config", "path", configPath)

		// Create default config file
		if err := SaveDefaultConfig(configDir); err != nil {
			log.Warn("Failed to create default config file", "error", err)
		}

		return GetDefaultConfig(), nil
	}

	// Read config file
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file %s: %w", configPath, err)
	}

	// Parse JSON
	var cfg context.Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: %w\nPlease check the JSON syntax", configPath, err)
	}

	// Merge with defaults for any missing fields
	defaults := GetDefaultConfig()
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = defaults.PollInterval
	}

	// Handle missing/empty theme field (backwards compatibility)
	if cfg.Theme == "" {
		cfg.Theme = defaults.Theme
	}

	// Resolve both built-in names and custom <config-dir>/themes/*.json files.
	// Keeping this check in config loading preserves the existing safe fallback
	// while allowing the available set to be data-driven.
	definitions := colortheme.LoadAll(configDir)
	selectedTheme, isValid := colortheme.Find(definitions, cfg.Theme)
	if !isValid {
		log.Warn("Invalid theme in config, using default", "theme", cfg.Theme, "default", defaults.Theme)
		cfg.Theme = defaults.Theme
	} else {
		cfg.Theme = selectedTheme.Name
	}

	// Merge keymap config with defaults
	mergeKeyMapConfig(&cfg.KeyMapConfig, defaults.KeyMapConfig)

	// Validate the config
	if err := ValidateConfig(&cfg); err != nil {
		return nil, fmt.Errorf("invalid config: %w", err)
	}

	log.Info("Config loaded successfully")
	return &cfg, nil
}

// SaveDefaultConfig saves the default configuration to the specified directory
func SaveDefaultConfig(configDir string) error {
	cfg := GetDefaultConfig()
	return SaveConfig(configDir, cfg)
}

// SaveConfig saves the given configuration to the specified directory
func SaveConfig(configDir string, cfg *context.Config) error {
	configPath := filepath.Join(configDir, ConfigFileName)

	// Marshal config to JSON with indentation for readability
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}

	// Write to file
	if err := os.WriteFile(configPath, data, 0o600); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", configPath, err)
	}
	if err := os.Chmod(configPath, 0o600); err != nil {
		return fmt.Errorf("failed to secure config file %s: %w", configPath, err)
	}

	log.Info("Config saved successfully", "path", configPath)
	return nil
}

// SaveServerConfig preserves the old API while writing the multi-server schema.
func SaveServerConfig(configDir string, cfg *context.ServerConfig) error {
	return SaveServerConfigs(configDir, []context.ServerConfig{*cfg})
}

func SaveServerConfigs(configDir string, servers []context.ServerConfig) error {
	configPath := filepath.Join(configDir, ServerConfigFileName)

	data, err := json.MarshalIndent(context.ServerDB{Servers: servers}, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal server config: %w", err)
	}

	// Replace atomically: a failed write must not truncate stored credentials.
	file, err := os.CreateTemp(configDir, ".server-*.json")
	if err != nil {
		return fmt.Errorf("failed to create server config file: %w", err)
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		return fmt.Errorf("failed to write server config file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to close server config file: %w", err)
	}
	if err := os.Rename(file.Name(), configPath); err != nil {
		return fmt.Errorf("failed to replace server config file: %w", err)
	}

	log.Info("Server config saved successfully", "path", configPath)
	return nil
}

// ValidateServerConfig validates the server configuration fields
func ValidateServerConfig(cfg *context.ServerConfig) error {
	if cfg.JenkinsURL == "" {
		return fmt.Errorf("jenkins_url cannot be empty")
	}

	parsedURL, err := url.Parse(cfg.JenkinsURL)
	if err != nil {
		return fmt.Errorf("invalid jenkins_url format")
	}

	if (parsedURL.Scheme != "http" && parsedURL.Scheme != "https") || parsedURL.Hostname() == "" {
		return fmt.Errorf("jenkins_url must be an absolute http or https URL")
	}
	if parsedURL.User != nil || parsedURL.RawQuery != "" || parsedURL.Fragment != "" {
		return fmt.Errorf("jenkins_url must not contain credentials, a query, or a fragment")
	}

	if cfg.Username == "" {
		return fmt.Errorf("username cannot be empty")
	}

	if cfg.APIToken == "" {
		return fmt.Errorf("api_token cannot be empty")
	}

	return nil
}

// ValidateConfig validates the configuration fields
func ValidateConfig(cfg *context.Config) error {
	if cfg.PollInterval <= 0 {
		return fmt.Errorf("poll_interval must be greater than 0, got: %d", cfg.PollInterval)
	}

	err := validateTheme(cfg)
	if err != nil {
		return err
	}

	return nil
}

func validateTheme(cfg *context.Config) error {
	// Theme existence depends on the caller's config directory because custom
	// themes are supported. LoadConfig resolves that catalog before reaching
	// this structural validation.
	if strings.TrimSpace(cfg.Theme) == "" {
		return fmt.Errorf("theme cannot be empty")
	}
	return nil
}

// mergeKeyMapConfig merges user keymap config with defaults for any missing fields
func mergeKeyMapConfig(cfg *context.KeyMapConfig, defaults context.KeyMapConfig) {
	// Older generated configs used "o" for My Runs' in-app Open Job action.
	// That now collides with the shared Open Build in Browser default. When the
	// new browser binding is absent, migrate that exact old default and let an
	// explicitly configured browser key preserve a deliberate "o" override.
	migrateMyRunsOpenJob := len(cfg.MyRuns.OpenBuildBrowser) == 0 &&
		slices.Equal(cfg.MyRuns.OpenJob, context.KeyBinding{"o"})

	// Use mergo to merge defaults into cfg for any empty fields
	if err := mergo.Merge(cfg, defaults); err != nil {
		log.Warn("Failed to merge keymap config with defaults", "error", err)
	}
	if migrateMyRunsOpenJob {
		cfg.MyRuns.OpenJob = slices.Clone(defaults.MyRuns.OpenJob)
	}
}
