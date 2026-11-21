package ui

import (
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/commandpalette"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/globalsearch"
	"github.com/atm1020/jenktui/src/ui/components/favorites"
	"github.com/atm1020/jenktui/src/ui/components/myruns"
	"github.com/atm1020/jenktui/src/ui/router"
	"github.com/atm1020/jenktui/src/ui/util"
)

func (a *App) openCommandPalette() tea.Cmd {
	entry := func(action commandpalette.Action, title, aliases string, binding key.Binding) commandpalette.Entry {
		return commandpalette.Entry{Action: action, Title: title, Aliases: aliases, Shortcut: binding.Help().Key}
	}
	entries := []commandpalette.Entry{
		entry(commandpalette.SearchJenkins, "Search Jenkins", "global search jobs folders", a.keyMap.GlobalSearch),
		entry(commandpalette.Favorites, "Favorites", "bookmarks starred", a.keyMap.Favorites),
		entry(commandpalette.MyRuns, "My Runs", "history builds", a.keyMap.MyRuns),
		entry(commandpalette.SwitchServer, "Switch Server", "change jenkins connection", a.keyMap.ServerSelector),
		entry(commandpalette.ChangeTheme, "Change Theme", "appearance colors", a.keyMap.ThemeSelector),
		{Action: commandpalette.NotificationStyle, Title: "Notification Style", Aliases: "notifications desktop sound bell settings"},
		entry(commandpalette.Help, "Show Help", "keybindings shortcuts", a.keyMap.Help),
	}
	dialog := commandpalette.New(entries, a.wWidth, a.wHeight, a.ctx.Config.KeyMapConfig.Dialog.CommandPalette)
	return a.handleOpenDialog(util.OpenDialogMsg{Dialog: dialog})
}

func (a *App) handlePaletteSelection(msg commandpalette.SelectedMsg) tea.Cmd {
	if msg.Dialog == nil || msg.Dialog != a.dialog {
		return nil
	}
	cleanup := a.replaceDialog(nil)
	return tea.Batch(cleanup, a.runAppAction(msg.Action))
}

func (a *App) runAppAction(action commandpalette.Action) tea.Cmd {
	switch action {
	case commandpalette.SearchJenkins:
		dialog := globalsearch.NewGlobalSearchDialogCmp(a.ctx, a.wWidth, a.wHeight)
		return a.handleOpenDialog(util.OpenDialogMsg{Dialog: dialog})
	case commandpalette.SwitchServer:
		return a.handleOpenDialog(util.OpenDialogMsg{Dialog: a.newServerSelectorDialog()})
	case commandpalette.ChangeTheme:
		// Re-scan custom theme files when opening the picker, outside Update.
		return loadThemeDefinitions(a.ctx.ConfigDir)
	case commandpalette.NotificationStyle:
		dialog := commandpalette.NewNotificationStyle(a.ctx.Config.Notification, a.wWidth, a.wHeight, a.ctx.Config.KeyMapConfig.Dialog.CommandPalette)
		return a.handleOpenDialog(util.OpenDialogMsg{Dialog: dialog})
	case commandpalette.NotificationsOff, commandpalette.NotificationsSilent, commandpalette.NotificationsBell:
		return a.changeNotificationStyle(action)
	case commandpalette.MyRuns:
		return a.openMyRuns()
	case commandpalette.Favorites:
		return util.CmdHandler(router.NavigateMsg{Model: favorites.NewModel(a.ctx, a.wWidth, a.wHeight)})
	case commandpalette.Help:
		return a.setHelpLevel(HelpFull)
	}
	return nil
}

func (a *App) openMyRuns() tea.Cmd {
	if provider, ok := a.router.CurrentView().(interface{ RunHistoryQuery() store.RunQuery }); ok {
		model := myruns.NewModelWithQuery(a.ctx, a.wWidth, a.wHeight, provider.RunHistoryQuery())
		return util.CmdHandler(router.NavigateMsg{Model: model})
	}
	return util.CmdHandler(router.NavigateMsg{Model: myruns.NewModel(a.ctx, a.wWidth, a.wHeight)})
}
