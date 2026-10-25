package artifactselector

import (
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

func testKeyMapConfig() context.ArtifactSelectorDialogKeyMapConfig {
	return context.ArtifactSelectorDialogKeyMapConfig{
		Up:          context.KeyBinding{"up", "k"},
		Down:        context.KeyBinding{"down", "j"},
		Toggle:      context.KeyBinding{"space"},
		Download:    context.KeyBinding{"enter"},
		DownloadAll: context.KeyBinding{"a"},
		Close:       context.KeyBinding{"esc"},
	}
}

func testArtifacts(names ...string) []gojenkins.Artifact {
	artifacts := make([]gojenkins.Artifact, len(names))
	for i, name := range names {
		artifacts[i].FileName = name
	}
	return artifacts
}

func keyRune(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg{Code: r, Text: string(r)}
}

func TestDialogDownloadsOnlyCheckedArtifacts(t *testing.T) {
	var downloaded []gojenkins.Artifact
	d := New(
		testArtifacts("one.zip", "two.zip", "three.zip"),
		80,
		24,
		func(artifacts []gojenkins.Artifact) tea.Cmd {
			downloaded = append([]gojenkins.Artifact(nil), artifacts...)
			return nil
		},
		testKeyMapConfig(),
	)

	_, _ = d.Update(keyRune('j'))
	_, _ = d.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if cmd == nil {
		t.Fatal("downloading a checked artifact returned no command")
	}
	if len(downloaded) != 1 || downloaded[0].FileName != "two.zip" {
		t.Fatalf("downloaded artifacts = %#v, want only two.zip", downloaded)
	}
	if msg := cmd(); msg != (util.CloseDialogMsg{}) {
		t.Fatalf("submit command produced %T, want CloseDialogMsg", msg)
	}
}

func TestDialogDownloadAllShortcutDownloadsEveryArtifact(t *testing.T) {
	var downloaded []gojenkins.Artifact
	d := New(
		testArtifacts("one.zip", "two.zip", "three.zip"),
		80,
		24,
		func(artifacts []gojenkins.Artifact) tea.Cmd {
			downloaded = append([]gojenkins.Artifact(nil), artifacts...)
			return nil
		},
		testKeyMapConfig(),
	)

	_, cmd := d.Update(keyRune('a'))

	if cmd == nil {
		t.Fatal("download-all shortcut returned no command")
	}
	if len(downloaded) != 3 {
		t.Fatalf("downloaded %d artifacts, want all 3", len(downloaded))
	}
}

func TestDialogRequiresAtLeastOneCheckedArtifact(t *testing.T) {
	called := false
	d := New(
		testArtifacts("one.zip", "two.zip"),
		80,
		24,
		func([]gojenkins.Artifact) tea.Cmd {
			called = true
			return nil
		},
		testKeyMapConfig(),
	)

	_, cmd := d.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if called {
		t.Fatal("download callback ran with no checked artifacts")
	}
	if cmd == nil {
		t.Fatal("empty selection returned no feedback command")
	}
	msg, ok := cmd().(util.FooterNotiMsg)
	if !ok || msg.Type != util.NotiWarning {
		t.Fatalf("empty selection feedback = %#v, want warning", msg)
	}
	if got := ansi.Strip(d.View()); !strings.Contains(got, "Select at least one artifact") {
		t.Fatalf("dialog does not show selection error: %q", got)
	}
}

func TestDialogRendersCheckboxesAndScrollsWithCursor(t *testing.T) {
	d := New(
		testArtifacts("one.zip", "two.zip", "three.zip", "four.zip"),
		44,
		12,
		nil,
		testKeyMapConfig(),
	)

	initial := ansi.Strip(d.View())
	if !strings.Contains(initial, "[ ] one.zip") {
		t.Fatalf("dialog does not render an unchecked artifact: %q", initial)
	}

	for range 3 {
		_, _ = d.Update(keyRune('j'))
	}
	_, _ = d.Update(tea.KeyPressMsg{Code: tea.KeySpace, Text: " "})
	view := ansi.Strip(d.View())
	if !strings.Contains(view, "[x] four.zip") {
		t.Fatalf("dialog did not scroll to and check the fourth artifact: %q", view)
	}
	if strings.Contains(view, "one.zip") {
		t.Fatalf("off-screen first artifact is still rendered: %q", view)
	}
}

func TestDialogDimensionsStayInsideSmallWindow(t *testing.T) {
	const width, height = 20, 8
	d := New(testArtifacts("a-very-long-artifact-name.zip"), width, height, nil, testKeyMapConfig())

	dialogWidth, dialogHeight := d.Dimensions()
	if dialogWidth > width || dialogHeight > height {
		t.Fatalf("dialog dimensions = %dx%d, exceed %dx%d window", dialogWidth, dialogHeight, width, height)
	}
	if got := lipgloss.Width(d.View()); got != dialogWidth {
		t.Fatalf("reported width = %d, rendered width = %d", dialogWidth, got)
	}
}
