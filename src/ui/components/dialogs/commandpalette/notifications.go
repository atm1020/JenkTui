package commandpalette

import "github.com/atm1020/jenktui/src/context"

func NewNotificationStyle(current context.NotificationConfig, width, height int, conf context.CommandPaletteDialogKeyMapConfig) *Dialog {
	entries := []Entry{
		{Action: NotificationsOff, Title: "Off", Aliases: "disabled"},
		{Action: NotificationsSilent, Title: "On without bell", Aliases: "enabled silent"},
		{Action: NotificationsBell, Title: "On with bell", Aliases: "enabled sound"},
	}
	selected := 0
	if current.DesktopEnabled {
		selected = 1
		if current.SoundEnabled {
			selected = 2
		}
	}
	entries[selected].Shortcut = "current"
	d := New(entries, width, height, conf)
	d.title = "Notification Style"
	d.input.Placeholder = "Type to filter styles"
	d.cursor = selected
	return d
}
