package ui

import (
	"os"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/serverselector"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/serversetup"
)

func TestFirstRunStartsSetupWithoutClientOrStore(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("APPDATA", home)
	t.Setenv("LOCALAPPDATA", home)
	data, err := config.GetDataDir()
	if err != nil {
		t.Fatal(err)
	}
	subdirs, err := config.InitializeSubdirectories(data)
	if err != nil {
		t.Fatal(err)
	}
	app, err := InitAppModel(subdirs)
	if err != nil {
		t.Fatal(err)
	}
	if app.ctx.Jenkins != nil || app.ctx.RunStore != nil || app.ctx.PollManager != nil {
		t.Fatal("unconfigured app created connection resources")
	}
	_ = app.Init()
	d, ok := app.dialog.(*serversetup.Dialog)
	if !ok {
		t.Fatal("first run did not open setup")
	}
	_, _ = app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	_ = app.View()
	cmd := app.cancelServerSetup(serversetup.CancelMsg{Dialog: d})
	if cmd == nil {
		t.Fatal("first-run cancel did not quit")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("first-run cancel did not exit cleanly")
	}
	dir, err := config.GetConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() == "server.json" {
			t.Fatal("cancel saved partial connection")
		}
	}
}

func TestSetupCancelReturnsToSelectorAndIgnoresStaleMessages(t *testing.T) {
	app := &App{ctx: &context.ProgramContext{Config: config.GetDefaultConfig(), ConfigDir: t.TempDir(), Servers: []context.ServerConfig{{ID: "id", Name: "Local", APIToken: "never-in-form"}}}, wWidth: 100}
	_ = app.openServerSetup("id")
	d, ok := app.dialog.(*serversetup.Dialog)
	if !ok {
		t.Fatal("edit did not open setup")
	}
	_ = app.cancelServerSetup(serversetup.CancelMsg{Dialog: d})
	if _, ok := app.dialog.(*serverselector.Dialog); !ok {
		t.Fatal("cancel did not return to selector")
	}
	if app.reloadSetupServer(serversetup.SavedMsg{Dialog: d}) != nil {
		t.Fatal("stale save accepted")
	}
	if app.connectSetupServer(setupServersLoadedMsg{dialog: d}) != nil {
		t.Fatal("stale server reload accepted")
	}
}
