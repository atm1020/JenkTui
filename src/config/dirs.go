package config

import (
	"os"
	"path/filepath"
	"runtime"
)

const appName = "JenkTui"

const privateDirMode = 0o700

// ensurePrivateDir creates a directory and tightens permissions on an existing
// directory. Chmod is best-effort on filesystems that do not model Unix bits.
func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, privateDirMode); err != nil {
		return err
	}
	return os.Chmod(path, privateDirMode)
}

func GetConfigDir() (string, error) {
	var dir string

	// Platform-specific defaults
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	switch runtime.GOOS {
	case "windows":
		// Windows: %APPDATA%\JenkTui
		appData := os.Getenv("APPDATA")
		if appData == "" {
			appData = filepath.Join(homeDir, "AppData", "Roaming")
		}
		dir = filepath.Join(appData, appName)
	case "darwin":
		// macOS: $HOME/Library/Application Support/JenkTui
		dir = filepath.Join(homeDir, "Library", "Application Support", appName)
	default:
		// Linux and others: $HOME/.config/JenkTui
		dir = filepath.Join(homeDir, ".config", appName)
	}

	// Configuration can contain Jenkins credentials, so the directory is private.
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}

	return dir, nil
}

func GetDataDir() (string, error) {
	var dir string

	// Check environment variable
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	switch runtime.GOOS {
	case "windows":
		// Windows: %LOCALAPPDATA%\JenkTui
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(homeDir, "AppData", "Local")
		}
		dir = filepath.Join(localAppData, appName)
	case "darwin":
		// macOS: $HOME/Library/Application Support/JenkTui
		dir = filepath.Join(homeDir, "Library", "Application Support", appName)
	default:
		// Linux: $HOME/.local/share/JenkTui
		dir = filepath.Join(homeDir, ".local", "share", appName)
	}

	// Logs, artifacts, and run metadata may contain sensitive build data.
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}

	return dir, nil
}

func GetCacheDir() (string, error) {
	var dir string

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}

	switch runtime.GOOS {
	case "windows":
		localAppData := os.Getenv("LOCALAPPDATA")
		if localAppData == "" {
			localAppData = filepath.Join(homeDir, "AppData", "Local")
		}
		dir = filepath.Join(localAppData, appName, "Cache")
	case "darwin":
		dir = filepath.Join(homeDir, "Library", "Caches", appName)
	default:
		dir = filepath.Join(homeDir, ".cache", appName)
	}

	// Replay scripts and downloaded logs are private temporary data.
	if err := ensurePrivateDir(dir); err != nil {
		return "", err
	}

	return dir, nil
}

type Subdirs struct {
	DataLogsDir      string
	DataArtifactsDir string
}

func InitializeSubdirectories(dataDir string) (*Subdirs, error) {
	subdirs := &Subdirs{
		DataLogsDir:      filepath.Join(dataDir, LogDirName),
		DataArtifactsDir: filepath.Join(dataDir, AttachmentDirName),
	}

	// Create data subdirectories
	paths := []string{
		subdirs.DataLogsDir,
		subdirs.DataArtifactsDir,
	}

	for _, path := range paths {
		if err := ensurePrivateDir(path); err != nil {
			return nil, err
		}
	}

	return subdirs, nil
}
