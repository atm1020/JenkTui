package paramsview

import (
	"testing"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

func testKeymapConf() context.ParamsViewDialogKeyMapConfig {
	return context.ParamsViewDialogKeyMapConfig{
		Close: context.KeyBinding{"esc"},
	}
}

func TestNewParamsDialog(t *testing.T) {
	params := []gojenkins.BuildParameter{
		{Name: "BRANCH", Value: "main"},
	}

	d := NewParamsDialog(params, 100, testKeymapConf())

	if d == nil {
		t.Fatal("NewParamsDialog returned nil")
		return
	}
	if len(d.params) != 1 {
		t.Errorf("params length = %d, want 1", len(d.params))
	}
	if d.width != 100 {
		t.Errorf("width = %d, want 100", d.width)
	}
	if d.closeHint != "esc to close" {
		t.Errorf("closeHint = %q, want %q", d.closeHint, "esc to close")
	}
}

func TestNewParamsDialog_EmptyParams(t *testing.T) {
	d := NewParamsDialog(nil, 80, testKeymapConf())

	if d == nil {
		t.Fatal("NewParamsDialog returned nil")
		return
	}
	if len(d.params) != 0 {
		t.Errorf("params length = %d, want 0", len(d.params))
	}
}

func TestInit_ReturnsNil(t *testing.T) {
	d := NewParamsDialog(nil, 80, testKeymapConf())

	cmd := d.Init()
	if cmd != nil {
		t.Error("Init() should return nil")
	}
}

func TestUpdate_CloseKey_SendsCloseDialogMsg(t *testing.T) {
	d := NewParamsDialog(nil, 80, testKeymapConf())

	msg := tea.KeyPressMsg{Code: tea.KeyEscape}
	_, cmd := d.Update(msg)

	if cmd == nil {
		t.Fatal("Update with close key should return a cmd")
	}

	result := cmd()
	if _, ok := result.(util.CloseDialogMsg); !ok {
		t.Errorf("cmd should produce CloseDialogMsg, got %T", result)
	}
}

func TestUpdate_OtherKey_ReturnsNilCmd(t *testing.T) {
	d := NewParamsDialog(nil, 80, testKeymapConf())

	msg := tea.KeyPressMsg{Code: tea.KeyTab}
	returned, cmd := d.Update(msg)

	if cmd != nil {
		t.Error("Update with non-close key should return nil cmd")
	}
	if returned != d {
		t.Error("Update should return the same dialog")
	}
}

func TestUpdate_NonKeyMsg_ReturnsNilCmd(t *testing.T) {
	d := NewParamsDialog(nil, 80, testKeymapConf())

	_, cmd := d.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	if cmd != nil {
		t.Error("Update with non-key msg should return nil cmd")
	}
}

func TestView_EmptyParams_ShowsNoParamsMessage(t *testing.T) {
	d := NewParamsDialog(nil, 80, testKeymapConf())
	view := d.View()

	if view == "" {
		t.Error("View() should not return empty string")
	}
}

func TestView_WithParams_RendersContent(t *testing.T) {
	params := []gojenkins.BuildParameter{
		{Name: "BRANCH", Value: "main"},
		{Name: "ENV", Value: "staging"},
	}

	d := NewParamsDialog(params, 120, testKeymapConf())
	view := d.View()

	if view == "" {
		t.Error("View() should not return empty string")
	}
}

func TestUpdateFooter_ReturnsNil(t *testing.T) {
	d := NewParamsDialog(nil, 80, testKeymapConf())

	cmd := d.UpdateFooter()
	if cmd != nil {
		t.Error("UpdateFooter() should return nil")
	}
}

func TestKeyMap_ReturnsBindings(t *testing.T) {
	d := NewParamsDialog(nil, 80, testKeymapConf())
	km := d.KeyMap()

	bindings := km.ShortHelp()
	if len(bindings) != 1 {
		t.Errorf("ShortHelp bindings = %d, want 1", len(bindings))
	}
	if !key.Matches(tea.KeyPressMsg{Code: tea.KeyEscape}, bindings[0]) {
		t.Error("close binding should match escape key")
	}
}

func TestKeyMap_FullHelp(t *testing.T) {
	d := NewParamsDialog(nil, 80, testKeymapConf())
	km := d.KeyMap()

	groups := km.FullHelp()
	if len(groups) != 1 {
		t.Errorf("FullHelp groups = %d, want 1", len(groups))
	}
	if len(groups[0]) != 1 {
		t.Errorf("FullHelp group[0] bindings = %d, want 1", len(groups[0]))
	}
}

func TestDimensions_ReturnsPositiveValues(t *testing.T) {
	d := NewParamsDialog(nil, 80, testKeymapConf())
	w, h := d.Dimensions()

	if w <= 0 {
		t.Errorf("width = %d, want > 0", w)
	}
	if h <= 0 {
		t.Errorf("height = %d, want > 0", h)
	}
}

func TestDimensions_WithParams(t *testing.T) {
	params := []gojenkins.BuildParameter{
		{Name: "BRANCH", Value: "main"},
		{Name: "ENV", Value: "staging"},
	}

	d := NewParamsDialog(params, 120, testKeymapConf())
	w, h := d.Dimensions()

	if w <= 0 {
		t.Errorf("width = %d, want > 0", w)
	}
	if h <= 0 {
		t.Errorf("height = %d, want > 0", h)
	}
}

func TestUpdate_MultipleCloseKeys(t *testing.T) {
	conf := context.ParamsViewDialogKeyMapConfig{
		Close: context.KeyBinding{"esc", "q"},
	}
	d := NewParamsDialog(nil, 80, conf)

	// Test esc
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Error("esc should trigger close")
	}

	// Test q
	_, cmd = d.Update(tea.KeyPressMsg{Code: 'q'})
	if cmd == nil {
		t.Error("q should trigger close")
	}
}
