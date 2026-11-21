package ui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/commandpalette"
	"github.com/atm1020/jenktui/src/ui/util"
)

func TestPaletteOpensSeparateNotificationChooser(t *testing.T) {
	a := buildNavigationApp(t)
	before := a.ctx.Config.Notification
	_, _ = a.Update(tea.KeyPressMsg{Code: 'P', Text: "P"})
	palette := a.dialog
	_, _ = a.Update(tea.PasteMsg{Content: "notification"})
	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	_, _ = a.Update(cmd())
	if a.dialog == palette || !strings.Contains(a.dialog.View(), "Notification Style") || strings.Contains(a.dialog.View(), "Command Palette") {
		t.Fatal("notification command did not replace palette with a separate chooser")
	}
	_, cmd = a.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	_, _ = a.Update(cmd())
	if a.dialog != nil || a.ctx.Config.Notification != before {
		t.Fatal("cancelling changed notifications or left the popup open")
	}
}

func TestNotificationStyleSaveFailureIsVisible(t *testing.T) {
	a := buildNavigationApp(t)
	path := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(path, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	a.ctx.ConfigDir = path
	msg := a.changeNotificationStyle(commandpalette.NotificationsBell)().(util.FooterNotiMsg)
	if msg.Type != util.NotiError || !strings.Contains(msg.Message, "could not save") {
		t.Fatalf("missing save failure: %+v", msg)
	}
	if !a.ctx.Config.Notification.SoundEnabled {
		t.Fatal("save failure should retain the in-memory choice")
	}
}

func TestNotificationStyleAppliesAndPersists(t *testing.T) {
	for _, tc := range []struct {
		action commandpalette.Action
		want   context.NotificationConfig
	}{
		{commandpalette.NotificationsOff, context.NotificationConfig{}},
		{commandpalette.NotificationsSilent, context.NotificationConfig{DesktopEnabled: true}},
		{commandpalette.NotificationsBell, context.NotificationConfig{DesktopEnabled: true, SoundEnabled: true}},
	} {
		a := buildNavigationApp(t)
		a.ctx.ConfigDir = t.TempDir()
		cmd := a.changeNotificationStyle(tc.action)
		if a.ctx.Config.Notification != tc.want {
			t.Fatalf("action %v: got %+v, want %+v", tc.action, a.ctx.Config.Notification, tc.want)
		}
		if _, err := os.Stat(filepath.Join(a.ctx.ConfigDir, config.ConfigFileName)); !os.IsNotExist(err) {
			t.Fatal("Update performed filesystem work")
		}
		msg := cmd().(util.FooterNotiMsg)
		if msg.Type == util.NotiError {
			t.Fatal(msg.Message)
		}
		loaded, err := config.LoadConfig(a.ctx.ConfigDir)
		if err != nil || loaded.Notification != tc.want {
			t.Fatalf("action %v did not persist: %v", tc.action, err)
		}
		info, err := os.Stat(filepath.Join(a.ctx.ConfigDir, config.ConfigFileName))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("config must be private: %v", err)
		}
	}
}
