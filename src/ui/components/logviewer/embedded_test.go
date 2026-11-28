package logviewer

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestEmbeddedModelRendersCompactToolbarAndExitsSearchOnEsc(t *testing.T) {
	_, path := testModelWithFile(t, 25)
	m := NewEmbeddedModel(testContext(), 110, 18, path)
	t.Cleanup(func() { _ = m.CleanUp() })

	m.SetEmbeddedToolbar("full", false)
	_, cmd := m.Update(FileReadyMsg{FilePath: path, LineCount: 25})
	if cmd == nil {
		t.Fatal("FileReadyMsg did not return a chunk load command")
	}
	// Load initial lines directly via ChunkLoadedMsg.
	lines := make([]string, 25)
	for i := range lines {
		lines[i] = DefaultLogStyle("[INFO] line output")
	}
	_, _ = m.Update(ChunkLoadedMsg{Lines: lines})

	if got := m.LineCountLabel(); got != "25 lines" {
		t.Fatalf("LineCountLabel() = %q, want %q", got, "25 lines")
	}

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "[1] full") || !strings.Contains(view, "[2] stage") || !strings.Contains(view, "[4] outside") || !strings.Contains(view, "Loaded:") {
		t.Fatalf("embedded view missing compact toolbar or stats:\n%s", view)
	}
	for i, line := range strings.Split(view, "\n") {
		if w := ansi.StringWidth(line); w > 110 {
			t.Fatalf("embedded line %d overflowed width 110 (got %d): %q", i, w, line)
		}
	}

	// FocusSearch enters SearchView; Esc exits cleanly back to LogView.
	_ = m.FocusSearch()
	if !m.InSearchView() || !m.IsCapturingInput() {
		t.Fatal("FocusSearch did not enter search capture mode")
	}
	_, _ = m.Update(makeKeyPress("esc"))
	if m.InSearchView() || m.IsCapturingInput() {
		t.Fatal("Esc did not exit SearchView")
	}

	// Resize shrinks cleanly without overflowing narrow widths.
	m.Resize(48, 10)
	narrow := ansi.Strip(m.View())
	for i, line := range strings.Split(narrow, "\n") {
		if w := ansi.StringWidth(line); w > 48 {
			t.Fatalf("narrow embedded line %d overflowed width 48 (got %d): %q", i, w, line)
		}
	}
}
