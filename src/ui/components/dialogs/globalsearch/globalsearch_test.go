package globalsearch

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	appcontext "github.com/atm1020/jenktui/src/context"

	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

func TestScopeDividerAndCommandHelp(t *testing.T) {
	ctx := &appcontext.ProgramContext{Config: &appcontext.Config{}}
	ctx.Config.KeyMapConfig.Dialog.Globalsearch = appcontext.GlobalsearchDialogKeyMapConfig{
		ToggleScope: appcontext.KeyBinding{"S"}, Search: appcontext.KeyBinding{"enter"},
		Select: appcontext.KeyBinding{"enter"},
	}
	dialog := NewFavoritesDialog(ctx, 160, 45)
	for _, width := range []int{100, 40, 5} {
		dialog.wWidth = width
		line := ansi.Strip(dialog.scopeDivider("Favorites", nil))
		if lipgloss.Width(line) != width || (width >= 40 && line != strings.Repeat("─", width-len(" Favorites"))+" Favorites") {
			t.Errorf("invalid scope divider at width %d: %q", width, line)
		}
	}
	dialog.wWidth = 100
	for _, want := range []string{"enter focus results", "enter select"} {
		view := ansi.Strip(dialog.View())
		divider := strings.Index(view, "─────── Favorites")
		results := strings.Index(view, "No saved favorites")
		if divider < 0 || results < 0 || divider > results {
			t.Error("expected the scope divider above the results")
		}
		found := false
		for _, line := range strings.Split(view, "\n") {
			found = found || (strings.Contains(line, "S switch scope") && strings.Contains(line, want))
		}
		if !found {
			t.Errorf("expected scope shortcut alongside %q", want)
		}
		dialog.focusToSearchResult()
	}
}

type deadlineRecordingTransport struct {
	sawDeadline bool
}

func (t *deadlineRecordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	_, t.sawDeadline = request.Context().Deadline()
	return nil, fmt.Errorf("stop after inspecting request context")
}

func TestGlobalSearchUsesRequestDeadline(t *testing.T) {
	transport := &deadlineRecordingTransport{}
	jenkins := gojenkins.CreateJenkins(&http.Client{Transport: transport}, "http://jenkins")
	dialog := &GlobalSearchDialogCmp{
		ctx:            &appcontext.ProgramContext{Jenkins: jenkins},
		isLegacySearch: true,
	}

	_ = dialog.fetchGlobalSearchResults("demo")()

	if !transport.sawDeadline {
		t.Fatal("global search Jenkins request had no deadline")
	}
}
