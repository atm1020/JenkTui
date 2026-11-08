package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/ui/components/footer"
	"github.com/atm1020/jenktui/src/ui/components/header"
	"github.com/atm1020/jenktui/src/ui/router"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
)

// countingDialog records how often the shell releases it.
type countingDialog struct {
	cleanups int
}

func (d *countingDialog) Init() tea.Cmd { return nil }
func (d *countingDialog) Update(tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	return d, nil
}
func (d *countingDialog) View() string           { return "" }
func (d *countingDialog) UpdateFooter() tea.Cmd  { return nil }
func (d *countingDialog) CleanUp() tea.Cmd       { d.cleanups++; return nil }
func (d *countingDialog) Dimensions() (int, int) { return 0, 0 }
func (d *countingDialog) KeyMap() help.KeyMap    { return nil }

type tallDialog struct{ countingDialog }

func (d *tallDialog) View() string {
	return strings.Repeat("dialog line\n", 40)
}
func (d *tallDialog) Dimensions() (int, int) { return 20, 40 }

func TestShellCleansUpDialogOnEveryReplacement(t *testing.T) {
	cases := map[string]func(*App){
		"swap":     func(a *App) { a.handleOpenDialog(util.OpenDialogMsg{Dialog: &countingDialog{}}) },
		"close":    func(a *App) { a.handleCloseDialog(util.CloseDialogMsg{}) },
		"navigate": func(a *App) { a.handleNavigate(router.NavigateMsg{Model: testViewModel{}}) },
		"error":    func(a *App) { a.handleError(util.ErrorMsg{Error: errors.New("boom"), Title: "Error"}) },
	}
	for name, replace := range cases {
		t.Run(name, func(t *testing.T) {
			app := buildNavigationApp(t)
			old := &countingDialog{}
			app.handleOpenDialog(util.OpenDialogMsg{Dialog: old})

			replace(app)

			if old.cleanups != 1 {
				t.Fatalf("CleanUp calls = %d, want 1", old.cleanups)
			}
		})
	}
}

func TestDialogDoesNotCoverTheFooter(t *testing.T) {
	app := buildNavigationApp(t)
	app.maxHeight = 12
	app.header = header.NewModel()
	app.footer = footer.NewModel(app.ctx, "ada")
	_, _ = app.footer.Update(tea.WindowSizeMsg{Width: app.wWidth, Height: app.maxHeight})
	_, _ = app.footer.Update(util.FooterUrlMsg{Url: "DevTeam / mobile-app"})
	app.dialog = &tallDialog{}

	view := app.View().Content
	lines := strings.Split(view, "\n")
	if len(lines) == 0 || !strings.Contains(lines[len(lines)-1], "ada") || !strings.Contains(lines[len(lines)-1], "mobile-app") {
		t.Fatalf("footer is not the bottom line:\n%s", view)
	}
}

func TestStaleCloseDoesNotDismissReplacementDialog(t *testing.T) {
	app := buildNavigationApp(t)
	old := &countingDialog{}
	next := &countingDialog{}
	app.handleOpenDialog(util.OpenDialogMsg{Dialog: old})
	app.handleOpenDialog(util.OpenDialogMsg{Dialog: next})

	if cmd := app.handleCloseDialog(util.CloseDialogMsg{Dialog: old}); cmd != nil {
		t.Fatal("stale close returned a command")
	}
	if app.dialog != next {
		t.Fatal("stale close dismissed the dialog that replaced it")
	}
	if next.cleanups != 0 {
		t.Fatalf("replacement CleanUp calls = %d, want 0", next.cleanups)
	}
}
