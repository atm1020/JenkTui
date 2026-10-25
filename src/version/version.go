package version

import "runtime/debug"

// Build-time parameters set via -ldflags
var (
	Version = "dev"
	Commit  = ""
	Date    = ""
)

// init attempts to set version from build info when not set via ldflags.
// This handles the case when users install via `go install github.com/atm1020/jenktui@latest`.
func init() {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return
	}

	// Only use module version if ldflags didn't set it (still default "dev")
	if Version == "dev" && info.Main.Version != "" && info.Main.Version != "(devel)" {
		Version = info.Main.Version
	}

	// Extract commit from build settings only if not already set via ldflags
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			if Commit == "" && len(setting.Value) >= 7 {
				Commit = setting.Value[:7]
			}
		case "vcs.time":
			if Date == "" {
				Date = setting.Value
			}
		}
	}
}

// Full returns a formatted version string with all available info.
func Full() string {
	v := Version
	if Commit != "" {
		v += " (" + Commit + ")"
	}
	if Date != "" {
		v += " built " + Date
	}
	return v
}
