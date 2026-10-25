package footer

import (
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"

	tea "charm.land/bubbletea/v2"
)

func TestUpdateWindowSizeMutatesModel(t *testing.T) {
	m := NewModel(&context.ProgramContext{}, "jenkins-user")

	updated, cmd := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})

	if cmd != nil {
		t.Fatalf("expected no command, got %T", cmd)
	}
	if updated != m {
		t.Fatalf("expected Update to return the same model pointer")
	}
	if m.wWith != 120 {
		t.Fatalf("expected width 120, got %d", m.wWith)
	}
}

func TestViewColorsNotificationByType(t *testing.T) {
	m := NewModel(&context.ProgramContext{
		Config: &context.Config{
			KeyMapConfig: context.KeyMapConfig{
				Global: context.GlobalKeyMapConfig{
					HelpToggle: context.KeyBinding{"?"},
				},
			},
		},
	}, "jenkins-user")
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 1})

	tests := []struct {
		typ        util.NotiType
		background string
	}{
		{util.NotiSuccess, "48;2;63;185;80"},
		{util.NotiError, "48;2;248;81;73"},
		{util.NotiSkipped, "48;2;125;133;144"},
	}

	for _, tt := range tests {
		m.Update(util.FooterNotiMsg{Message: "Build #42 completed", Type: tt.typ})
		view := m.View()
		if !strings.Contains(view, tt.background) {
			t.Errorf("type %v view missing background %s\n%s", tt.typ, tt.background, view)
		}
	}
}
