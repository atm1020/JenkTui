package serversetup

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/ui/perfprofile"
	"github.com/atm1020/jenktui/src/ui/util"
)

// profilePage adapts the dialog to the shared page harness. The live workload
// uses the real text cursor blink; no network, files, or credentials are involved.
type profilePage struct{ *Dialog }

func (p *profilePage) ID() string { return "server-setup-profile" }
func (p *profilePage) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	_, cmd := p.Dialog.Update(msg)
	return p, cmd
}

func newProfilePage() *profilePage {
	d := New("", Settings{Name: "Local Jenkins", URL: "http://localhost:8080", Username: "user"}, 160, config.GetDefaultConfig().KeyMapConfig.Dialog.ServerSetup)
	d.advanced = true
	return &profilePage{Dialog: d}
}

func TestServerSetupProfile(t *testing.T) {
	perfprofile.Run(t, func() perfprofile.Scenario {
		page := newProfilePage()
		return perfprofile.Scenario{Page: page, Start: page.Init(), Width: 160, Height: 50, Metrics: map[string]any{"scenario": "server-setup-v1", "replay_benchmark": "BenchmarkServerSetupFrame", "replay_views_per_op": 1, "fields": 5}}
	})
}

func BenchmarkServerSetupFrame(b *testing.B) {
	page := newProfilePage()
	_ = page.Init()
	b.Cleanup(func() { _ = page.CleanUp() })
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = page.move(1)
		_ = page.View()
	}
}
