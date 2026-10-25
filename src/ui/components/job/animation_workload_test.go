package job

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestAnimationWorkloadRendersAndAdvances(t *testing.T) {
	w := newAnimationWorkload(t)
	before := w.page.View()
	for _, text := range []string{"animation-profile", "Health Score: 80%", "Builds", "#120", "Stage 1", "█"} {
		if !strings.Contains(ansi.Strip(before), text) {
			t.Fatalf("workload is missing visible %q:\n%s", text, before)
		}
	}
	// One frame must advance every active animation. A replay that only
	// changes one of them would measure a different operation than production.
	header := w.page.spinnerModel.View()
	stage := w.page.stageView.spinnerModel.View()
	pulse := w.page.stageView.pulseBar.View()
	w.advance()
	after := w.page.View()
	if after == before {
		t.Fatal("animation frame did not change the page")
	}
	if w.page.spinnerModel.View() == header || w.page.stageView.spinnerModel.View() == stage || w.page.stageView.pulseBar.View() == pulse {
		t.Fatal("animation frame did not advance header, stage, and pulse together")
	}
	if !strings.Contains(after, w.page.spinnerModel.View()) ||
		!strings.Contains(after, w.page.stageView.spinnerModel.View()) ||
		!strings.Contains(after, w.page.stageView.pulseBar.View()) {
		t.Fatal("animation frame did not render the advanced header, stage, and pulse")
	}
}
