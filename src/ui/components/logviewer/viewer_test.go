package logviewer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

func testContext() *context.ProgramContext {
	cfg := config.GetDefaultConfig()
	return &context.ProgramContext{
		Config: cfg,
	}
}

func testModelWithFile(t *testing.T, lineCount int) (*Model, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	var sb strings.Builder
	for i := range lineCount {
		fmt.Fprintf(&sb, "[INFO] line %d: log output\n", i+1)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewModel(testContext(), 80, 40, path)
	return m, path
}

// makeKeyPress creates a tea.KeyPressMsg that key.Matches can match against.
func makeKeyPress(keyStr string) tea.KeyPressMsg {
	switch keyStr {
	case "ctrl+k":
		return tea.KeyPressMsg{Code: 'k', Mod: tea.ModCtrl}
	case "ctrl+j":
		return tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	default:
		// Single character key
		if len(keyStr) == 1 {
			return tea.KeyPressMsg{Code: rune(keyStr[0]), Text: keyStr}
		}
		return tea.KeyPressMsg{}
	}
}

func TestNewModel(t *testing.T) {
	m, _ := testModelWithFile(t, 100)

	if m.activeView != LogView {
		t.Errorf("activeView = %d, want LogView(%d)", m.activeView, LogView)
	}
	if !m.loading {
		t.Error("expected loading = true on new model")
	}
	if m.width != 80 {
		t.Errorf("width = %d, want 80", m.width)
	}
	if m.height != 40 {
		t.Errorf("height = %d, want 40", m.height)
	}
	if m.searchState != nil {
		t.Error("expected searchState = nil on new model")
	}
}

func TestInitStartsLogViewerSpinner(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	if cmd := m.Init(); cmd == nil {
		t.Fatal("Init() returned no loading commands")
	}
	if !m.spinning {
		t.Fatal("Init() did not start the loading spinner")
	}
}

func TestSearchCompletionStopsLogViewerSpinner(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.loading = true
	m.spinning = true
	m.searchInput.SetValue("missing")

	if cmd := m.handleSearchCompleted(SearchCompletedMsg{}); cmd == nil {
		t.Fatal("search completion did not report the empty result")
	}
	if m.spinning || m.loading {
		t.Fatal("search completion left the spinner or loading state active")
	}
	if cmd := m.handleSpinnerTick(m.spinner.Tick().(spinner.TickMsg)); cmd != nil {
		t.Fatal("completed search kept scheduling spinner ticks")
	}
}

func TestCleanUpRejectsStaleViewerResults(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.session = 1
	m.chunkGeneration = 1
	m.loading = true
	request := &chunkLoadRequest{generation: 1, session: 1}

	m.CleanUp()
	m.handleFileReady(FileReadyMsg{LineCount: 10, generation: 1})
	m.handleChunkLoaded(ChunkLoadedMsg{Lines: []string{"stale"}, request: request})

	if m.bufferedViewport.TotalLines() != 0 {
		t.Fatal("stale file-ready message changed the re-entered viewer")
	}
	if !m.loading {
		t.Fatal("stale chunk result changed the re-entered loading state")
	}
}

func TestCalcScrollPercentage(t *testing.T) {
	tests := []struct {
		name       string
		totalLines int
		cursorLine int
		wantPct    float64
	}{
		{
			name:       "zero total lines",
			totalLines: 0,
			cursorLine: 0,
			wantPct:    0,
		},
		{
			name:       "at start",
			totalLines: 1000,
			cursorLine: 0,
			wantPct:    0.1, // (0+1)/1000 * 100
		},
		{
			name:       "at end",
			totalLines: 1000,
			cursorLine: 999,
			wantPct:    100.0,
		},
		{
			name:       "at middle",
			totalLines: 100,
			cursorLine: 49,
			wantPct:    50.0, // (49+1)/100 * 100
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m, _ := testModelWithFile(t, max(tt.totalLines, 1))

			if tt.totalLines > 0 {
				m.bufferedViewport.SetTotalLines(tt.totalLines)
			}
			m.bufferedViewport.SetCursorLine(tt.cursorLine)

			got := m.calcScrollPercentage()
			if got != tt.wantPct {
				t.Errorf("calcScrollPercentage() = %f, want %f", got, tt.wantPct)
			}
		})
	}
}

func TestUpdate_FileReadyMsg(t *testing.T) {
	m, _ := testModelWithFile(t, 100)

	updated, cmd := m.Update(FileReadyMsg{
		FilePath:  m.filePath,
		LineCount: 100,
	})

	if updated.bufferedViewport.TotalLines() != 100 {
		t.Errorf("TotalLines = %d, want 100", updated.bufferedViewport.TotalLines())
	}
	if cmd == nil {
		t.Error("expected cmd from FileReadyMsg (loadChunk)")
	}
}

func TestUpdate_ChunkLoadedMsg_None(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.loading = true

	lines := make([]string, 50)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}

	updated, _ := m.Update(ChunkLoadedMsg{Lines: lines, ScrollType: None})

	if updated.loading {
		t.Error("expected loading = false after ChunkLoadedMsg with None scroll")
	}
}

func TestUpdate_ChunkLoadedMsg_ScrollUp(t *testing.T) {
	m, _ := testModelWithFile(t, 100)
	m.bufferedViewport.SetTotalLines(100)
	m.loading = true

	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}

	updated, _ := m.Update(ChunkLoadedMsg{Lines: lines, ScrollType: Up})

	if updated.loading {
		t.Error("expected loading = false after ChunkLoadedMsg with Up scroll")
	}
}

func TestUpdate_ChunkLoadedMsg_ScrollDown(t *testing.T) {
	m, _ := testModelWithFile(t, 100)
	m.bufferedViewport.SetTotalLines(100)
	m.loading = true

	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}

	updated, _ := m.Update(ChunkLoadedMsg{Lines: lines, ScrollType: Down})

	if updated.loading {
		t.Error("expected loading = false after ChunkLoadedMsg with Down scroll")
	}
}

func TestUpdate_SearchCompletedMsg_NoMatches(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.searchInput.SetValue("nonexistent")

	updated, cmd := m.Update(SearchCompletedMsg{Matches: linesOf()})

	if updated.searchState == nil {
		t.Fatal("expected searchState to be set")
	}
	if updated.searchState.TotalMatches() != 0 {
		t.Error("expected 0 matches")
	}
	if cmd == nil {
		t.Error("expected footer notification cmd")
	}
}

func TestUpdate_SearchCompletedMsg_WithMatches(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.searchInput.SetValue("test")

	updated, cmd := m.Update(SearchCompletedMsg{Matches: linesOf(5, 10, 20)})

	if updated.searchState == nil {
		t.Fatal("expected searchState to be set")
	}
	if updated.searchState.TotalMatches() != 3 {
		t.Errorf("TotalMatches = %d, want 3", updated.searchState.TotalMatches())
	}
	if updated.activeView != LogView {
		t.Error("expected activeView = LogView after search with matches")
	}
	if cmd == nil {
		t.Error("expected command to load the first match")
	}
}

func TestUpdate_SearchCompletionLoadsFirstMatchBeforeAcceptingNavigation(t *testing.T) {
	m := loadedModel(t, 1000)
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	m.searchInput.SetValue("line")

	_, cmd := m.Update(SearchCompletedMsg{Matches: linesOf(500, 700)})
	if cmd == nil {
		t.Fatal("expected search completion to load the first match's chunk")
	}
	m.Update(makeKeyPress("n"))

	pending := ansi.Strip(m.View())
	if !strings.Contains(pending, "Matches: 1/2") {
		t.Fatalf("search navigation advanced before the first match loaded:\n%s", pending)
	}

	drain(t, m, cmd)
	loaded := ansi.Strip(m.View())
	if !strings.Contains(loaded, "Matches: 1/2") || !strings.Contains(loaded, "Line: 500") {
		t.Fatalf("search did not settle on its first match:\n%s", loaded)
	}
}

func TestUpdate_WindowSizeMsg(t *testing.T) {
	m, _ := testModelWithFile(t, 50)

	updated, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 60})

	if updated.width != 120 {
		t.Errorf("width = %d, want 120", updated.width)
	}
	if updated.height != 60 {
		t.Errorf("height = %d, want 60", updated.height)
	}
}

func TestHandleKeyMsg_Search(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = LogView

	m.handleKeyMsg(makeKeyPress("/"))

	if m.activeView != SearchView {
		t.Error("expected activeView = SearchView after '/' key")
	}
}

func TestIsCapturingInput(t *testing.T) {
	m, _ := testModelWithFile(t, 50)

	m.activeView = LogView
	if m.IsCapturingInput() {
		t.Error("log view should not capture input")
	}

	m.activeView = SearchView
	if !m.IsCapturingInput() {
		t.Error("search view should capture input")
	}
}

func TestSearchViewTypesLettersInsteadOfNavigating(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = LogView
	m.handleKeyMsg(makeKeyPress("/")) // open + focus the search box

	// "n" is the Next-match binding in log view; while the search box is focused it
	// must type into the query, not navigate.
	m.handleKeyMsg(makeKeyPress("n"))
	// "/" is the search-open key; while already in search view it must type too,
	// not reset the query.
	m.handleKeyMsg(makeKeyPress("/"))

	if got := m.searchInput.Value(); got != "n/" {
		t.Fatalf("search query = %q, want %q (letters/'/' should type, not trigger actions)", got, "n/")
	}
	if m.activeView != SearchView {
		t.Error("typing in the search box should not leave search view")
	}
}

func TestHandleKeyMsg_ComponentUp(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = LogView

	m.handleKeyMsg(makeKeyPress("ctrl+k"))

	if m.activeView != SearchView {
		t.Error("expected activeView = SearchView after ComponentUp")
	}
}

func TestHandleKeyMsg_ComponentDown(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = SearchView

	m.handleKeyMsg(makeKeyPress("ctrl+j"))

	if m.activeView != LogView {
		t.Error("expected activeView = LogView after ComponentDown")
	}
}

func TestHandleLogViewKeys_Loading(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = LogView
	m.loading = true

	cmd := m.handleLogViewKeys(makeKeyPress("j"))
	if cmd != nil {
		t.Error("expected nil cmd when loading is true")
	}
}

func TestHandleLogViewKeys_NextPrevMatch(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = LogView
	m.loading = false
	m.searchState = NewSearchState(linesOf(5, 10, 15))
	m.bufferedViewport.SetTotalLines(50)

	m.handleLogViewKeys(makeKeyPress("n"))
	if got := m.bufferedViewport.CursorLine(); got != 9 {
		t.Errorf("CursorLine = %d, want 9 after next match", got)
	}

	m.handleLogViewKeys(makeKeyPress("p"))
	if got := m.bufferedViewport.CursorLine(); got != 4 {
		t.Errorf("CursorLine = %d, want 4 after previous match", got)
	}
}

func TestHandleLogViewKeys_NextWithoutSearchState(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = LogView
	m.loading = false
	m.searchState = nil

	cmd := m.handleLogViewKeys(makeKeyPress("n"))
	if cmd != nil {
		t.Error("expected nil cmd for 'n' without search state")
	}
}

func TestHandleLogViewKeys_ScrollDown(t *testing.T) {
	m, _ := testModelWithFile(t, 100)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(100)

	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	m.bufferedViewport.SetContentLines(lines)

	initial := m.bufferedViewport.CursorLine()
	m.handleLogViewKeys(makeKeyPress("j"))

	if m.bufferedViewport.CursorLine() != initial+1 {
		t.Errorf("CursorLine = %d, want %d after scroll down", m.bufferedViewport.CursorLine(), initial+1)
	}
}

func TestModel_PageDown_AdvancesCursorByViewportHeight(t *testing.T) {
	m, _ := testModelWithFile(t, 500)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(500)

	lines := make([]string, 500)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	m.bufferedViewport.SetContentLines(lines)

	initial := m.bufferedViewport.CursorLine()
	visible := m.bufferedViewport.VisibleLineCount()

	m.pageDown()

	want := initial + visible
	if got := m.bufferedViewport.CursorLine(); got != want {
		t.Errorf("CursorLine = %d, want %d after pageDown", got, want)
	}
}

func TestModel_PageDown_ClampsAtLastLine(t *testing.T) {
	m, _ := testModelWithFile(t, 20)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(20)

	lines := make([]string, 20)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	m.bufferedViewport.SetContentLines(lines)

	// A viewport this tall relative to 20 lines means a single page down
	// would overshoot the end of the file if not clamped.
	m.pageDown()

	if got, want := m.bufferedViewport.CursorLine(), 19; got != want {
		t.Errorf("CursorLine = %d, want %d (clamped to last line)", got, want)
	}
}

func TestModel_PageDown_NoopAtLastLine(t *testing.T) {
	m, _ := testModelWithFile(t, 20)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(20)
	m.bufferedViewport.SetCursorLine(19)

	if cmd := m.pageDown(); cmd != nil {
		t.Error("expected nil cmd when already on the last line")
	}
	if got := m.bufferedViewport.CursorLine(); got != 19 {
		t.Errorf("CursorLine = %d, want 19 unchanged", got)
	}
}

func TestModel_PageUp_RetreatsCursorByViewportHeight(t *testing.T) {
	m, _ := testModelWithFile(t, 500)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(500)

	lines := make([]string, 500)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	m.bufferedViewport.SetContentLines(lines)

	visible := m.bufferedViewport.VisibleLineCount()
	m.bufferedViewport.SetCursorLine(400)

	m.pageUp()

	if got, want := m.bufferedViewport.CursorLine(), 400-visible; got != want {
		t.Errorf("CursorLine = %d, want %d after pageUp", got, want)
	}
}

func TestModel_PageUp_ClampsAtFirstLine(t *testing.T) {
	m, _ := testModelWithFile(t, 500)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(500)

	lines := make([]string, 500)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	m.bufferedViewport.SetContentLines(lines)
	m.bufferedViewport.SetCursorLine(5)

	m.pageUp()

	if got := m.bufferedViewport.CursorLine(); got != 0 {
		t.Errorf("CursorLine = %d, want 0 (clamped to first line)", got)
	}
}

func TestModel_PageUp_NoopAtFirstLine(t *testing.T) {
	m, _ := testModelWithFile(t, 20)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(20)

	if cmd := m.pageUp(); cmd != nil {
		t.Error("expected nil cmd when already on the first line")
	}
	if got := m.bufferedViewport.CursorLine(); got != 0 {
		t.Errorf("CursorLine = %d, want 0 unchanged", got)
	}
}

func TestHandleLogViewKeys_PageDownPageUp(t *testing.T) {
	m, _ := testModelWithFile(t, 500)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(500)

	lines := make([]string, 500)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	m.bufferedViewport.SetContentLines(lines)

	// Configured defaults: PageDown = "{", PageUp = "}". Whether a chunk
	// reload cmd fires depends on how much of the file the buffer window
	// already covers — what matters here is that the cursor actually moves.
	m.handleLogViewKeys(makeKeyPress("{"))
	afterDown := m.bufferedViewport.CursorLine()
	if afterDown == 0 {
		t.Error("expected cursor to advance after PageDown key")
	}

	m.handleLogViewKeys(makeKeyPress("}"))
	if got := m.bufferedViewport.CursorLine(); got >= afterDown {
		t.Errorf("CursorLine = %d, want less than %d after PageUp key", got, afterDown)
	}
}

func TestHandleLogViewKeys_HorizontalScroll(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetContentLines([]string{strings.Repeat("x", 200)})

	m.handleLogViewKeys(makeKeyPress("l"))
	if got := m.bufferedViewport.XOffset(); got != horizontalScrollStep {
		t.Fatalf("XOffset = %d, want %d after scrolling right", got, horizontalScrollStep)
	}

	m.handleLogViewKeys(makeKeyPress("h"))
	if got := m.bufferedViewport.XOffset(); got != 0 {
		t.Errorf("XOffset = %d, want 0 after scrolling left", got)
	}
}

func TestHandleLogViewKeys_ToggleWrap(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.activeView = LogView
	m.loading = false

	if m.bufferedViewport.WrapEnabled() {
		t.Fatal("expected wrap to start disabled")
	}

	m.handleLogViewKeys(makeKeyPress("w"))
	if !m.bufferedViewport.WrapEnabled() {
		t.Error("expected wrap enabled after pressing w")
	}

	m.handleLogViewKeys(makeKeyPress("w"))
	if m.bufferedViewport.WrapEnabled() {
		t.Error("expected wrap disabled after pressing w again")
	}
}

func TestHandleLogViewKeys_ScrollLeftStopsAtStart(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetContentLines([]string{strings.Repeat("x", 200)})

	m.handleLogViewKeys(makeKeyPress("h"))
	if got := m.bufferedViewport.XOffset(); got != 0 {
		t.Errorf("XOffset = %d, want 0", got)
	}
}

func TestHandleLogViewKeys_ScrollUp_AtTop(t *testing.T) {
	m, _ := testModelWithFile(t, 100)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(100)

	cmd := m.handleLogViewKeys(makeKeyPress("k"))
	if cmd != nil {
		t.Error("expected nil cmd when scrolling up at first line")
	}
}

func TestHandleLogViewKeys_ScrollDown_AtBottom(t *testing.T) {
	m, _ := testModelWithFile(t, 100)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(100)
	m.bufferedViewport.SetCursorLine(99) // last line

	cmd := m.handleLogViewKeys(makeKeyPress("j"))
	if cmd != nil {
		t.Error("expected nil cmd when scrolling down at last line")
	}
}

func TestHandleSearchViewKeys_Enter(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = SearchView
	m.searchInput.SetValue("test")

	cmd := m.handleSearchViewKeys(makeKeyPress("enter"))
	if cmd == nil {
		t.Error("expected cmd from search enter")
	}
	if !m.loading {
		t.Error("expected loading = true after search submit")
	}
}

func TestKeyMap(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	km := m.KeyMap()
	if km == nil {
		t.Error("KeyMap() should not be nil")
	}
}

func TestRenderStats_Loading(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.loading = true

	stats := m.renderStats(styles.TierFull)
	if !strings.Contains(stats, "Loading") {
		t.Errorf("expected 'Loading' in stats during loading, got: %s", stats)
	}
}

func TestRenderStats_WithData(t *testing.T) {
	m, _ := testModelWithFile(t, 100)
	m.bufferedViewport.SetTotalLines(100)
	m.loading = false

	stats := m.renderStats(styles.TierFull)
	if !strings.Contains(stats, "Loaded") {
		t.Errorf("expected 'Loaded' in stats, got: %s", stats)
	}
	if !strings.Contains(stats, "Line:") {
		t.Errorf("expected 'Line:' in stats, got: %s", stats)
	}
}

func TestRenderStats_WithSearchState(t *testing.T) {
	m, _ := testModelWithFile(t, 100)
	m.bufferedViewport.SetTotalLines(100)
	m.loading = false
	m.searchState = NewSearchState(linesOf(5, 10, 15))

	stats := m.renderStats(styles.TierFull)
	if !strings.Contains(stats, "Matches:") {
		t.Errorf("expected 'Matches:' in stats with search state, got: %s", stats)
	}
}

func TestCheckBufferDown_ZeroTotalLines(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.bufferedViewport.buffer.TotalLines = 0

	cmd := m.checkBufferDown()
	if cmd != nil {
		t.Error("expected nil cmd when TotalLines=0")
	}
}

func TestCheckBufferUp_ZeroTotalLines(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.bufferedViewport.buffer.TotalLines = 0

	cmd := m.checkBufferUp()
	if cmd != nil {
		t.Error("expected nil cmd when TotalLines=0")
	}
}

func TestView_DoesNotPanic(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.bufferedViewport.SetTotalLines(50)
	m.loading = false

	lines := make([]string, 50)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	m.bufferedViewport.SetContentLines(lines)

	// Should not panic
	v := m.View()
	if v == "" {
		t.Error("View() should not return empty string")
	}
}

func TestUpdate_SetCursorMsg_KeepsRenderedPositionWhileTargetChunkLoads(t *testing.T) {
	m := loadedModel(t, 1000)
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})

	before := ansi.Strip(m.View())
	if !strings.Contains(before, "Loaded: 1-250/1000 | Line: 1") {
		t.Fatalf("test setup: unexpected initial viewport:\n%s", before)
	}

	_, cmd := m.Update(SetCursorMsg{Line: 500})
	if cmd == nil {
		t.Fatal("expected a command to load the target chunk")
	}

	pending := ansi.Strip(m.View())
	if !strings.Contains(pending, "Loaded: 1-250/1000 | Line: 1") {
		t.Fatalf("viewport moved before the target chunk arrived:\n%s", pending)
	}

	drain(t, m, cmd)
	loaded := ansi.Strip(m.View())
	if !strings.Contains(loaded, "Line: 500") || !strings.Contains(loaded, "[INFO] line 500: log output") {
		t.Fatalf("target line was not rendered after its chunk arrived:\n%s", loaded)
	}
}

func TestUpdate_SetCursorMsg_IgnoresOlderChunkResult(t *testing.T) {
	m := loadedModel(t, 1000)
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})

	_, firstCmd := m.Update(SetCursorMsg{Line: 500})
	_, secondCmd := m.Update(SetCursorMsg{Line: 700})
	if firstCmd == nil || secondCmd == nil {
		t.Fatal("expected both cursor jumps to load a chunk")
	}

	firstResult := firstChunkResult(t, firstCmd)
	first, ok := firstResult.(ChunkLoadedMsg)
	if !ok {
		t.Fatalf("first command returned %T, want ChunkLoadedMsg", firstResult)
	}
	secondResult := firstChunkResult(t, secondCmd)
	second, ok := secondResult.(ChunkLoadedMsg)
	if !ok {
		t.Fatalf("second command returned %T, want ChunkLoadedMsg", secondResult)
	}

	m.Update(second)
	m.Update(first)

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Line: 700") || strings.Contains(view, "Line: 500") {
		t.Fatalf("older chunk result replaced the latest cursor target:\n%s", view)
	}
}

func firstChunkResult(t *testing.T, cmd tea.Cmd) tea.Msg {
	t.Helper()
	result := cmd()
	batch, ok := result.(tea.BatchMsg)
	if !ok {
		return result
	}
	for _, child := range batch {
		if child == nil {
			continue
		}
		if msg := child(); msg != nil {
			if _, ok := msg.(spinner.TickMsg); !ok {
				return msg
			}
		}
	}
	return nil
}

func TestUpdate_NextMatchDoesNotQueueAnotherJumpWhileChunkLoads(t *testing.T) {
	m := loadedModel(t, 1000)
	m.Update(tea.WindowSizeMsg{Width: 200, Height: 40})
	m.searchState = NewSearchState(linesOf(1, 500, 700))

	_, cmd := m.Update(makeKeyPress("n"))
	if cmd == nil {
		t.Fatal("expected next match to load its target chunk")
	}
	m.Update(makeKeyPress("n"))

	pending := ansi.Strip(m.View())
	if !strings.Contains(pending, "Matches: 2/3") {
		t.Fatalf("another search jump was queued while the target chunk was loading:\n%s", pending)
	}

	drain(t, m, cmd)
	loaded := ansi.Strip(m.View())
	if !strings.Contains(loaded, "Matches: 2/3") || !strings.Contains(loaded, "Line: 500") {
		t.Fatalf("first search jump did not settle on its target:\n%s", loaded)
	}
}

func TestUpdate_SetCursorMsg_Visible(t *testing.T) {
	m, _ := testModelWithFile(t, 100)
	m.bufferedViewport.SetTotalLines(100)

	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	m.bufferedViewport.SetContentLines(lines)
	m.loading = false

	// Target a line that's visible (within the initial viewport)
	first := m.bufferedViewport.FirstVisibleLine()
	last := m.bufferedViewport.LastVisibleLine()
	target := (first + last) / 2

	_, cmd := m.Update(SetCursorMsg{Line: target, Init: false})
	// Should not need to load a chunk for a visible line
	if cmd != nil {
		t.Error("expected nil cmd for visible line SetCursorMsg")
	}
}

// Jumping to a match must scroll horizontally into view too, not just
// vertically — a match far to the right of a long line is useless to land
// on if it's still off-screen.
func TestUpdate_SetCursorMsg_ScrollsHorizontallyToColumn(t *testing.T) {
	m, _ := testModelWithFile(t, 100)
	m.bufferedViewport.SetTotalLines(100)

	lines := make([]string, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", 200)
	}
	m.bufferedViewport.SetContentLines(lines)
	m.loading = false
	m.searchInput.SetValue("word")

	target := m.bufferedViewport.FirstVisibleLine()

	m.Update(SetCursorMsg{Line: target, Col: 150, Init: false})

	if got := m.bufferedViewport.XOffset(); got == 0 {
		t.Error("expected XOffset > 0 after jumping to a match far to the right of a long line")
	}
}

func TestUpdate_SetCursorMsg_SelectsCurrentSearchOccurrence(t *testing.T) {
	styles.SwitchTheme(styles.Github)
	m, _ := testModelWithFile(t, 1)
	m.searchInput.SetValue("MATCH")
	m.searchState = NewSearchState([]MatchPosition{
		{Line: 1, Col: 0},
		{Line: 1, Col: 11},
	})
	match := m.searchState.NextMatch()
	m.bufferedViewport.SetTotalLines(1)
	raw := HighlightMatches(DefaultLogStyle("MATCH then MATCH"), "MATCH", styles.GetTheme().S().TextMatchStyle)
	m.bufferedViewport.SetContentLines([]string{raw})

	m.Update(SetCursorMsg{Line: match.Line, Col: match.Col})

	if m.bufferedViewport.currentSearchMatch != match {
		t.Fatalf("current search match = %+v, want %+v", m.bufferedViewport.currentSearchMatch, match)
	}
	if got := m.bufferedViewport.viewport.GetContent(); !strings.Contains(got, "48;2;102;78;28") {
		t.Fatalf("current search occurrence should use the dark highlight, got %q", got)
	}
}

// A match that already fits on screen from the left edge must reset any
// existing horizontal scroll, so it's not left off-screen to one side.
func TestUpdate_SetCursorMsg_ResetsHorizontalScrollWhenColumnFits(t *testing.T) {
	m, _ := testModelWithFile(t, 100)
	m.bufferedViewport.SetTotalLines(100)

	lines := make([]string, 100)
	for i := range lines {
		lines[i] = strings.Repeat("x", 200)
	}
	m.bufferedViewport.SetContentLines(lines)
	m.loading = false
	m.searchInput.SetValue("word")

	target := m.bufferedViewport.FirstVisibleLine()
	m.bufferedViewport.ScrollRight(horizontalScrollStep)
	if m.bufferedViewport.XOffset() == 0 {
		t.Fatal("test setup: expected non-zero XOffset before the cursor jump")
	}

	m.Update(SetCursorMsg{Line: target, Col: 0, Init: false})

	if got := m.bufferedViewport.XOffset(); got != 0 {
		t.Errorf("XOffset = %d, want 0 after jumping to a match that fits from the left edge", got)
	}
}

func TestHandleLogViewKeys_EnterVisualMode(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(50)
	m.bufferedViewport.SetCursorLine(7)

	cmd := m.handleLogViewKeys(makeKeyPress("v"))

	if !m.bufferedViewport.VisualMode() {
		t.Fatal("expected VisualMode() = true after 'v'")
	}
	from, to := m.bufferedViewport.SelectionRange()
	if from != 7 || to != 8 {
		t.Errorf("SelectionRange() = (%d, %d), want (7, 8) right after entering visual mode", from, to)
	}
	assertVisualModeFooterMessage(t, cmd, "Visual mode enabled")
}

func TestHandleVisualViewKeys_ExitWithEsc(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(50)
	m.bufferedViewport.SetVisualMode(true, m.bufferedViewport.CursorLine())

	cmd := m.handleLogViewKeys(makeKeyPress("esc"))

	if m.bufferedViewport.VisualMode() {
		t.Error("expected VisualMode() = false after esc")
	}
	assertVisualModeFooterMessage(t, cmd, "Visual mode disabled")
}

// Pressing the VisualMode key again while already selecting must exit
// visual mode (a toggle, like vim's 'v'), not be a no-op.
func TestHandleVisualViewKeys_ExitWithVisualModeKeyAgain(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(50)
	m.bufferedViewport.SetVisualMode(true, m.bufferedViewport.CursorLine())

	cmd := m.handleLogViewKeys(makeKeyPress("v"))

	if m.bufferedViewport.VisualMode() {
		t.Error("expected VisualMode() = false after pressing the VisualMode key again")
	}
	assertVisualModeFooterMessage(t, cmd, "Visual mode disabled")
}

func assertVisualModeFooterMessage(t *testing.T, cmd tea.Cmd, want string) {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected visual mode change to return a footer command")
	}
	raw := cmd()
	msg, ok := raw.(util.FooterNotiMsg)
	if !ok {
		t.Fatalf("visual mode change returned %T, want util.FooterNotiMsg", raw)
	}
	if msg.Message != want {
		t.Fatalf("footer message = %q, want %q", msg.Message, want)
	}
}

func TestHandleVisualViewKeys_MovementExtendsSelection(t *testing.T) {
	m, _ := testModelWithFile(t, 100)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(100)

	lines := make([]string, 100)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	m.bufferedViewport.SetContentLines(lines)

	m.bufferedViewport.SetVisualMode(true, 0)
	m.handleLogViewKeys(makeKeyPress("j"))
	m.handleLogViewKeys(makeKeyPress("j"))

	if !m.bufferedViewport.VisualMode() {
		t.Fatal("expected still in visual mode after movement")
	}
	from, to := m.bufferedViewport.SelectionRange()
	if from != 0 || to != 3 {
		t.Errorf("SelectionRange() = (%d, %d), want (0, 3) after moving down twice from anchor 0", from, to)
	}
}

func TestIsCapturingInput_VisualMode(t *testing.T) {
	m, _ := testModelWithFile(t, 50)
	m.activeView = LogView
	m.bufferedViewport.SetVisualMode(true, 0)

	if !m.IsCapturingInput() {
		t.Error("visual mode should capture input")
	}
}

func TestYankSelection_ReadsSelectedRangeFromDisk(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.bufferedViewport.SetTotalLines(10)
	m.bufferedViewport.SetCursorLine(2)
	m.bufferedViewport.SetVisualMode(true, 0)

	cmd := m.yankSelection()
	if cmd == nil {
		t.Fatal("expected non-nil cmd from yankSelection")
	}

	msg, ok := cmd().(YankCompletedMsg)
	if !ok {
		t.Fatalf("expected YankCompletedMsg, got %T", msg)
	}
	if msg.Err != nil {
		t.Fatalf("unexpected error: %v", msg.Err)
	}

	want := []string{
		"[INFO] line 1: log output",
		"[INFO] line 2: log output",
		"[INFO] line 3: log output",
	}
	if len(msg.Lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %v", len(msg.Lines), len(want), msg.Lines)
	}
	for i, line := range msg.Lines {
		if line != want[i] {
			t.Errorf("line[%d] = %q, want %q", i, line, want[i])
		}
	}
}

func TestHandleYankCompleted_CopiesAndExitsVisualMode(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.bufferedViewport.SetVisualMode(true, 0)

	cmd := m.handleYankCompleted(YankCompletedMsg{Lines: []string{"a", "b"}})
	if cmd == nil {
		t.Fatal("expected non-nil cmd")
	}
	if m.bufferedViewport.VisualMode() {
		t.Error("expected VisualMode() = false after handleYankCompleted")
	}

	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected tea.BatchMsg, got %T", batch)
	}
	if len(batch) != 2 {
		t.Fatalf("expected 2 batched cmds (clipboard + notification), got %d", len(batch))
	}

	notiMsg, ok := batch[1]().(util.FooterNotiMsg)
	if !ok {
		t.Fatalf("expected second batched cmd to produce util.FooterNotiMsg, got %T", notiMsg)
	}
	if notiMsg.Message != "Copied 2 lines" {
		t.Errorf("notification message = %q, want %q", notiMsg.Message, "Copied 2 lines")
	}
}

func TestHandleYankCompleted_Error(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.bufferedViewport.SetVisualMode(true, 0)

	cmd := m.handleYankCompleted(YankCompletedMsg{Err: fmt.Errorf("boom")})
	if m.bufferedViewport.VisualMode() {
		t.Error("expected VisualMode() = false after handleYankCompleted even on error")
	}

	errMsg, ok := cmd().(util.ErrorMsg)
	if !ok {
		t.Fatalf("expected util.ErrorMsg, got %T", errMsg)
	}
	if errMsg.Title != "Failed to copy selection" {
		t.Errorf("error title = %q, want %q", errMsg.Title, "Failed to copy selection")
	}
}

func TestHandleVisualViewKeys_Yank(t *testing.T) {
	m, _ := testModelWithFile(t, 10)
	m.activeView = LogView
	m.loading = false
	m.bufferedViewport.SetTotalLines(10)
	m.bufferedViewport.SetVisualMode(true, 0)

	cmd := m.handleLogViewKeys(makeKeyPress("y"))
	if cmd == nil {
		t.Fatal("expected non-nil cmd from yank ('y')")
	}
	if _, ok := cmd().(YankCompletedMsg); !ok {
		t.Error("expected 'y' in visual mode to produce a cmd yielding YankCompletedMsg")
	}
}

// pressKey feeds a key through the log view and drives the resulting commands
// back into the model, standing in for the Bubble Tea loop: a top/bottom jump
// that lands outside the loaded buffer only finishes once its ChunkLoadedMsg
// comes back.
func pressKey(t *testing.T, m *Model, keyStr string) {
	t.Helper()
	drain(t, m, m.handleLogViewKeys(makeKeyPress(keyStr)))
}

func drain(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	switch msg := cmd().(type) {
	case nil:
	case tea.BatchMsg:
		for _, c := range msg {
			drain(t, m, c)
		}
	default:
		_, next := m.Update(msg)
		drain(t, m, next)
	}
}

func loadedModel(t *testing.T, lineCount int) *Model {
	t.Helper()
	m, _ := testModelWithFile(t, lineCount)
	m.activeView = LogView
	drain(t, m, m.Init())
	if m.loading {
		t.Fatal("expected model to finish its initial load")
	}
	return m
}

func TestHandleLogViewKeys_GoToBottom(t *testing.T) {
	m := loadedModel(t, 500)

	pressKey(t, m, "G")

	if got, want := m.bufferedViewport.CursorLine(), 499; got != want {
		t.Errorf("CursorLine = %d, want %d after 'G'", got, want)
	}
	if !m.bufferedViewport.IsRealLastLine() {
		t.Error("expected cursor on the last line after 'G'")
	}
}

// "gg" is a two-key sequence: the first 'g' must be held, not acted on.
func TestHandleLogViewKeys_GoToTopSequence(t *testing.T) {
	m := loadedModel(t, 500)
	pressKey(t, m, "G")

	pressKey(t, m, "g")
	if m.pendingSeq != "g" {
		t.Errorf("pendingSeq = %q, want %q after first 'g'", m.pendingSeq, "g")
	}
	if m.bufferedViewport.IsRealFirstLine() {
		t.Fatal("expected a single 'g' not to move the cursor")
	}

	pressKey(t, m, "g")
	if got := m.bufferedViewport.CursorLine(); got != 0 {
		t.Errorf("CursorLine = %d, want 0 after 'gg'", got)
	}
	if m.pendingSeq != "" {
		t.Errorf("pendingSeq = %q, want empty after completing the sequence", m.pendingSeq)
	}
}

// A pending 'g' must not swallow the key that follows it when the pair isn't a
// binding — "gj" still scrolls down.
func TestHandleLogViewKeys_PendingSequenceFallsThrough(t *testing.T) {
	m := loadedModel(t, 500)

	pressKey(t, m, "g")
	pressKey(t, m, "j")

	if got := m.bufferedViewport.CursorLine(); got != 1 {
		t.Errorf("CursorLine = %d, want 1 after 'g' then 'j'", got)
	}
	if m.pendingSeq != "" {
		t.Errorf("pendingSeq = %q, want empty after a non-matching key", m.pendingSeq)
	}
}

// The named aliases jump immediately; only the doubled-rune form waits.
func TestHandleLogViewKeys_GoToNamedKeys(t *testing.T) {
	m := loadedModel(t, 500)

	drain(t, m, m.handleLogViewKeys(tea.KeyPressMsg{Code: tea.KeyEnd}))
	if got, want := m.bufferedViewport.CursorLine(), 499; got != want {
		t.Errorf("CursorLine = %d, want %d after 'end'", got, want)
	}

	drain(t, m, m.handleLogViewKeys(tea.KeyPressMsg{Code: tea.KeyHome}))
	if got := m.bufferedViewport.CursorLine(); got != 0 {
		t.Errorf("CursorLine = %d, want 0 after 'home'", got)
	}
}

// In visual mode the jumps extend the selection instead of being ignored.
func TestHandleLogViewKeys_GoToBottomExtendsSelection(t *testing.T) {
	m := loadedModel(t, 500)
	m.bufferedViewport.SetVisualMode(true, 0)

	pressKey(t, m, "G")

	from, to := m.bufferedViewport.SelectionRange()
	if from != 0 || to != 500 {
		t.Errorf("SelectionRange = (%d, %d), want (0, 500) after 'G' in visual mode", from, to)
	}
}

func TestGoTo_NoopAtBoundaries(t *testing.T) {
	m := loadedModel(t, 20)

	if cmd := m.goToTop(); cmd != nil {
		t.Error("expected nil cmd from goToTop when already on the first line")
	}

	drain(t, m, m.goToBottom())
	if cmd := m.goToBottom(); cmd != nil {
		t.Error("expected nil cmd from goToBottom when already on the last line")
	}
}
