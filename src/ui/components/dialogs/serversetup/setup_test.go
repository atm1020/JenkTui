package serversetup

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/ui/util"
)

func newTestDialog(t *testing.T, settings Settings) *Dialog {
	t.Helper()
	d := New(t.TempDir(), settings, 100, config.GetDefaultConfig().KeyMapConfig.Dialog.ServerSetup)
	_ = d.Init()
	return d
}

func press(d *Dialog, code rune) tea.Cmd { _, cmd := d.Update(tea.KeyPressMsg{Code: code}); return cmd }

func TestEditFormNeverLoadsStoredToken(t *testing.T) {
	d := newTestDialog(t, Settings{ID: "stable", Name: "Local", URL: "http://localhost", Username: "user"})
	if d.inputs[3].Value() != "" || d.inputs[3].Placeholder != "**.." {
		t.Fatal("edit token must be an empty placeholder")
	}
	d.inputs[3].SetValue("replacement-secret")
	if strings.Contains(d.View(), "replacement-secret") {
		t.Fatal("token rendered in clear text")
	}
	if d.id != "stable" {
		t.Fatal("edit changed server identity")
	}
	_ = d.CleanUp()
	if d.inputs[3].Value() != "" {
		t.Fatal("cleanup retained replacement token")
	}
}

func TestValidationFailureRetainsFormAndAllowsRetry(t *testing.T) {
	d := newTestDialog(t, Settings{})
	d.inputs[0].SetValue("Local")
	d.inputs[3].SetValue("replacement-secret")
	cmd := press(d, tea.KeyEnter)
	if cmd == nil || !d.testing {
		t.Fatal("submit did not start test")
	}
	_, next := d.Update(cmd())
	if next != nil || d.testing || d.saving || d.errorText == "" {
		t.Fatal("invalid form did not show inline error")
	}
	if d.inputs[0].Value() != "Local" || d.inputs[3].Value() != "replacement-secret" {
		t.Fatal("failed test lost entered values")
	}
	if press(d, tea.KeyEnter) == nil {
		t.Fatal("retry not allowed")
	}
	_ = d.CleanUp()
}

func TestCanceledOrStaleValidationCannotSave(t *testing.T) {
	d := newTestDialog(t, Settings{})
	_ = press(d, tea.KeyEnter)
	cancel := press(d, tea.KeyEscape)
	if cancel == nil {
		t.Fatal("cancel ignored")
	}
	if msg, ok := cancel().(CancelMsg); !ok || msg.Dialog != d {
		t.Fatal("cancel did not identify dialog")
	}
	_, cmd := d.Update(verifiedMsg{owner: d})
	if cmd != nil || d.saving {
		t.Fatal("canceled validation started a save")
	}
	other := newTestDialog(t, Settings{})
	other.testing = true
	_, cmd = other.Update(verifiedMsg{owner: d})
	if cmd != nil || other.saving {
		t.Fatal("stale validation started a save")
	}
}

func TestSaveFailureRetainsForm(t *testing.T) {
	d := newTestDialog(t, Settings{})
	d.inputs[0].SetValue("Local")
	d.saving = true
	if press(d, tea.KeyEscape) != nil || d.closed {
		t.Fatal("save must finish before cancel")
	}
	_, cmd := d.Update(savedMsg{owner: d, err: errors.New("Cannot save")})
	if cmd != nil || d.saving || d.errorText != "Cannot save" || d.inputs[0].Value() != "Local" {
		t.Fatal("save failure was not recoverable")
	}
}

func TestAdvancedAndResize(t *testing.T) {
	d := newTestDialog(t, Settings{})
	// Invoke the configured action rather than duplicating control-key encoding.
	_, _ = d.Update(tea.KeyPressMsg{Code: 'a', Mod: tea.ModCtrl})
	if !d.advanced || !strings.Contains(d.View(), "CA certificate") {
		t.Fatal("advanced setting inaccessible")
	}
	_, _ = d.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if width, _ := d.Dimensions(); width > 80 {
		t.Fatalf("dialog too wide: %d", width)
	}
	var _ util.JenkTuiDialogModel = d
}
