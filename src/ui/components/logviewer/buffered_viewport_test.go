package logviewer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

const githubCursorLineBackgroundANSI = "48;2;27;44;65m"

func newTestBV(width, height, totalLines int) *BufferedViewport {
	bv := NewBufferedViewport(width, height)
	bv.SetTotalLines(totalLines)
	return bv
}

func TestBufferedViewport_SetTotalLines(t *testing.T) {
	bv := NewBufferedViewport(80, 40)
	bv.SetTotalLines(1000)

	if bv.TotalLines() != 1000 {
		t.Errorf("TotalLines() = %d, want 1000", bv.TotalLines())
	}
	if bv.CursorLine() != 0 {
		t.Errorf("CursorLine() = %d, want 0", bv.CursorLine())
	}

	from, to := bv.GetRange()
	if from != 0 {
		t.Errorf("From = %d, want 0", from)
	}
	// To should be min(InitHiddenLines + visibleLineCount, 1000)
	if to == 0 {
		t.Error("To should be > 0 after SetTotalLines")
	}
}

// The gutter surface is transparent (OSC 11 background model): a normal line
// must not paint any background so it blends with the log body in every
// terminal. Only the cursor/visual selection chips carry a background.
func TestBufferedViewportGutterNormalLineIsTransparent(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	bv := newTestBV(80, 10, 10)
	gutter := bv.createGutterFunc(0, false, 0, 0)

	result := gutter(viewport.GutterContext{Index: 1, TotalLines: 10})

	if strings.Contains(result, "[48;") {
		t.Fatalf("normal gutter line paints a background; rely on OSC 11 instead: %q", result)
	}
}

func TestBufferedViewportGutterCursorLineKeepsHighlight(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	bv := newTestBV(80, 10, 10)
	gutter := bv.createGutterFunc(0, false, 0, 0)

	result := gutter(viewport.GutterContext{Index: 0, TotalLines: 10})

	if !strings.Contains(result, "48;2;88;166;255m") {
		t.Fatalf("cursor gutter line lost its selection highlight background: %q", result)
	}
}

func TestBufferedViewport_SetTotalLines_SmallFile(t *testing.T) {
	bv := NewBufferedViewport(80, 40)
	bv.SetTotalLines(10)

	_, to := bv.GetRange()
	if to != 10 {
		t.Errorf("To = %d, want 10 for small file", to)
	}
}

func TestBufferedViewport_CursorLine(t *testing.T) {
	bv := newTestBV(80, 40, 1000)

	if bv.CursorLine() != 0 {
		t.Errorf("initial CursorLine() = %d, want 0", bv.CursorLine())
	}

	bv.SetCursorLine(42)
	if bv.CursorLine() != 42 {
		t.Errorf("after set CursorLine() = %d, want 42", bv.CursorLine())
	}
}

func TestBufferedViewport_CursorInViewport(t *testing.T) {
	bv := newTestBV(80, 40, 1000)
	bv.SetCursorLine(10)
	// CursorInViewport = cursorLine - buffer.From = 10 - 0 = 10
	if got := bv.CursorInViewport(); got != 10 {
		t.Errorf("CursorInViewport() = %d, want 10", got)
	}
}

func TestBufferedViewport_IsRealFirstLine(t *testing.T) {
	bv := newTestBV(80, 40, 1000)

	if !bv.IsRealFirstLine() {
		t.Error("expected IsRealFirstLine() = true when cursorLine=0")
	}

	bv.SetCursorLine(1)
	if bv.IsRealFirstLine() {
		t.Error("expected IsRealFirstLine() = false when cursorLine=1")
	}
}

func TestBufferedViewport_IsRealLastLine(t *testing.T) {
	bv := newTestBV(80, 40, 1000)

	if bv.IsRealLastLine() {
		t.Error("expected IsRealLastLine() = false at start")
	}

	bv.SetCursorLine(999)
	if !bv.IsRealLastLine() {
		t.Error("expected IsRealLastLine() = true when cursorLine=999 and TotalLines=1000")
	}
}

func TestBufferedViewport_IsFirstChunk_IsLastChunk(t *testing.T) {
	bv := newTestBV(80, 40, 1000)

	if !bv.IsFirstChunk() {
		t.Error("expected IsFirstChunk() = true at start")
	}
	if bv.IsLastChunk() {
		t.Error("expected IsLastChunk() = false when buffer doesn't reach end")
	}

	// Small file where buffer covers everything
	bvSmall := newTestBV(80, 40, 10)
	if !bvSmall.IsFirstChunk() {
		t.Error("small file: expected IsFirstChunk() = true")
	}
	if !bvSmall.IsLastChunk() {
		t.Error("small file: expected IsLastChunk() = true")
	}
}

func TestBufferedViewport_ShouldLoadDownFor(t *testing.T) {
	bv := newTestBV(80, 40, 10000)

	_, to := bv.GetRange()
	// Far from buffer edge — no load needed
	if bv.ShouldLoadDownFor(0) {
		t.Error("should not need to load when far from buffer edge")
	}

	// Near buffer edge — load needed
	if !bv.ShouldLoadDownFor(to - 10) {
		t.Errorf("should need to load near buffer edge (to=%d, line=%d)", to, to-10)
	}
}

func TestBufferedViewport_ShouldLoadUpFor(t *testing.T) {
	bv := newTestBV(80, 40, 10000)

	// Buffer starts at 0, so no upward loading
	if bv.ShouldLoadUpFor(10) {
		t.Error("should not need to load up when buffer starts at 0")
	}
}

func TestBufferedViewport_CalculateNewBufferRangeDown(t *testing.T) {
	bv := newTestBV(80, 40, 10000)
	oldFrom, oldTo := bv.GetRange()

	bv.CalculateNewBufferRangeDown(5000)

	from, to := bv.GetRange()
	if from == oldFrom && to == oldTo {
		t.Error("buffer range should have changed after CalculateNewBufferRangeDown")
	}
	if bv.CursorLine() != 5000 {
		t.Errorf("CursorLine() = %d, want 5000", bv.CursorLine())
	}
}

func TestBufferedViewport_CalculateNewBufferRangeUp(t *testing.T) {
	bv := newTestBV(80, 40, 10000)
	// First move buffer down
	bv.CalculateNewBufferRangeDown(5000)

	bv.CalculateNewBufferRangeUp(100)

	from, _ := bv.GetRange()
	if from > 100 {
		t.Errorf("From = %d, should be <= 100 after calculating range up to 100", from)
	}
	if bv.CursorLine() != 100 {
		t.Errorf("CursorLine() = %d, want 100", bv.CursorLine())
	}
}

func TestBufferedViewport_ScrollDown(t *testing.T) {
	bv := newTestBV(80, 40, 1000)
	// Load some content so viewport has lines
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = "test line"
	}
	bv.SetContentLines(lines)

	initial := bv.CursorLine()
	bv.ScrollDown(1)

	if bv.CursorLine() != initial+1 {
		t.Errorf("CursorLine() = %d, want %d after ScrollDown", bv.CursorLine(), initial+1)
	}
}

func TestBufferedViewport_ScrollUp(t *testing.T) {
	bv := newTestBV(80, 40, 1000)
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = "test line"
	}
	bv.SetContentLines(lines)

	// Move down first so we can scroll up
	bv.ScrollDown(1)
	bv.ScrollDown(1)
	pos := bv.CursorLine()
	bv.ScrollUp(1)

	if bv.CursorLine() != pos-1 {
		t.Errorf("CursorLine() = %d, want %d after ScrollUp", bv.CursorLine(), pos-1)
	}
}

func TestBufferedViewport_FirstVisibleLine_LastVisibleLine(t *testing.T) {
	bv := newTestBV(80, 40, 1000)
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = "test line"
	}
	bv.SetContentLines(lines)

	first := bv.FirstVisibleLine()
	last := bv.LastVisibleLine()

	if first < 0 {
		t.Errorf("FirstVisibleLine() = %d, should be >= 0", first)
	}
	if last <= first {
		t.Errorf("LastVisibleLine() = %d should be > FirstVisibleLine() = %d", last, first)
	}
}

func TestBufferedViewport_IsVisible(t *testing.T) {
	bv := newTestBV(80, 40, 1000)
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = "test line"
	}
	bv.SetContentLines(lines)

	first := bv.FirstVisibleLine()
	last := bv.LastVisibleLine()

	// Line within visible range should be visible
	mid := (first + last) / 2
	if !bv.IsVisible(mid) {
		t.Errorf("line %d should be visible (first=%d, last=%d)", mid, first, last)
	}

	// Line way outside should not be visible
	if bv.IsVisible(999) {
		t.Error("line 999 should not be visible in initial viewport")
	}
}

func TestBufferedViewport_AdjustForScrollDownTo(t *testing.T) {
	bv := newTestBV(80, 40, 1000)
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = "test line"
	}
	bv.SetContentLines(lines)

	targetLine := 100
	bv.AdjustForScrollDownTo(targetLine)

	// Cursor should be near the target
	if bv.CursorLine() != targetLine-1 {
		t.Errorf("CursorLine() = %d, want %d", bv.CursorLine(), targetLine-1)
	}
}

func TestBufferedViewport_AdjustForScrollUpTo(t *testing.T) {
	bv := newTestBV(80, 40, 1000)
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = "test line"
	}
	bv.SetContentLines(lines)

	// First scroll down, then adjust up
	bv.AdjustForScrollDownTo(200)
	bv.AdjustForScrollUpTo(50)

	if bv.CursorLine() != 49 {
		t.Errorf("CursorLine() = %d, want 49", bv.CursorLine())
	}
}

func TestBufferedViewport_VisibleLineCount(t *testing.T) {
	bv := newTestBV(80, 40, 1000)

	lines := make([]string, 300)
	for i := range lines {
		lines[i] = "test line"
	}
	bv.SetContentLines(lines)

	count := bv.VisibleLineCount()
	if count <= 0 {
		t.Errorf("VisibleLineCount() = %d, should be > 0", count)
	}
}

func TestBufferedViewport_YOffset(t *testing.T) {
	bv := newTestBV(80, 40, 1000)

	// Initial offset should be 0
	if bv.YOffset() != 0 {
		t.Errorf("initial YOffset() = %d, want 0", bv.YOffset())
	}
}

func TestBufferedViewport_ToggleWrap(t *testing.T) {
	bv := newTestBV(80, 40, 1000)

	if bv.WrapEnabled() {
		t.Error("expected WrapEnabled() = false initially")
	}

	bv.ToggleWrap()
	if !bv.WrapEnabled() {
		t.Error("expected WrapEnabled() = true after first ToggleWrap")
	}

	bv.ToggleWrap()
	if bv.WrapEnabled() {
		t.Error("expected WrapEnabled() = false after second ToggleWrap")
	}
}

// The cursor line's own (non-wrapped) row and its wrapped continuation rows
// must render a visually unbroken highlight bar. Before this fix, the "|"
// bar on the cursor's own row used the plain (unhighlighted) Separator style
// while every continuation row used CurrentSeparator, leaving a gap in the
// background color at the top of the highlighted block once wrap made that
// block span multiple rows. The trailing marker glyph deliberately keeps no
// background of its own — it's a Nerd Font chevron meant to read as a
// pointer, not a filled badge — so only the "|" segment is checked here.
func TestBufferedViewportGutterCursorLineSeparatorMatchesSoftWrapContinuation(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	bv := newTestBV(80, 10, 10)
	gutter := bv.createGutterFunc(0, false, 0, 0)

	hardRow := gutter(viewport.GutterContext{Index: 0, TotalLines: 10, Soft: false})

	// Lipgloss emits one ANSI-styled run per Render call; isolate the run
	// containing "|" so the background check can't pass on the line
	// number's own highlight instead of the separator bar's.
	segments := strings.Split(hardRow, "\x1b[m")
	barSegment := ""
	for _, seg := range segments {
		if strings.Contains(seg, "|") {
			barSegment = seg
			break
		}
	}

	if barSegment == "" {
		t.Fatalf("expected a rendered segment containing the separator bar \"|\", got row %q", hardRow)
	}
	if !strings.Contains(barSegment, "48;2;88;166;255") {
		t.Errorf("cursor line's own separator bar should carry the current-line highlight background like its wrapped continuation rows, got segment %q in full row %q", barSegment, hardRow)
	}
}

func TestBufferedViewportGutterCursorMarkerUsesThemeForeground(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	bv := newTestBV(80, 10, 10)
	gutter := bv.createGutterFunc(0, false, 0, 0)

	hardRow := gutter(viewport.GutterContext{Index: 0, TotalLines: 10, Soft: false})
	markerSegment := ""
	for _, segment := range strings.Split(hardRow, "\x1b[m") {
		if strings.Contains(segment, "›") {
			markerSegment = segment
			break
		}
	}

	if markerSegment == "" {
		t.Fatalf("expected a rendered cursor marker, got row %q", hardRow)
	}
	if !strings.Contains(markerSegment, "38;2;230;237;243") {
		t.Errorf("cursor marker should use the theme foreground, got segment %q in full row %q", markerSegment, hardRow)
	}
	if strings.Contains(markerSegment, "48;") {
		t.Errorf("cursor marker should remain background-free, got segment %q in full row %q", markerSegment, hardRow)
	}
}

func TestBufferedViewport_CurrentLineHighlightsFullContentWidth(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	bv := newTestBV(40, 10, 3)
	bv.SetContentLines([]string{"first", "second", "third"})

	lines := strings.Split(bv.viewport.GetContent(), "\n")
	if !strings.Contains(lines[0], githubCursorLineBackgroundANSI) {
		t.Fatalf("current line should carry the cursor-line background, got %q", lines[0])
	}
	if strings.Contains(lines[1], githubCursorLineBackgroundANSI) {
		t.Fatalf("non-current line should stay transparent, got %q", lines[1])
	}

	gutterWidth := lipgloss.Width(bv.viewport.LeftGutterFunc(viewport.GutterContext{}))
	wantWidth := bv.viewport.Width() - gutterWidth
	if got := ansi.StringWidth(lines[0]); got != wantWidth {
		t.Fatalf("current line width = %d, want viewport content width %d", got, wantWidth)
	}

	bv.ScrollDown(1)
	lines = strings.Split(bv.viewport.GetContent(), "\n")
	if strings.Contains(lines[0], githubCursorLineBackgroundANSI) {
		t.Fatalf("previous current line kept the cursor-line background, got %q", lines[0])
	}
	if !strings.Contains(lines[1], githubCursorLineBackgroundANSI) {
		t.Fatalf("new current line should carry the cursor-line background, got %q", lines[1])
	}
}

func TestBufferedViewport_CurrentLineKeepsSearchHighlight(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	theme := styles.GetTheme()
	raw := HighlightMatches(DefaultLogStyle("prefix MATCH suffix"), "MATCH", theme.S().TextMatchStyle)

	bv := newTestBV(40, 10, 1)
	bv.SetContentLines([]string{raw})

	got := bv.viewport.GetContent()
	if count := strings.Count(got, githubCursorLineBackgroundANSI); count < 2 {
		t.Fatalf("cursor-line background should resume after embedded style resets, found %d spans in %q", count, got)
	}
	if !strings.Contains(got, "48;2;210;153;34m") {
		t.Fatalf("current line should retain the search-match background, got %q", got)
	}
	if !strings.HasPrefix(ansi.Strip(got), "prefix MATCH suffix") {
		t.Fatalf("current-line styling changed the log text, got %q", ansi.Strip(got))
	}
}

func TestBufferedViewport_CurrentSearchMatchIsDarkerThanOtherMatches(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	theme := styles.GetTheme()
	raw := HighlightMatches(DefaultLogStyle("MATCH then MATCH"), "MATCH", theme.S().TextMatchStyle)

	bv := newTestBV(40, 10, 1)
	bv.SetContentLines([]string{raw})
	bv.setCurrentSearchMatch(MatchPosition{Line: 1, Col: 11}, len("MATCH"))
	bv.updateCursorHighlight()

	got := bv.viewport.GetContent()
	firstMatch := strings.Index(got, "MATCH")
	if firstMatch < 0 {
		t.Fatalf("first match missing from rendered content: %q", got)
	}
	secondMatch := strings.Index(got[firstMatch+len("MATCH"):], "MATCH")
	if secondMatch < 0 {
		t.Fatalf("second match missing from rendered content: %q", got)
	}
	secondMatch += firstMatch + len("MATCH")
	if light := strings.LastIndex(got[:firstMatch], "48;2;210;153;34"); light < 0 {
		t.Fatalf("non-current match should keep the light highlight, got %q", got)
	}
	if dark := strings.LastIndex(got[:secondMatch], "48;2;102;78;28"); dark < 0 {
		t.Fatalf("current match should use the dark highlight, got %q", got)
	}
	if !strings.HasPrefix(ansi.Strip(got), "MATCH then MATCH") {
		t.Fatalf("current-match styling changed the log text, got %q", ansi.Strip(got))
	}
}

func TestBufferedViewport_SetVisualMode(t *testing.T) {
	bv := newTestBV(80, 40, 1000)

	if bv.VisualMode() {
		t.Fatal("expected VisualMode() = false before SetVisualMode")
	}

	bv.SetCursorLine(10)
	bv.SetVisualMode(true, 10)
	if !bv.VisualMode() {
		t.Error("expected VisualMode() = true after SetVisualMode(true, ...)")
	}

	bv.SetVisualMode(false, 0)
	if bv.VisualMode() {
		t.Error("expected VisualMode() = false after SetVisualMode(false, ...)")
	}
}

func TestBufferedViewport_SelectionRange(t *testing.T) {
	tests := []struct {
		name       string
		anchorLine int
		cursorLine int
		wantFrom   int
		wantTo     int
	}{
		{
			name:       "cursor below anchor",
			anchorLine: 5,
			cursorLine: 10,
			wantFrom:   5,
			wantTo:     11,
		},
		{
			name:       "cursor above anchor",
			anchorLine: 10,
			cursorLine: 5,
			wantFrom:   5,
			wantTo:     11,
		},
		{
			name:       "single line, anchor equals cursor",
			anchorLine: 7,
			cursorLine: 7,
			wantFrom:   7,
			wantTo:     8,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			bv := newTestBV(80, 40, 1000)
			bv.SetCursorLine(tt.cursorLine)
			bv.SetVisualMode(true, tt.anchorLine)

			from, to := bv.SelectionRange()
			if from != tt.wantFrom {
				t.Errorf("from = %d, want %d", from, tt.wantFrom)
			}
			if to != tt.wantTo {
				t.Errorf("to = %d, want %d", to, tt.wantTo)
			}
		})
	}
}

// SelectionRange must stay correct even after the sliding buffer window
// reloads and evicts the lines around the anchor — yank reads from disk by
// absolute line number, not from whatever the viewport currently holds.
func TestBufferedViewport_SelectionRange_SurvivesBufferReload(t *testing.T) {
	bv := newTestBV(80, 40, 10000)
	bv.SetCursorLine(100)
	bv.SetVisualMode(true, 100)

	bv.CalculateNewBufferRangeDown(5000)

	from, to := bv.SelectionRange()
	if from != 100 || to != 5001 {
		t.Errorf("SelectionRange() = (%d, %d), want (100, 5001) after buffer reload", from, to)
	}
}

// Visual-mode selection reuses the same accessible accent/background pair as
// the cursor-line highlight (GutterVisualBg == GutterCurrentBg for every
// palette). Rows inside [selStart, selEnd] get the selection foreground; rows
// outside get the plain line-number style.
func TestBufferedViewportGutterVisualSelectionHighlightsRange(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	bv := newTestBV(80, 10, 20)
	gutter := bv.createGutterFunc(2, true, 1, 3)

	inRange := gutter(viewport.GutterContext{Index: 2, TotalLines: 20})
	if !strings.Contains(inRange, "48;2;88;166;255") {
		t.Fatalf("row inside selection should carry the selection background, got %q", inRange)
	}
	if !strings.Contains(inRange, "38;2;13;17;23") {
		t.Fatalf("row inside selection should use the contrasting canvas foreground, got %q", inRange)
	}

	outOfRange := gutter(viewport.GutterContext{Index: 5, TotalLines: 20})
	if strings.Contains(outOfRange, "48;2;88;166;255") {
		t.Fatalf("row outside selection should not carry the selection background, got %q", outOfRange)
	}
}

func TestBufferedViewport_SetVisualMode_UpdatesGutterHighlight(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	bv := newTestBV(80, 10, 20)
	lines := make([]string, 20)
	for i := range lines {
		lines[i] = "test line"
	}
	bv.SetContentLines(lines)
	bv.SetCursorLine(5)
	bv.SetVisualMode(true, 3)

	view := bv.View()
	if !strings.Contains(view, "48;2;88;166;255") {
		t.Fatalf("expected view to contain the visual-selection gutter background after SetVisualMode, got %q", view)
	}
}

// A visual-selected hard row's width (line number + separator) must equal
// that of a normal hard row. Before the fix, the selected row was missing
// the separator, rendering 2 columns narrower, creating ragged left edges
// across multi-line selections.
func TestBufferedViewportGutterVisualSelectionMatchesNormalRowWidth(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	bv := newTestBV(80, 10, 20)
	gutter := bv.createGutterFunc(2, true, 1, 3)

	selectedRow := gutter(viewport.GutterContext{Index: 2, TotalLines: 20})
	normalRow := gutter(viewport.GutterContext{Index: 10, TotalLines: 20})

	selectedWidth := lipgloss.Width(selectedRow)
	normalWidth := lipgloss.Width(normalRow)

	if selectedWidth != normalWidth {
		t.Fatalf("selected row width = %d, normal row width = %d, want equal (selected: %q, normal: %q)",
			selectedWidth, normalWidth, selectedRow, normalRow)
	}
	if !strings.Contains(selectedRow, "│") {
		t.Errorf("selected row should carry the gutter separator bar, got %q", selectedRow)
	}
}

// Visual selection must highlight the log line text itself, not just the
// gutter number column: rows inside the selection get a background painted
// across their content, rows outside stay exactly as loaded.
func TestBufferedViewport_LineHighlight_SelectedRowsGetBackground(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	bv := newTestBV(80, 10, 4)
	lines := make([]string, 4)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i)
	}
	bv.SetContentLines(lines)
	bv.SetCursorLine(2)
	bv.SetVisualMode(true, 1)

	content := bv.viewport.GetContent()
	got := strings.Split(content, "\n")
	if len(got) != 4 {
		t.Fatalf("expected 4 content lines, got %d: %q", len(got), got)
	}

	if got[0] != "line0" {
		t.Errorf("row 0 (outside selection) should be untouched, got %q", got[0])
	}
	if got[3] != "line3" {
		t.Errorf("row 3 (outside selection) should be untouched, got %q", got[3])
	}

	for _, i := range []int{1, 2} {
		if !strings.Contains(got[i], githubCursorLineBackgroundANSI) {
			t.Errorf("row %d (inside selection) should match the cursor-line background, got %q", i, got[i])
		}
		if !strings.HasPrefix(ansi.Strip(got[i]), fmt.Sprintf("line%d", i)) {
			t.Errorf("row %d text content changed, got %q", i, ansi.Strip(got[i]))
		}
		if width := ansi.StringWidth(got[i]); width != bv.cursorLineRenderWidth() {
			t.Errorf("row %d width = %d, want %d", i, width, bv.cursorLineRenderWidth())
		}
	}
}

// Log lines already carry embedded ANSI (log-level color, search-match
// highlight) before selection ever touches them. Visual selection keeps those
// styles while reasserting its row background after embedded resets.
func TestBufferedViewport_LineHighlight_SurvivesEmbeddedANSI(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	match := lipgloss.NewStyle().Foreground(lipgloss.Color("#ff0000")).Render("MATCH")
	raw := "prefix " + match + " suffix"

	bv := newTestBV(80, 10, 1)
	bv.SetContentLines([]string{raw})
	bv.SetCursorLine(0)
	bv.SetVisualMode(true, 0)

	got := bv.viewport.GetContent()

	if !strings.HasPrefix(ansi.Strip(got), "prefix MATCH suffix") {
		t.Fatalf("selected line text corrupted: got plain text %q", ansi.Strip(got))
	}
	if n := strings.Count(got, githubCursorLineBackgroundANSI); n < 2 {
		t.Fatalf("visual background should resume after embedded resets, found %d spans in %q", n, got)
	}
	if !strings.Contains(got, "38;2;255;0;0") {
		t.Fatalf("visual line should retain its embedded foreground, got %q", got)
	}
}

// Exiting visual mode must remove the visual-selection overlay and restore the
// normal cursor-line highlight without leaving that background on other rows.
func TestBufferedViewport_LineHighlight_RevertsOnExitVisualMode(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	bv := newTestBV(80, 10, 2)
	bv.SetContentLines([]string{"line0", "line1"})
	bv.SetCursorLine(1)
	bv.SetVisualMode(true, 1)

	bv.SetVisualMode(false, 0)

	got := strings.Split(bv.viewport.GetContent(), "\n")
	if got[0] != "line0" {
		t.Fatalf("non-current row should revert to raw content, got %q", got[0])
	}
	if !strings.Contains(got[1], githubCursorLineBackgroundANSI) {
		t.Fatalf("current row should restore its cursor-line background, got %q", got[1])
	}
	if strings.Contains(got[1], "48;2;88;166;255") {
		t.Fatalf("current row retained the visual-selection background, got %q", got[1])
	}
}

// As the cursor moves while selecting, the highlighted range must track it:
// previously-selected rows that fall out of range revert, newly-included
// rows get painted.
func TestBufferedViewport_LineHighlight_TracksSelectionMovement(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	bv := newTestBV(80, 10, 5)
	lines := make([]string, 5)
	for i := range lines {
		lines[i] = fmt.Sprintf("line%d", i)
	}
	bv.SetContentLines(lines)
	bv.SetCursorLine(2)
	bv.SetVisualMode(true, 2)

	bv.ScrollDown(0) // cursor 2 -> 3, anchor stays at 2

	got := strings.Split(bv.viewport.GetContent(), "\n")
	for _, i := range []int{2, 3} {
		if !strings.Contains(got[i], githubCursorLineBackgroundANSI) {
			t.Errorf("row %d should be selected after cursor moved to 3, got %q", i, got[i])
		}
	}
	for _, i := range []int{0, 1, 4} {
		if strings.Contains(got[i], githubCursorLineBackgroundANSI) {
			t.Errorf("row %d should not be selected, got %q", i, got[i])
		}
	}
}
