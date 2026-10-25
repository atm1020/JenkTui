package logviewer

import (
	"fmt"
	"strings"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
)

type ActiveView int

const (
	LogView ActiveView = iota
	SearchView
)

type Model struct {
	width  int
	height int

	// Components
	searchInput textinput.Model
	spinner     spinner.Model
	keymap      KeyMap

	// File state
	loader   *Loader
	filePath string

	bufferedViewport *BufferedViewport

	// View state
	activeView       ActiveView
	loading          bool
	spinning         bool
	session          uint64
	chunkGeneration  uint64
	searchGeneration uint64

	// pendingSeq holds the keys of a half-typed multi-key binding (vim's "gg"),
	// cleared as soon as the next key either completes it or doesn't.
	pendingSeq string

	// Search state
	searchState *SearchState

	// Context
	ctx *context.ProgramContext
}

func NewModel(ctx *context.ProgramContext, width, height int, logFilePath string) *Model {
	ti := textinput.New()
	ti.Placeholder = "Search in logs..."
	ti.CharLimit = 32
	ti.SetWidth(width)

	headerSize := 3
	viewportHeight := height - headerSize

	return &Model{
		ctx:              ctx,
		width:            width,
		height:           height,
		filePath:         logFilePath,
		bufferedViewport: NewBufferedViewport(width, viewportHeight),
		searchInput:      ti,
		keymap:           UserKeyMap(ctx.Config.KeyMapConfig.Logviewer),
		activeView:       LogView,
		loading:          true,
		spinner:          newSpinner(),
		loader:           NewLoader(logFilePath),
	}
}

func newSpinner() spinner.Model {
	return spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(styles.GetTheme().S().Spinner),
	)
}

func (m *Model) startSpinner() tea.Cmd {
	if m.spinning {
		return nil
	}
	m.spinner = newSpinner()
	m.spinning = true
	return m.spinner.Tick
}

func (m *Model) stopSpinner() {
	m.spinning = false
}

func (m *Model) Init() tea.Cmd {
	m.loading = true
	m.session++
	return tea.Batch(m.scanLinesAndLoad(), m.startSpinner())
}

// CleanUp stops the viewer-owned spinner chain when its wrapper view leaves
// the router. Pending spinner messages are rejected after the next restart by
// the fresh spinner model created in startSpinner.
func (m *Model) CleanUp() tea.Cmd {
	m.stopSpinner()
	m.session++
	m.chunkGeneration++
	m.searchGeneration++
	return nil
}

// scanLinesAndLoad scans file line metrics and loads the initial chunk.
func (m *Model) scanLinesAndLoad() tea.Cmd {
	session := m.session
	return func() tea.Msg {
		lineCount, err := m.loader.ScanLines()
		if err != nil {
			return loadFailedMsg{Error: err, Title: "Failed to read log file", session: session}
		}

		log.Info("File ready", "path", m.filePath, "lines", lineCount)
		return FileReadyMsg{
			FilePath:   m.filePath,
			LineCount:  lineCount,
			generation: session,
		}
	}
}

// loadChunk loads the viewport's current range.
func (m *Model) loadChunk(scrollType ScrollType) tea.Cmd {
	from, to := m.bufferedViewport.GetRange()
	return m.loadChunkRequest(chunkLoadRequest{
		from:       from,
		to:         to,
		scrollType: scrollType,
	})
}

// loadChunkRequest captures every value needed by the asynchronous file read.
// The active viewport remains untouched until the resulting ChunkLoadedMsg is
// handled, so View never combines a new range with the previous range's lines.
func (m *Model) loadChunkRequest(request chunkLoadRequest) tea.Cmd {
	m.loading = true
	start := m.startSpinner()
	m.chunkGeneration++
	request.generation = m.chunkGeneration
	request.session = m.session
	pattern := m.searchInput.Value()
	loader := m.loader

	load := func() tea.Msg {
		styleFn := func(line string) string {
			line = DefaultLogStyle(line)
			if pattern != "" {
				line = HighlightMatches(line, pattern, styles.GetTheme().S().TextMatchStyle)
			}
			return line
		}

		log.Debug("Loading chunk", "from", request.from, "to", request.to)
		lines, err := loader.LoadChunk(request.from, request.to, styleFn)
		if err != nil {
			return loadFailedMsg{
				Error:           err,
				Title:           "Failed to load log chunk",
				chunkGeneration: request.generation,
				session:         request.session,
			}
		}

		return ChunkLoadedMsg{
			Lines:      lines,
			ScrollType: request.scrollType,
			request:    &request,
		}
	}
	return tea.Batch(load, start)
}

// searchFile searches the file
func (m *Model) searchFile(pattern string) tea.Cmd {
	m.searchGeneration++
	generation := m.searchGeneration
	session := m.session
	return func() tea.Msg {
		matches, err := SearchFile(m.filePath, pattern)
		if err != nil {
			return loadFailedMsg{Error: err, Title: "Search failed", searchGeneration: generation, session: session}
		}

		return SearchCompletedMsg{Matches: matches, generation: generation, session: session}
	}
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m, m.handleKeyMsg(msg)
	case tea.WindowSizeMsg:
		return m, m.handleWindowSizeMsg(msg)
	case FileReadyMsg:
		return m, m.handleFileReady(msg)
	case ChunkLoadedMsg:
		return m, m.handleChunkLoaded(msg)
	case SearchCompletedMsg:
		return m, m.handleSearchCompleted(msg)
	case loadFailedMsg:
		return m, m.handleLoadFailed(msg)
	case YankCompletedMsg:
		return m, m.handleYankCompleted(msg)
	case SetCursorMsg:
		return m, m.handleSetCursor(msg)
	case spinner.TickMsg:
		return m, tea.Batch(m.handleSpinnerTick(msg), m.bufferedViewport.Update(msg))
	case cursor.BlinkMsg:
		return m, m.handleCursorBlink(msg)
	default:
		return m, m.updateViewport(msg)
	}
}

func (m *Model) handleCursorBlink(msg cursor.BlinkMsg) tea.Cmd {
	if m.activeView != SearchView {
		return nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return cmd
}

// View renders the model
func (m Model) View() string {
	t := styles.GetTheme()

	// Border styles based on active view
	var viewportBorder, searchBorder lipgloss.Style
	if m.activeView == SearchView {
		viewportBorder = t.S().DefaultBorder
		searchBorder = t.S().SelectedBorder
	} else {
		viewportBorder = t.S().SelectedBorder
		searchBorder = t.S().DefaultBorder
	}

	viewportBorder = viewportBorder.
		BorderTop(true).
		BorderBottom(true).
		BorderLeft(false).
		BorderRight(false)

	// The header is a search box (left) and a stats readout (right) joined
	// horizontally. If their combined width exceeds m.width the row wraps and the
	// header breaks on narrow terminals — the previous fixed Width(m.width/2) plus a
	// verbose, never-shrinking stats string did exactly that. Fix both levers: the
	// stats shrink by width tier (renderStats), then the search box takes whatever
	// space is left, floored so it stays usable.
	tier := styles.WidthTierFor(m.width)

	statsStyle := t.S().Base.
		Foreground(t.Grey).
		PaddingLeft(1).
		PaddingRight(1).
		PaddingBottom(1).
		Align(lipgloss.Right)
	stats := statsStyle.Render(m.renderStats(tier))

	const searchChrome = 4 // rounded border (2) + horizontal padding (2)
	searchWidth := max(12, m.width-lipgloss.Width(stats)-searchChrome)
	m.searchInput.SetWidth(searchWidth)
	searchInput := searchBorder.
		Width(searchWidth).
		MaxHeight(3).
		Render(m.searchInput.View())

	// Viewport
	m.bufferedViewport.SetStyle(viewportBorder)
	body := m.bufferedViewport.View()

	return lipgloss.JoinVertical(
		lipgloss.Top,
		lipgloss.JoinHorizontal(
			lipgloss.Right,
			searchInput,
			stats,
		),
		body,
	)
}

func (m Model) KeyMap() help.KeyMap {
	return m.keymap
}

func (m *Model) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	m.width = msg.Width
	m.height = msg.Height
	return m.bufferedViewport.Update(msg)
}

func (m *Model) handleFileReady(msg FileReadyMsg) tea.Cmd {
	if msg.generation != 0 && msg.generation != m.session {
		return nil
	}
	m.bufferedViewport.SetTotalLines(msg.LineCount)
	return tea.Batch(m.loadChunk(None))
}

func (m *Model) handleChunkLoaded(msg ChunkLoadedMsg) tea.Cmd {
	if msg.request != nil && msg.request.generation != m.chunkGeneration {
		log.Debug("Ignoring stale chunk", "generation", msg.request.generation, "current", m.chunkGeneration)
		return nil
	}
	if msg.request != nil && msg.request.session != m.session {
		return nil
	}

	log.Debug("Set contents", "lines", len(msg.Lines))
	if msg.request != nil {
		m.bufferedViewport.setBufferRange(msg.request.from, msg.request.to)
	}
	m.bufferedViewport.SetContentLines(msg.Lines)

	if msg.request != nil && msg.request.cursor != nil {
		m.applyLoadedCursor(*msg.request.cursor, msg.request.scrollType)
	} else {
		m.adjustAfterLegacyChunkLoad(msg.ScrollType)
	}

	m.bufferedViewport.updateCursorHighlight()
	m.loading = false
	m.stopSpinner()
	return nil
}

func (m *Model) applyLoadedCursor(target chunkCursorTarget, scrollType ScrollType) {
	switch scrollType {
	case Up:
		m.bufferedViewport.AdjustForScrollUpTo(target.line)
	case Down:
		m.bufferedViewport.AdjustForScrollDownTo(target.line)
	default:
		m.bufferedViewport.SetCursorLine(target.line - 1)
	}

	if !target.ensureColumn {
		return
	}
	msg := SetCursorMsg{Line: target.line, Col: target.col}
	m.syncCurrentSearchMatch(msg)
	m.bufferedViewport.EnsureColumnVisible(target.col, target.col+len(m.searchInput.Value()))
}

func (m *Model) adjustAfterLegacyChunkLoad(scrollType ScrollType) {
	if scrollType == Up {
		m.bufferedViewport.AdjustForScrollUpTo(m.bufferedViewport.CursorLine())
	}
	if scrollType == Down {
		m.bufferedViewport.AdjustForScrollDownTo(m.bufferedViewport.CursorLine())
	}
}

func (m *Model) handleSpinnerTick(msg spinner.TickMsg) tea.Cmd {
	if !m.spinning || !m.loading {
		return nil
	}
	var cmd tea.Cmd
	m.spinner, cmd = m.spinner.Update(msg)
	return cmd
}

func (m *Model) handleLoadFailed(msg loadFailedMsg) tea.Cmd {
	if msg.session != 0 && msg.session != m.session {
		return nil
	}
	if (msg.chunkGeneration != 0 && msg.chunkGeneration != m.chunkGeneration) ||
		(msg.searchGeneration != 0 && msg.searchGeneration != m.searchGeneration) {
		return nil
	}
	m.loading = false
	m.stopSpinner()
	return util.CmdHandler(util.ErrorMsg{Error: msg.Error, Title: msg.Title})
}

func (m *Model) updateViewport(msg tea.Msg) tea.Cmd {
	return m.bufferedViewport.Update(msg)
}

// renderStats renders the header's right-hand readout, shrinking its verbosity by
// width tier so it never overruns a narrow header. Full/medium show line, load
// range and scroll; narrow and tiny collapse to line/total and percent.
func (m Model) renderStats(tier styles.WidthTier) string {
	spinnerView := "  "
	if m.loading {
		spinnerView = m.spinner.View()
	}

	// Handle case where buffer is not yet initialized
	if m.bufferedViewport.TotalLines() == 0 {
		if tier <= styles.TierNarrow {
			return spinnerView
		}
		return spinnerView + " Loading..."
	}

	from, to := m.bufferedViewport.GetRange()
	scrollPct := m.calcScrollPercentage()
	total := m.bufferedViewport.TotalLines()
	line := m.bufferedViewport.CursorLine() + 1

	var stats, matchStats string
	switch tier {
	case styles.TierFull:
		stats = fmt.Sprintf(" Loaded: %d-%d/%d | Line: %d | Scroll: %.0f%%", from+1, to, total, line, scrollPct)
		if m.searchState != nil {
			matchStats = fmt.Sprintf(" Matches: %d/%d | ", m.searchState.CurrentIndex()+1, m.searchState.TotalMatches())
		}
	case styles.TierMedium:
		stats = fmt.Sprintf(" Line: %d/%d | %.0f%%", line, total, scrollPct)
		if m.searchState != nil {
			matchStats = fmt.Sprintf(" %d/%d | ", m.searchState.CurrentIndex()+1, m.searchState.TotalMatches())
		}
	default: // narrow + tiny: minimal readout
		stats = fmt.Sprintf(" %d/%d %.0f%%", line, total, scrollPct)
	}

	return spinnerView + matchStats + stats
}

// calcScrollPercentage calculates scroll percentage
func (m *Model) calcScrollPercentage() float64 {
	if m.bufferedViewport.TotalLines() == 0 {
		return 0
	}
	y := float64(m.bufferedViewport.CursorLine() + 1)
	t := float64(m.bufferedViewport.TotalLines())
	return (y / t) * 100
}

// handleKeyMsg handles key messages
func (m *Model) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	// A half-typed sequence ("g" of "gg") only survives consecutive log-view
	// keys; anything typed into the search box abandons it.
	if m.activeView != LogView {
		m.pendingSeq = ""
	}

	// Switch between views
	if key.Matches(msg, m.keymap.ComponentUp) {
		m.activeView = SearchView
		return m.searchInput.Focus()
	}

	if key.Matches(msg, m.keymap.ComponentDown) {
		m.activeView = LogView
		return nil
	}

	// Open search only from the log view; while the search box is already focused,
	// the search key ("/") types into the query instead of resetting it.
	if m.activeView == LogView && key.Matches(msg, m.keymap.Search) {
		m.activeView = SearchView
		m.searchInput.Reset()
		return m.searchInput.Focus()
	}

	// Route to active view
	if m.activeView == SearchView {
		return m.handleSearchViewKeys(msg)
	}

	return m.handleLogViewKeys(msg)
}

// IsCapturingInput reports whether the search box is focused and receiving text, so
// the app shell can withhold its global letter shortcuts (Back "b", Forward "f",
// MyRuns "m", Help "?") rather than firing them into the query.
func (m *Model) IsCapturingInput() bool {
	return m.activeView == SearchView || m.bufferedViewport.VisualMode()
}

// handleSearchViewKeys handles keys in search view
func (m *Model) handleSearchViewKeys(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, m.keymap.Enter) {
		m.loading = true
		return tea.Batch(m.searchFile(m.searchInput.Value()), m.startSpinner())
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return cmd
}

// handleLogViewKeys handles keys in log view
const horizontalScrollStep = 6

func (m *Model) handleLogViewKeys(msg tea.KeyMsg) tea.Cmd {
	if m.loading {
		return nil
	}

	// Top/bottom jumps are resolved before visual mode so "gg"/"G" extend a
	// selection the same way they move the cursor in normal mode.
	if cmd, handled := m.handleGotoKeys(msg); handled {
		return cmd
	}

	if m.bufferedViewport.VisualMode() {
		return m.handleVisualViewKeys(msg)
	}

	if key.Matches(msg, m.keymap.VisualMode) {
		m.bufferedViewport.SetVisualMode(true, m.bufferedViewport.CursorLine())
		return visualModeChangedCmd(true)
	}

	// Search navigation
	if key.Matches(msg, m.keymap.Next) && m.searchState != nil {
		match := m.searchState.NextMatch()
		return m.handleSetCursor(SetCursorMsg{Line: match.Line, Col: match.Col})
	}

	if key.Matches(msg, m.keymap.Prev) && m.searchState != nil {
		match := m.searchState.PrevMatch()
		return m.handleSetCursor(SetCursorMsg{Line: match.Line, Col: match.Col})
	}

	// Horizontal scrolling is independent of the selected log line.
	if key.Matches(msg, m.keymap.Left) {
		m.bufferedViewport.ScrollLeft(horizontalScrollStep)
		return nil
	}

	if key.Matches(msg, m.keymap.Right) {
		m.bufferedViewport.ScrollRight(horizontalScrollStep)
		return nil
	}

	if key.Matches(msg, m.keymap.Wrap) {
		m.bufferedViewport.ToggleWrap()
		return nil
	}

	// Vertical scrolling
	if key.Matches(msg, m.keymap.Down) {
		return m.scrollDown()
	}

	if key.Matches(msg, m.keymap.Up) {
		return m.scrollUp()
	}

	if key.Matches(msg, m.keymap.PageDown) {
		return m.pageDown()
	}

	if key.Matches(msg, m.keymap.PageUp) {
		return m.pageUp()
	}

	return nil
}

// scrollDown advances the cursor one line down, loading a new buffer chunk if
// the cursor is nearing the loaded window's edge. Shared by normal and visual
// mode movement so both trigger the same buffer-window reload behavior.
func (m *Model) scrollDown() tea.Cmd {
	if m.bufferedViewport.IsRealLastLine() {
		return nil
	}

	m.bufferedViewport.ScrollDown(1)
	if m.bufferedViewport.ShouldLoadDownFor(m.bufferedViewport.CursorLine()) {
		log.Debug("scrollDown: loading new buffer range downward")
		cursorLine := m.bufferedViewport.CursorLine()
		from, to := m.bufferedViewport.bufferRangeDownFor(cursorLine)
		return m.loadChunkRequest(chunkLoadRequest{
			from:       from,
			to:         to,
			scrollType: Down,
			cursor:     &chunkCursorTarget{line: cursorLine + 1},
		})
	}
	return nil
}

// scrollUp retreats the cursor one line up, loading a new buffer chunk if the
// cursor is nearing the loaded window's edge. Shared by normal and visual mode
// movement so both trigger the same buffer-window reload behavior.
func (m *Model) scrollUp() tea.Cmd {
	if m.bufferedViewport.IsRealFirstLine() {
		return nil
	}

	m.bufferedViewport.ScrollUp(1)
	if m.bufferedViewport.ShouldLoadUpFor(m.bufferedViewport.CursorLine()) {
		log.Debug("scrollUp: loading new buffer range upward")
		cursorLine := m.bufferedViewport.CursorLine()
		from, to := m.bufferedViewport.bufferRangeUpFor(cursorLine)
		return m.loadChunkRequest(chunkLoadRequest{
			from:       from,
			to:         to,
			scrollType: Up,
			cursor:     &chunkCursorTarget{line: cursorLine + 1},
		})
	}
	return nil
}

func (m *Model) pageDown() tea.Cmd {
	if m.bufferedViewport.IsRealLastLine() {
		return nil
	}
	target := min(
		m.bufferedViewport.CursorLine()+m.bufferedViewport.VisibleLineCount(),
		m.bufferedViewport.TotalLines()-1,
	)
	return m.handleSetCursor(SetCursorMsg{Line: target + 1})
}

func (m *Model) pageUp() tea.Cmd {
	if m.bufferedViewport.IsRealFirstLine() {
		return nil
	}
	target := max(m.bufferedViewport.CursorLine()-m.bufferedViewport.VisibleLineCount(), 0)
	return m.handleSetCursor(SetCursorMsg{Line: target + 1})
}

// handleGotoKeys resolves the top/bottom jumps and reports whether it consumed
// the key. It also drives the multi-key sequence used by vim's "gg": the first
// press is held in pendingSeq and only the second completes the binding. Any
// other key clears the pending press and falls through to normal handling, so
// "g" followed by "j" still scrolls down.
func (m *Model) handleGotoKeys(msg tea.KeyMsg) (tea.Cmd, bool) {
	typed := m.pendingSeq + msg.String()
	m.pendingSeq = ""

	switch {
	case matchesKey(typed, m.keymap.GoToTop):
		return m.goToTop(), true
	case matchesKey(typed, m.keymap.GoToBottom):
		return m.goToBottom(), true
	case isSequencePrefix(typed, m.keymap.GoToTop), isSequencePrefix(typed, m.keymap.GoToBottom):
		m.pendingSeq = typed
		return nil, true
	}

	return nil, false
}

// goToTop jumps the cursor to the file's first line, reloading the buffer
// window when the target sits outside it.
func (m *Model) goToTop() tea.Cmd {
	if m.bufferedViewport.TotalLines() == 0 || m.bufferedViewport.IsRealFirstLine() {
		return nil
	}
	return m.handleSetCursor(SetCursorMsg{Line: 1})
}

// goToBottom jumps the cursor to the file's last line, reloading the buffer
// window when the target sits outside it.
func (m *Model) goToBottom() tea.Cmd {
	if m.bufferedViewport.TotalLines() == 0 || m.bufferedViewport.IsRealLastLine() {
		return nil
	}
	return m.handleSetCursor(SetCursorMsg{Line: m.bufferedViewport.TotalLines()})
}

// handleVisualViewKeys handles keys while line-wise visual selection is
// active: movement (shared with normal mode via scrollDown/scrollUp), yank,
// and exit. Unrecognized keys (search, wrap, horizontal scroll) no-op, matching
// vim's visual mode.
func (m *Model) handleVisualViewKeys(msg tea.KeyMsg) tea.Cmd {
	// Esc exits visual mode, and so does pressing the VisualMode key again
	// (a toggle, like vim's 'v') rather than leaving it a no-op.
	if key.Matches(msg, m.keymap.ExitVisualMode) || key.Matches(msg, m.keymap.VisualMode) {
		m.bufferedViewport.SetVisualMode(false, 0)
		return visualModeChangedCmd(false)
	}

	if key.Matches(msg, m.keymap.Yank) {
		return m.yankSelection()
	}

	if key.Matches(msg, m.keymap.Down) {
		return m.scrollDown()
	}

	if key.Matches(msg, m.keymap.Up) {
		return m.scrollUp()
	}

	return nil
}

// yankSelection reads the selected line range directly from disk — not from
// the buffered viewport's in-memory window, which can slide and evict lines
// outside the currently loaded chunk — and returns a command that emits
// YankCompletedMsg once the read finishes.
func visualModeChangedCmd(enabled bool) tea.Cmd {
	message := "Visual mode disabled"
	if enabled {
		message = "Visual mode enabled"
	}
	return util.CmdHandler(util.FooterNotiMsg{Message: message, Type: util.NotiInfo})
}

func (m *Model) yankSelection() tea.Cmd {
	from, to := m.bufferedViewport.SelectionRange()
	return func() tea.Msg {
		lines, err := m.loader.LoadChunk(from, to, nil)
		return YankCompletedMsg{Lines: lines, Err: err}
	}
}

// handleYankCompleted copies the yanked lines to the system clipboard via
// OSC 52 (tea.SetClipboard) and reports how many lines were copied.
func (m *Model) handleYankCompleted(msg YankCompletedMsg) tea.Cmd {
	m.bufferedViewport.SetVisualMode(false, 0)

	if msg.Err != nil {
		return util.CmdHandler(util.ErrorMsg{Error: msg.Err, Title: "Failed to copy selection"})
	}

	text := strings.Join(msg.Lines, "\n")
	return tea.Batch(
		tea.SetClipboard(text),
		util.CmdHandler(util.FooterNotiMsg{
			Message: fmt.Sprintf("Copied %d lines", len(msg.Lines)),
			Type:    util.NotiInfo,
		}),
	)
}

// checkBufferDown checks if buffer needs updating on scroll down
func (m *Model) checkBufferDown() tea.Cmd {
	if m.bufferedViewport.TotalLines() == 0 {
		return nil
	}

	if m.bufferedViewport.ShouldLoadDown() {
		m.loading = true
		return m.loadChunk(Down)
	}

	return nil
}

// checkBufferUp checks if buffer needs updating on scroll up
func (m *Model) checkBufferUp() tea.Cmd {
	if m.bufferedViewport.TotalLines() == 0 {
		return nil
	}

	if m.bufferedViewport.ShouldLoadUp() {
		m.loading = true
		return m.loadChunk(Up)
	}

	return nil
}

// handleSearchCompleted handles search completion
func (m *Model) handleSearchCompleted(msg SearchCompletedMsg) tea.Cmd {
	if msg.session != 0 && msg.session != m.session {
		return nil
	}
	if msg.generation != 0 && msg.generation != m.searchGeneration {
		return nil
	}
	m.searchState = NewSearchState(msg.Matches)
	m.bufferedViewport.clearCurrentSearchMatch()
	m.bufferedViewport.updateCursorHighlight()

	m.loading = false
	m.stopSpinner()
	if m.searchState.TotalMatches() == 0 {
		return util.CmdHandler(util.FooterNotiMsg{
			Message: fmt.Sprintf("No matches found for '%s'", m.searchInput.Value()),
			Type:    util.NotiWarning,
		})
	}

	m.activeView = LogView
	match := m.searchState.CurrentMatch()
	return m.handleSetCursor(SetCursorMsg{Line: match.Line, Col: match.Col, Init: true})
}

// handleSetCursor handles cursor positioning
func (m *Model) handleSetCursor(msg SetCursorMsg) tea.Cmd {
	log.Info("SetCursorMsg", "pos", msg.Line, "init", msg.Init)
	from, to := m.bufferedViewport.GetRange()
	log.Debug("SetCursor",
		"targetLine", msg.Line,
		"init", msg.Init,
		"cursorLine", m.bufferedViewport.CursorLine(),
		"bufferFrom", from,
		"bufferTo", to,
		"yOffset", m.bufferedViewport.YOffset(),
		"firstVisible", m.bufferedViewport.FirstVisibleLine(),
		"lastVisible", m.bufferedViewport.LastVisibleLine(),
		"totalLines", m.bufferedViewport.TotalLines(),
	)

	reload := msg.Init
	scrollType := None

	// Move cursor if the target line is visible.
	switch {
	case m.bufferedViewport.IsVisible(msg.Line):
		log.Debug("SetCursor: target is visible, moving cursor in place",
			"targetLine", msg.Line,
		)
		if !reload {
			m.bufferedViewport.SetCursorLine(msg.Line - 1)
		}

	case m.bufferedViewport.CursorLine() >= msg.Line:
		scrollType = Up
		log.Debug("SetCursor: target is ABOVE cursor",
			"cursorLine", m.bufferedViewport.CursorLine(),
			"targetLine", msg.Line,
			"yOffset", m.bufferedViewport.YOffset(),
			"needsLoad", m.bufferedViewport.ShouldLoadUpFor(msg.Line),
		)
		if m.bufferedViewport.ShouldLoadUpFor(msg.Line) {
			log.Debug("SetCursor: loading new buffer range upward")
			from, to = m.bufferedViewport.bufferRangeUpFor(msg.Line)
			reload = true
		} else if !reload {
			log.Debug("SetCursor: target in buffer, adjusting scroll position upward")
			m.bufferedViewport.AdjustForScrollUpTo(msg.Line)
		}

	default:
		scrollType = Down
		log.Debug("SetCursor: target is BELOW cursor",
			"cursorLine", m.bufferedViewport.CursorLine(),
			"targetLine", msg.Line,
			"needsLoad", m.bufferedViewport.ShouldLoadDownFor(msg.Line),
		)
		if m.bufferedViewport.ShouldLoadDownFor(msg.Line) {
			log.Debug("SetCursor: loading new buffer range downward")
			from, to = m.bufferedViewport.bufferRangeDownFor(msg.Line)
			reload = true
		} else if !reload {
			log.Debug("SetCursor: target in buffer, adjusting scroll position downward")
			m.bufferedViewport.AdjustForScrollDownTo(msg.Line)
		}
	}

	if reload {
		return m.loadChunkRequest(chunkLoadRequest{
			from:       from,
			to:         to,
			scrollType: scrollType,
			cursor: &chunkCursorTarget{
				line:         msg.Line,
				col:          msg.Col,
				ensureColumn: true,
			},
		})
	}

	m.syncCurrentSearchMatch(msg)
	m.bufferedViewport.updateCursorHighlight()
	// Bring the match's column into view too — vertical positioning above
	// only handles which line is visible, not where on that (possibly
	// horizontally-scrolled) line the match actually sits.
	m.bufferedViewport.EnsureColumnVisible(msg.Col, msg.Col+len(m.searchInput.Value()))
	return nil
}

func (m *Model) syncCurrentSearchMatch(msg SetCursorMsg) {
	if m.searchState == nil || m.searchState.TotalMatches() == 0 {
		return
	}
	match := m.searchState.CurrentMatch()
	if match.Line != msg.Line || match.Col != msg.Col {
		return
	}
	m.bufferedViewport.setCurrentSearchMatch(match, len(m.searchInput.Value()))
}
