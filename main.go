package main

import (
	"fmt"
	"net/http"
	"os"

	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/constants"
	applog "github.com/atm1020/jenktui/src/log"
	"github.com/atm1020/jenktui/src/ui"
	"github.com/atm1020/jenktui/src/version"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"

	_ "net/http/pprof"
)

func main() {
	// Resolve data directories early so logging can use them.
	dataDir, err := config.GetDataDir()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to get data directory: %v\n", err)
		os.Exit(1)
	}
	subdirs, err := config.InitializeSubdirectories(dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to initialize subdirectories: %v\n", err)
		os.Exit(1)
	}

	applog.Setup(subdirs.DataLogsDir)
	log.Info("Starting JenkTui", "version", version.Version, "commit", version.Commit)

	if os.Getenv("DEVELOPMENT") != "" {
		go func() {
			log.Info("Serving pprof at localhost:6060")
			if httpErr := http.ListenAndServe("localhost:6060", nil); httpErr != nil {
				log.Error("Failed to pprof listen", "error", httpErr)
			}
		}()
	}

	app := initApp(subdirs)
	p := tea.NewProgram(app)
	if _, err := p.Run(); err != nil {
		fmt.Printf("Error: %v", err)
	}
}

func initApp(subdirs *config.Subdirs) *ui.App {
	app, err := ui.InitAppModel(subdirs)
	if err != nil {
		log.Error("Failed to initialize JenkTui", "error", err)
		fmt.Fprintf(os.Stderr, "\n%s\n\n", constants.ASCII)
		fmt.Fprintf(os.Stderr, "  Version: %s\n\n", version.Full())
		fmt.Fprintf(os.Stderr, "❌ Error: %v\n\n", err)
		fmt.Fprintf(os.Stderr, "Suggestions:\n")
		fmt.Fprintf(os.Stderr, "  - Check configuration file at ~/.config/JenkTui/server.json\n")
		fmt.Fprintf(os.Stderr, "  - Verify JSON syntax is valid\n")
		fmt.Fprintf(os.Stderr, "  - Ensure all required fields are present (jenkins_url, username, api_token)\n")
		fmt.Fprintf(os.Stderr, "  - Verify ca_cert_path exists and is readable when configured\n\n")
		fmt.Fprintf(os.Stderr, "See ~/.local/share/JenkTui/logs/jenktui.log for more details.\n\n")
		os.Exit(1)
	}
	return app
}
