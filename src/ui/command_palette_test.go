package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/commandpalette"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/globalsearch"
)

func TestCommandPaletteOpensSearchWithoutChangingItsShortcut(t *testing.T) {
	a := buildNavigationApp(t)
	_, _ = a.Update(tea.KeyPressMsg{Code: 'p', Text: "p"})
	if a.dialog != nil {
		t.Fatalf("lowercase p opened %T", a.dialog)
	}
	_, _ = a.Update(tea.KeyPressMsg{Code: 'P', Text: "P"})
	d, ok := a.dialog.(*commandpalette.Dialog)
	if !ok {
		t.Fatalf("P opened %T", a.dialog)
	}
	_, _ = a.Update(tea.PasteMsg{Content: "jobs"})
	_, cmd := a.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("no selection")
	}
	_, _ = a.Update(cmd())
	search, ok := a.dialog.(*globalsearch.GlobalSearchDialogCmp)
	if !ok {
		t.Fatalf("palette selection opened %T", a.dialog)
	}
	_, _ = a.Update(commandpalette.SelectedMsg{Dialog: d, Action: commandpalette.Help})
	if a.dialog != search || a.helpLevel != HelpHidden {
		t.Fatal("stale palette selection changed the UI")
	}
	a.replaceDialog(nil)
	_, _ = a.Update(tea.KeyPressMsg{Code: 'p', Mod: tea.ModCtrl})
	if _, ok := a.dialog.(*globalsearch.GlobalSearchDialogCmp); !ok {
		t.Fatalf("Ctrl+P opened %T", a.dialog)
	}
}
