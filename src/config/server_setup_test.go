package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMissingOrEmptyServerConfigNeedsSetup(t *testing.T) {
	for _, contents := range []string{"missing", `{}`, `{"servers":[]}`, `{"jenkins_url":"","username":"","api_token":""}`} {
		t.Run(contents, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ServerConfigFileName)
			if contents != "missing" {
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			servers, err := LoadServerConfigs(dir)
			if err != nil || len(servers) != 0 {
				t.Fatal("missing or empty server configuration should start setup")
			}
			if contents == "missing" {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatal("missing configuration must not create a template")
				}
			}
			if _, err := LoadServerConfig(dir); err == nil {
				t.Fatal("single-server API must not panic on empty configuration")
			}
		})
	}
}

func TestExistingConfigGetsSetupKeyDefaults(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ConfigFileName), []byte(`{"key_map":{"dialogs":{"server_selector":{"select":["space"]}}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(dir)
	if err != nil {
		t.Fatal(err)
	}
	keys := cfg.KeyMapConfig.Dialog
	if len(keys.ServerSelector.Select) != 1 || keys.ServerSelector.Select[0] != "space" {
		t.Fatal("custom selector binding was not preserved")
	}
	if len(keys.ServerSelector.Add) == 0 || len(keys.ServerSelector.Edit) == 0 || len(keys.ServerSetup.Submit) == 0 || len(keys.ServerSetup.Close) == 0 {
		t.Fatal("existing config did not get setup defaults")
	}
}

func TestMalformedServerConfigIsNotOverwritten(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ServerConfigFileName)
	original := []byte(`{"servers": broken}`)
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadServerConfigs(dir); err == nil {
		t.Fatal("malformed configuration accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Fatal("malformed config overwritten")
	}
}
