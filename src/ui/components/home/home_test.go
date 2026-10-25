package home

import (
	"testing"

	"github.com/atm1020/jenktui/src/context"

	tea "charm.land/bubbletea/v2"
)

func TestUpdateWindowSizeMutatesModel(t *testing.T) {
	m := NewModel(&context.ProgramContext{Config: &context.Config{}})

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if cmd != nil {
		t.Fatalf("expected no command, got %T", cmd)
	}
	if updated != m {
		t.Fatalf("expected Update to return the same model pointer")
	}
	if m.wWidth != 120 || m.wHeight != 40 {
		t.Fatalf("expected window size 120x40, got %dx%d", m.wWidth, m.wHeight)
	}
}
