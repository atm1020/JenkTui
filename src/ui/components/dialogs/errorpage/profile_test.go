package errorpage

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/ui/perfprofile"
	"github.com/atm1020/jenktui/src/ui/util"
)

type (
	profilePage struct {
		*Model
		narrow bool
	}
	resizeTick struct{}
)

func (p *profilePage) ID() string { return "error-dialog-profile" }
func resizeCommand() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return resizeTick{} })
}

func (p *profilePage) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	if _, ok := msg.(resizeTick); ok {
		p.advance()
		return p, resizeCommand()
	}
	_, cmd := p.Model.Update(msg)
	return p, cmd
}

func (p *profilePage) advance() {
	p.narrow = !p.narrow
	width := 160
	if p.narrow {
		width = 150
	}
	_, _ = p.Model.Update(tea.WindowSizeMsg{Width: width, Height: 50})
}

func newProfilePage() *profilePage {
	return &profilePage{Model: NewModel(responseError{body: "<html>" + strings.Repeat("x", 256*1024-6), truncated: true}, "Failed to fetch build", 160, 50, config.GetDefaultConfig().KeyMapConfig.Dialog.Error)}
}

func TestErrorDialogProfile(t *testing.T) {
	perfprofile.Run(t, func() perfprofile.Scenario {
		p := newProfilePage()
		return perfprofile.Scenario{Page: p, Start: resizeCommand(), Width: 160, Height: 50, Metrics: map[string]any{
			"scenario": "error-dialog-resize-v1", "replay_benchmark": "BenchmarkErrorDialogResizeFrame", "replay_views_per_op": 1,
			"retained_html_bytes": 256 * 1024, "input_interval_ms": 100,
		}}
	})
}

func TestErrorDialogProfileChangesVisibleFrame(t *testing.T) {
	p := newProfilePage()
	t.Cleanup(func() { _ = p.CleanUp() })
	before := p.View()
	p.advance()
	if before == p.View() {
		t.Fatal("resize must change the visible frame")
	}
	if strings.Contains(before, "<html>") {
		t.Fatal("diagnostic HTML must not be rendered")
	}
}

func BenchmarkErrorDialogResizeFrame(b *testing.B) {
	p := newProfilePage()
	b.Cleanup(func() { _ = p.CleanUp() })
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		p.advance()
		_ = p.View()
	}
	b.ReportMetric(1, "updates/op")
	b.ReportMetric(1, "views/op")
}
