package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/commandpalette"
	"github.com/atm1020/jenktui/src/ui/util"
)

func (a *App) changeNotificationStyle(action commandpalette.Action) tea.Cmd {
	a.ctx.Config.Notification = context.NotificationConfig{
		DesktopEnabled: action != commandpalette.NotificationsOff,
		SoundEnabled:   action == commandpalette.NotificationsBell,
	}
	// Snapshot on the update loop; the command must not read mutable app state.
	cfg := *a.ctx.Config
	configDir := a.ctx.ConfigDir
	return func() tea.Msg {
		if err := config.SaveConfig(configDir, &cfg); err != nil {
			return util.FooterNotiMsg{Message: fmt.Sprintf("Notification style applied, but could not save: %v", err), Type: util.NotiError}
		}
		return util.FooterNotiMsg{Message: "Notification style saved"}
	}
}
