package commandpalette

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/ui/util"
	"github.com/charmbracelet/x/ansi"
)

func testPalette() *Dialog {
	return New([]Entry{
		{Action: SearchJenkins, Title: "Search Jenkins", Shortcut: "ctrl+p"},
		{Action: MyRuns, Title: "My Runs", Aliases: "history", Shortcut: "m"},
		{Action: ChangeTheme, Title: "Change Theme", Aliases: "appearance", Shortcut: "ctrl+t"},
	}, 100, 30, config.GetDefaultConfig().KeyMapConfig.Dialog.CommandPalette)
}

func TestPaletteUsesChooserLayout(t *testing.T) {
	d := testPalette()
	view := ansi.Strip(d.View())
	divider := "│  " + strings.Repeat("─", d.width) + "  │"
	if strings.Count(view, divider) != 3 {
		t.Fatalf("expected chooser-style dividers above input, commands, and help:\n%s", view)
	}
	if !strings.Contains(view, "   Command Palette") || !strings.Contains(view, "   Search Jenkins") {
		t.Fatalf("expected chooser-style inset title and rows:\n%s", view)
	}
}

func TestPaletteFiltersAndSelects(t *testing.T) {
	d := testPalette()
	_ = d.Init()
	t.Cleanup(func() { _ = d.CleanUp() })
	if !strings.Contains(d.View(), "ctrl+t") {
		t.Fatal("missing configured action shortcut")
	}
	_, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	assertAction(t, d, MyRuns)
	_, _ = d.Update(tea.PasteMsg{Content: "apprnce"})
	assertAction(t, d, ChangeTheme)
	_, _ = d.Update(tea.PasteMsg{Content: "zzzzz"})
	if !strings.Contains(d.View(), "No matching commands") {
		t.Fatal("missing empty-state message")
	}
	if _, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter}); cmd != nil {
		t.Fatal("Enter should do nothing with no matches")
	}
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if msg, ok := cmd().(util.CloseDialogMsg); !ok || msg.Dialog != d {
		t.Fatal("Esc did not request dismissal of this palette")
	}
}

func assertAction(t *testing.T, d *Dialog, action Action) {
	t.Helper()
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a selection command")
	}
	if msg, ok := cmd().(SelectedMsg); !ok || msg.Action != action || msg.Dialog != d {
		t.Fatalf("expected selection of action %v, got %+v", action, msg)
	}
}

func TestPaletteKeepsSelectionVisibleInShortWindow(t *testing.T) {
	d := testPalette()
	_, _ = d.Update(tea.WindowSizeMsg{Width: 30, Height: 11})
	_, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	_, _ = d.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	w, h := d.Dimensions()
	if w > 30 || h > 8 || !strings.Contains(d.View(), "Change Theme") {
		t.Fatalf("selected row must fit within the overlay: %dx%d\n%s", w, h, d.View())
	}
}
