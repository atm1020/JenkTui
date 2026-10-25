//go:build !windows

package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitializeSubdirectoriesMakesExistingDirectoriesPrivate(t *testing.T) {
	dataDir := t.TempDir()
	logsDir := filepath.Join(dataDir, LogDirName)
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	subdirs, err := InitializeSubdirectories(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{subdirs.DataLogsDir, subdirs.DataArtifactsDir} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if got := info.Mode().Perm(); got != 0o700 {
			t.Errorf("%s mode = %o, want 700", path, got)
		}
	}
}
