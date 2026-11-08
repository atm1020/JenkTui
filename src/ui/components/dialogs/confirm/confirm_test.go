package confirm

import (
	"regexp"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/context"

	"charm.land/lipgloss/v2"
)

var ansi = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// buttonRowGaps returns the rendered button line's distance from the left and
// right inner edges of the dialog, with styling stripped. Both buttons must
// share that one line.
func buttonRowGaps(t *testing.T, view string) (left, right int) {
	t.Helper()
	for _, line := range strings.Split(ansi.ReplaceAllString(view, ""), "\n") {
		if !strings.Contains(line, "Yes") {
			continue
		}
		if !strings.Contains(line, "No") {
			t.Fatalf("buttons were split across lines:\n%s", ansi.ReplaceAllString(view, ""))
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(line, "│"), "│")
		return len(inner) - len(strings.TrimLeft(inner, " ")),
			len(inner) - len(strings.TrimRight(inner, " "))
	}
	t.Fatalf("no button row in:\n%s", ansi.ReplaceAllString(view, ""))
	return 0, 0
}

// The buttons belong against the dialog's right edge at every size. Laying them
// out on the question's width instead left them stranded mid-box whenever the
// dialog was wider than its question (every reserved-width trigger dialog), and
// wrapped them onto separate lines whenever the question was shorter than the
// row (any brief confirmation).
func TestButtonsHugTheRightEdgeRegardlessOfQuestionAndWidth(t *testing.T) {
	keymap := context.ConfirmDialogKeyMapConfig{Close: context.KeyBinding{"esc"}}
	questions := map[string]string{
		"shorter than the button row": "Delete?",
		"typical":                     "Are you sure you want to trigger a new build for post-fail?",
		"wider than the dialog":       strings.Repeat("a very long confirmation question ", 6) + "?",
	}

	var want int
	for _, windowWidth := range []int{60, 100, 160} {
		for name, question := range questions {
			dialog := NewConfirmDialog(question, nil, nil, keymap)
			dialog.SetWindowWidth(windowWidth)

			left, right := buttonRowGaps(t, dialog.View())
			if left <= right {
				t.Errorf("window %d, %s: buttons are not right-aligned (left gap %d, right gap %d)",
					windowWidth, name, left, right)
			}
			// The gap to the right edge is the dialog's own padding, so it must
			// not grow with the window or shrink with the question.
			if want == 0 {
				want = right
			}
			if right != want {
				t.Errorf("window %d, %s: right gap = %d, want %d everywhere",
					windowWidth, name, right, want)
			}
		}
	}
}

// A confirmation is sized by its question, not by the window. Putting it on the
// shared list-dialog width left a one-line question stranded in a box twice the
// width it needed.
func TestConfirmWidthTracksTheQuestionNotTheWindow(t *testing.T) {
	keymap := context.ConfirmDialogKeyMapConfig{Close: context.KeyBinding{"esc"}}
	question := "Are you sure you want to trigger a new build for post-fail?"

	var want int
	for _, windowWidth := range []int{100, 160, 220} {
		dialog := NewConfirmDialog(question, nil, nil, keymap)
		dialog.SetWindowWidth(windowWidth)
		got := lipgloss.Width(dialog.View())
		if want == 0 {
			want = got
		}
		if got != want {
			t.Errorf("window %d: width = %d, want %d — the question did not change", windowWidth, got, want)
		}
		if got > lipgloss.Width(question)+16 {
			t.Errorf("window %d: width %d dwarfs its %d-cell question", windowWidth, got, lipgloss.Width(question))
		}
	}
}
