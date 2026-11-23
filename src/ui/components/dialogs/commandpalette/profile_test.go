package commandpalette

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/ui/perfprofile"
	"github.com/atm1020/jenktui/src/ui/util"
)

type (
	profilePage struct{ *Dialog }
	filterTick  struct{}
)

func (p *profilePage) ID() string { return "command-palette-profile" }

func (p *profilePage) advance() {
	if p.input.Value() == "" {
		_, _ = p.Dialog.Update(tea.PasteMsg{Content: "apprnce"})
	} else {
		_, _ = p.Dialog.Update(tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl})
	}
}

func (p *profilePage) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	if _, ok := msg.(filterTick); ok {
		p.advance()
		return p, filterCommand()
	}
	_, cmd := p.Dialog.Update(msg)
	return p, cmd
}

func filterCommand() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return filterTick{} })
}

func TestPaletteProfile(t *testing.T) {
	perfprofile.Run(t, func() perfprofile.Scenario {
		p := &profilePage{testPalette()}
		return perfprofile.Scenario{Page: p, Start: filterCommand(), Width: 100, Height: 30, Metrics: map[string]any{
			"scenario": "command-palette-filter-v1", "replay_benchmark": "BenchmarkPaletteFrame", "replay_views_per_op": 1,
			"input_interval_ms": 100,
		}}
	})
}

func BenchmarkPaletteFrame(b *testing.B) {
	p := &profilePage{testPalette()}
	_ = p.Init()
	b.Cleanup(func() { _ = p.CleanUp() })
	b.ReportAllocs()
	for b.Loop() {
		p.advance()
		_ = p.View()
	}
	b.ReportMetric(1, "updates/op")
	b.ReportMetric(1, "views/op")
}
