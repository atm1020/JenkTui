package perfprofile_test

import (
	"bytes"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/ui/perfprofile"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestTerminalSinkCountsNonEmptyWrites(t *testing.T) {
	var sink perfprofile.TerminalSink
	for _, p := range []string{"abc", "", "de"} {
		if n, err := sink.Write([]byte(p)); n != len(p) || err != nil {
			t.Fatalf("Write(%q) = %d, %v", p, n, err)
		}
	}
	if writes, written := sink.Totals(); writes != 2 || written != 5 {
		t.Fatalf("totals = %d writes, %d bytes; want 2, 5", writes, written)
	}
}

type tickMsg struct{}

// tickingPage changes its view on every tick so each frame reaches the
// terminal.
type tickingPage struct {
	frames  int
	cleaned bool
}

func (p *tickingPage) Init() tea.Cmd { return nil }

func (p *tickingPage) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	if _, ok := msg.(tickMsg); !ok {
		return p, nil
	}
	p.frames++
	return p, tea.Tick(20*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (p *tickingPage) View() string {
	return styles.GetTheme().S().Title.Render("profiled-page " + strings.Repeat("#", p.frames%10))
}

func (p *tickingPage) ID() string            { return "ticking" }
func (p *tickingPage) UpdateFooter() tea.Cmd { return nil }
func (p *tickingPage) CleanUp() tea.Cmd      { p.cleaned = true; return nil }
func (p *tickingPage) KeyMap() help.KeyMap   { return nil }

// The live profile must measure what a terminal receives: the real renderer,
// full colour, and the app shell's alt-screen/background view settings.
func TestProgramWritesStyledFrames(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	page := &tickingPage{}
	var captured bytes.Buffer
	sink := &perfprofile.TerminalSink{}
	model := perfprofile.NewModel(page, func() tea.Msg { return tickMsg{} }, 350*time.Millisecond)
	done := make(chan error, 1)
	go func() {
		_, err := perfprofile.NewProgram(model, io.MultiWriter(sink, &captured), 80, 24).Run()
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("profile program did not stop")
	}

	writes, written := sink.Totals()
	if writes < 2 || written == 0 {
		t.Fatalf("renderer wrote %d times, %d bytes; want frames after the first", writes, written)
	}
	out := captured.String()
	if int64(len(out)) != written {
		t.Fatalf("captured %d bytes, counted %d", len(out), written)
	}
	for _, seq := range []string{ansi.SetModeAltScreenSaveCursor, "38;2;", "\x1b]11;"} {
		if !strings.Contains(out, seq) {
			t.Fatalf("terminal output is missing %q", seq)
		}
	}
	if !strings.Contains(ansi.Strip(out), "profiled-page") {
		t.Fatal("terminal output does not contain the page")
	}
	if !page.cleaned {
		t.Fatal("profile end did not clean up the page")
	}
}
