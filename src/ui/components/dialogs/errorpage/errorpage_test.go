package errorpage

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"
)

type responseError struct {
	body      string
	truncated bool
}

func (e responseError) Error() string {
	return "Jenkins returned HTTP 500 Internal Server Error: HTML instead of JSON"
}
func (e responseError) HTMLResponseBody() (string, bool) { return e.body, e.truncated }

func TestCopyHTML(t *testing.T) {
	const body = "<!doctype html><html><body>diagnostic fixture</body></html>"
	conf := config.GetDefaultConfig().KeyMapConfig.Dialog.Error
	m := NewModel(fmt.Errorf("trigger: %w", responseError{body: body}), "Trigger failed", 160, 45, conf)
	if m.Init() != nil {
		t.Fatal("initialization must not copy HTML")
	}
	view := m.View()
	if strings.Contains(view, "<!doctype") || !strings.Contains(view, "sensitive data") || !m.keys.CopyHTML.Enabled() {
		t.Fatal("dialog must offer copying with a warning, not render HTML")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd == nil || !reflect.DeepEqual(cmd(), tea.SetClipboard(body)()) {
		t.Fatal("copy must send the exact original HTML to the clipboard")
	}
	if !strings.Contains(m.View(), "HTML sent to clipboard") {
		t.Fatal("missing copy feedback")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("escape must close the dialog")
	}
	if _, ok := cmd().(util.CloseDialogMsg); !ok {
		t.Fatal("expected close message")
	}
	m.CleanUp()
	if m.body != "" || m.keys.CopyHTML.Enabled() {
		t.Fatal("cleanup must release the diagnostic payload")
	}
}

func TestCopyHTMLUnavailable(t *testing.T) {
	m := NewModel(errors.New("network unavailable"), "Failed", 160, 45, config.GetDefaultConfig().KeyMapConfig.Dialog.Error)
	if m.keys.CopyHTML.Enabled() || strings.Contains(m.View(), "copy HTML") {
		t.Fatal("ordinary errors must not offer HTML copying")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("no clipboard command without HTML")
	}
}

func TestCopyHTMLCustomBindingAndTruncation(t *testing.T) {
	conf := context.ErrorDialogKeyMapConfig{CopyHTML: context.KeyBinding{"c"}, Close: context.KeyBinding{"q"}}
	m := NewModel(responseError{body: "<html>partial", truncated: true}, "Failed", 160, 45, conf)
	if !strings.Contains(m.View(), "truncated") || !strings.Contains(m.View(), "c copy HTML") {
		t.Fatal("missing truncation warning or configured binding")
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: 'y', Text: "y"})
	if cmd != nil {
		t.Fatal("default binding must not override custom config")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'c', Text: "c"})
	if cmd == nil || !reflect.DeepEqual(cmd(), tea.SetClipboard("<html>partial")()) {
		t.Fatal("custom copy binding did not work")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: 'q', Text: "q"})
	if cmd == nil {
		t.Fatal("custom close binding did not work")
	}
}
