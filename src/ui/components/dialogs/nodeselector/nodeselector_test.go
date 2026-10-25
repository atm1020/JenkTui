package nodeselector

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestNodeDelegateRendersLongNameInTwoLines(t *testing.T) {
	const width = 12
	item := NewNodeItem("jenkins-agent-from-template-with-a-very-long-pod-suffix-lz8kq", []string{"Build", "Integration"})
	delegate := NodeDelegate{width: width, horizontalLineWidth: width}
	model := list.New([]list.Item{item}, delegate, width, 2)

	var output bytes.Buffer
	delegate.Render(&output, model, 0, item)
	rendered := ansi.Strip(output.String())
	if got := lipgloss.Height(rendered); got != 2 {
		t.Fatalf("delegate height = %d, want 2: %q", got, rendered)
	}
	for _, line := range strings.Split(rendered, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("delegate line width = %d, want <= %d: %q", got, width, line)
		}
	}
}

func TestSelectCopiesRawBuiltInNodeAndCloses(t *testing.T) {
	cfg := config.GetDefaultConfig()
	dialog := New(
		&appcontext.ProgramContext{Config: cfg},
		[]NodeItem{NewNodeItem("", []string{"Build"})},
		80,
		30,
	)

	_, cmd := dialog.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("select returned nil command")
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 3 {
		t.Fatalf("select returned %T, want three-command BatchMsg", cmd())
	}
	if got, want := batch[0](), tea.SetClipboard("")(); !reflect.DeepEqual(got, want) {
		t.Fatalf("clipboard message = %#v, want %#v", got, want)
	}
	notification, ok := batch[1]().(util.FooterNotiMsg)
	if !ok || notification.Message != "Copied node name (built-in — empty)" {
		t.Fatalf("notification = %#v", notification)
	}
	if _, ok := batch[2]().(util.CloseDialogMsg); !ok {
		t.Fatalf("close message = %T, want CloseDialogMsg", batch[2]())
	}
}
