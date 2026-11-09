package job

import (
	"strings"
	"testing"

	"github.com/atm1020/gojenkins"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/poll"
)

func TestUnsupportedPageReleasesWatchAndStopsAnimation(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.KeyMapConfig.Global.Back = context.KeyBinding{"alt+left"}
	api := gojenkins.CreateJenkins(nil, "http://jenkins")
	manager := poll.NewManager(api, nil, poll.Config{})
	page := NewModelFromUrl(&context.ProgramContext{Config: cfg, Jenkins: api, PollManager: manager}, 100, 30, "http://jenkins/job/demo/")
	// Watch registration is synchronous; no scheduler needs to run for this
	// lifecycle assertion. The fetch command is deliberately not executed.
	_ = page.Init()
	if page.pollWatch == nil || !page.clock.Running() {
		t.Fatal("Init did not acquire watch and start loading animation")
	}
	_, _ = page.Update(JobLoadedMsg{Generation: page.requestGeneration, UnsupportedMessage: `Navigation to "demo" is not supported`})
	if page.pollWatch != nil || page.clock.Running() || page.loading {
		t.Fatal("unsupported state retained watch or animation")
	}
	if view := page.View(); !strings.Contains(view, "alt+left: Back returns") {
		t.Fatalf("hint ignores configured Back key:\n%s", view)
	}
	_ = page.CleanUp()
	_ = page.CleanUp()
}
