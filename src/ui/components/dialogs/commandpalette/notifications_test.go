package commandpalette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"
)

func TestNotificationStyleChooser(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current context.NotificationConfig
		want    Action
	}{
		{"off", context.NotificationConfig{}, NotificationsOff},
		{"silent", context.NotificationConfig{DesktopEnabled: true}, NotificationsSilent},
		{"bell", context.NotificationConfig{DesktopEnabled: true, SoundEnabled: true}, NotificationsBell},
		{"disabled with stale sound", context.NotificationConfig{SoundEnabled: true}, NotificationsOff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := NewNotificationStyle(tc.current, 100, 30, config.GetDefaultConfig().KeyMapConfig.Dialog.CommandPalette)
			if !strings.Contains(d.View(), "Notification Style") || !strings.Contains(d.View(), "current") {
				t.Fatal("missing heading or current marker")
			}
			_ = d.Init()
			assertAction(t, d, tc.want)
			_, _ = d.Update(tea.PasteMsg{Content: "silent"})
			assertAction(t, d, NotificationsSilent)
			_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
			if msg, ok := cmd().(util.CloseDialogMsg); !ok || msg.Dialog != d {
				t.Fatal("cancel did not close this chooser")
			}
			_, _ = d.Update(tea.WindowSizeMsg{Width: 30, Height: 11})
			if w, h := d.Dimensions(); w > 30 || h > 8 {
				t.Fatalf("chooser exceeds small terminal: %dx%d", w, h)
			}
		})
	}
}
