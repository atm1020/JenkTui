package ui

import (
	"errors"
	"testing"

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

func TestShellCleansUpDialogOnEveryReplacement(t *testing.T) {
	cases := map[string]func(*App){
		"swap":     func(a *App) { a.handleOpenDialog(util.OpenDialogMsg{Dialog: &countingDialog{}}) },
		"close":    func(a *App) { a.handleCloseDialog() },
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
