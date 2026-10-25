package block

import (
	"fmt"
	"testing"

	"charm.land/lipgloss/v2"
)

var positions = []lipgloss.Position{lipgloss.Top, 0.2, lipgloss.Center, 0.7, lipgloss.Bottom}

func fixtures() []string {
	bold := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ff8800"))
	return []string{
		"",
		"one",
		"a\nbbb\ncc",
		bold.Render("styled") + "\nplain line\n" + bold.Render("x"),
		"界面\nwide 字\n",
		"\n\n\nfour\n",
		lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1).Render("box\nbody"),
	}
}

func TestJoinHorizontalMatchesLipgloss(t *testing.T) {
	texts := fixtures()
	for _, pos := range positions {
		for i := range texts {
			for j := range texts {
				want := lipgloss.JoinHorizontal(pos, texts[i], texts[j], texts[(i+j)%len(texts)])
				got := JoinHorizontal(pos, Measure(texts[i]), Measure(texts[j]), Measure(texts[(i+j)%len(texts)]))
				assertBlock(t, fmt.Sprintf("pos %v blocks %d,%d", pos, i, j), got, want)
			}
		}
	}
}

func TestJoinVerticalMatchesLipgloss(t *testing.T) {
	texts := fixtures()
	for _, pos := range positions {
		for i := range texts {
			for j := range texts {
				want := lipgloss.JoinVertical(pos, texts[i], texts[j])
				got := JoinVertical(pos, Measure(texts[i]), Measure(texts[j]))
				assertBlock(t, fmt.Sprintf("pos %v blocks %d,%d", pos, i, j), got, want)
			}
		}
	}
}

func TestPlaceMatchesLipgloss(t *testing.T) {
	for _, h := range positions {
		for _, v := range positions {
			for i, text := range fixtures() {
				for _, size := range [][2]int{{0, 0}, {3, 2}, {13, 9}, {40, 12}} {
					want := lipgloss.Place(size[0], size[1], h, v, text)
					got := Place(size[0], size[1], h, v, Measure(text))
					assertBlock(t, fmt.Sprintf("h %v v %v block %d size %v", h, v, i, size), got, want)
				}
			}
		}
	}
}

func TestChromeMatchesStyleRender(t *testing.T) {
	styles := map[string]lipgloss.Style{
		"rounded": lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#00ff00")),
		"bottom":  lipgloss.NewStyle().Border(lipgloss.NormalBorder(), false, false, true, false),
		"padded":  lipgloss.NewStyle().Border(lipgloss.ThickBorder(), true, true, true, false).Padding(0, 2),
		"plain":   lipgloss.NewStyle(),
	}
	content := Place(12, 4, lipgloss.Left, lipgloss.Top, Measure("one\n"+lipgloss.NewStyle().Bold(true).Render("two")+"\n界"))
	for name, style := range styles {
		chrome, ok := NewChrome(style, 12)
		if !ok {
			t.Fatalf("%s: chrome probe failed", name)
		}
		got, ok := chrome.Wrap(content)
		if !ok {
			t.Fatalf("%s: chrome rejected exact-width content", name)
		}
		assertBlock(t, name, got, style.Render(content.String()))
	}
}

func TestChromeRejectsWhatItCannotReproduce(t *testing.T) {
	if _, ok := NewChrome(lipgloss.NewStyle().Padding(2, 0), 5); ok {
		t.Fatal("two-line vertical padding was accepted")
	}
	chrome, _ := NewChrome(lipgloss.NewStyle().Border(lipgloss.NormalBorder()), 5)
	if _, ok := chrome.Wrap(Measure("short")); !ok {
		t.Fatal("exact-width content was rejected")
	}
	if _, ok := chrome.Wrap(Measure("no")); ok {
		t.Fatal("content narrower than the probe was accepted")
	}
}

func TestMemoRendersOncePerKey(t *testing.T) {
	var m Memo[int]
	calls := 0
	render := func() Block { calls++; return Measure(fmt.Sprint(calls)) }
	first := m.Get(1, render)
	if again := m.Get(1, render); again.String() != first.String() || calls != 1 {
		t.Fatal("same key rendered twice")
	}
	if m.Get(2, render).String() != "2" || m.Renders() != 2 {
		t.Fatal("new key did not render")
	}
	if m.Get(1, render).String() != "3" {
		t.Fatal("a one-entry memo kept a key it replaced")
	}
	m.Reset()
	if m.Get(1, render).String() != "4" {
		t.Fatal("Reset kept the old section")
	}
}

func assertBlock(t *testing.T, name string, got Block, want string) {
	t.Helper()
	if got.String() != want {
		t.Fatalf("%s:\n got %q\nwant %q", name, got.String(), want)
	}
	check := Measure(want)
	if got.Width != check.Width {
		t.Fatalf("%s: width %d, want %d", name, got.Width, check.Width)
	}
	for i := range check.Widths {
		if got.Widths[i] != check.Widths[i] {
			t.Fatalf("%s: line %d width %d, want %d", name, i, got.Widths[i], check.Widths[i])
		}
	}
}

func TestFitMatchesSizedStyle(t *testing.T) {
	for i, text := range fixtures() {
		b := Measure(text)
		for _, size := range [][2]int{{b.Width, 0}, {b.Width + 3, b.Height()}, {b.Width + 1, b.Height() + 4}} {
			want := lipgloss.NewStyle().Width(size[0]).Height(size[1]).Render(text)
			assertBlock(t, fmt.Sprintf("block %d size %v", i, size), Fit(b, size[0], size[1]), want)
		}
	}
}

func TestConcatMatchesNewlineJoin(t *testing.T) {
	texts := fixtures()
	for i := range texts {
		for j := range texts {
			assertBlock(t, fmt.Sprintf("blocks %d,%d", i, j), Concat(Measure(texts[i]), Measure(texts[j])), texts[i]+"\n"+texts[j])
		}
	}
}

func TestMemoKeepsOneEntryPerAnimationFrame(t *testing.T) {
	m := Memo[int]{Keep: 3}
	calls := 0
	render := func() Block { calls++; return Measure("frame") }
	for range 4 {
		for frame := range 3 {
			_ = m.Get(frame, render)
		}
	}
	if calls != 3 {
		t.Fatalf("three cycling frames rendered %d times, want 3", calls)
	}
	_ = m.Get(99, render)
	if len(m.entries) != 1 {
		t.Fatalf("a key past Keep left %d entries, want only the new one", len(m.entries))
	}
}
