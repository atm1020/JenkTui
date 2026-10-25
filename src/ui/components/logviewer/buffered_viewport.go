package logviewer

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/charmbracelet/x/ansi"
)

type BufferedViewport struct {
	viewport *viewport.Model
	buffer   BufferWindow

	cursorLine int // Absolute cursor line number in the file

	visualMode   bool
	visualAnchor int // Absolute line number where visual selection started

	currentSearchMatch      MatchPosition
	currentSearchMatchWidth int

	rawLines []string // last content set via SetContentLines, before any cursor/selection overlay
}

func NewBufferedViewport(width, height int) *BufferedViewport {
	v := viewport.New(
		viewport.WithWidth(width),
		viewport.WithHeight(height),
	)
	return &BufferedViewport{
		viewport:   &v,
		cursorLine: 0,
	}
}

// --- Viewport delegation methods ---

func (bv *BufferedViewport) Update(msg tea.Msg) tea.Cmd {
	resized := false
	if msg, ok := msg.(tea.WindowSizeMsg); ok {
		bv.viewport.SetWidth(msg.Width)
		bv.viewport.SetHeight(msg.Height)
		resized = true
	}

	vp, cmd := bv.viewport.Update(msg)
	bv.viewport = &vp
	if resized {
		bv.pushContent()
	}
	return cmd
}

func (bv *BufferedViewport) View() string {
	return bv.viewport.View()
}

func (bv *BufferedViewport) SetContentLines(lines []string) {
	bv.rawLines = lines
	bv.pushContent()
}

func (bv *BufferedViewport) setCurrentSearchMatch(match MatchPosition, width int) {
	bv.currentSearchMatch = match
	bv.currentSearchMatchWidth = width
}

func (bv *BufferedViewport) clearCurrentSearchMatch() {
	bv.currentSearchMatch = MatchPosition{}
	bv.currentSearchMatchWidth = 0
}

func (bv *BufferedViewport) SetStyle(style lipgloss.Style) {
	bv.viewport.Style = style
}

func (bv *BufferedViewport) ScrollLeft(columns int) {
	oldOffset := bv.viewport.XOffset()
	bv.viewport.ScrollLeft(columns)
	if bv.viewport.XOffset() != oldOffset {
		bv.pushContent()
	}
}

func (bv *BufferedViewport) ScrollRight(columns int) {
	oldOffset := bv.viewport.XOffset()
	bv.viewport.ScrollRight(columns)
	if bv.viewport.XOffset() != oldOffset {
		bv.pushContent()
	}
}

// EnsureColumnVisible scrolls horizontally so the given column range (display
// columns within the current line) is visible, without touching vertical
// scroll — vertical positioning is handled separately by the buffer-window
// scroll methods. Passing the viewport's own current YOffset as the "line"
// to the underlying EnsureVisible keeps its vertical-scroll branch a no-op.
func (bv *BufferedViewport) EnsureColumnVisible(colStart, colEnd int) {
	oldOffset := bv.viewport.XOffset()
	bv.viewport.EnsureVisible(bv.viewport.YOffset(), colStart, colEnd)
	if bv.viewport.XOffset() != oldOffset {
		bv.pushContent()
	}
}

func (bv *BufferedViewport) ToggleWrap() {
	bv.viewport.SoftWrap = !bv.viewport.SoftWrap
	bv.pushContent()
}

func (bv *BufferedViewport) WrapEnabled() bool {
	return bv.viewport.SoftWrap
}

func (bv *BufferedViewport) XOffset() int {
	return bv.viewport.XOffset()
}

func (bv *BufferedViewport) AtTop() bool {
	return bv.viewport.AtTop()
}

func (bv *BufferedViewport) AtBottom() bool {
	return bv.viewport.AtBottom()
}

func (bv *BufferedViewport) VisibleLineCount() int {
	return bv.viewport.VisibleLineCount()
}

func (bv *BufferedViewport) SetYOffset(offset int) {
	log.Error("Before SetYOffset", "offset", offset, "Y", bv.viewport.YOffset())
	log.Warn("Total lines", "lines", bv.viewport.TotalLineCount())
	bv.viewport.SetYOffset(offset)
	log.Error("After SetYOffset", "offset", offset, "Y", bv.viewport.YOffset())
}

func (bv *BufferedViewport) YOffset() int {
	return bv.viewport.YOffset()
}

// --- Buffer delegation methods ---

func (bv *BufferedViewport) GetRange() (from, to int) {
	return bv.buffer.GetRange()
}

func (bv *BufferedViewport) SetTotalLines(totalLines int) {
	bv.buffer.SetTotalLines(totalLines, bv.viewport.VisibleLineCount())
	bv.cursorLine = 0
	bv.updateCursorHighlight()
}

func (bv *BufferedViewport) TotalLines() int {
	return bv.buffer.TotalLines
}

func (bv *BufferedViewport) IsLastChunk() bool {
	return bv.buffer.IsLastChunk()
}

func (bv *BufferedViewport) IsFirstChunk() bool {
	return bv.buffer.IsFirstChunk()
}

func (bv *BufferedViewport) ShouldLoadDownFor(line int) bool {
	return bv.buffer.ShouldLoadDownFor(line)
}

func (bv *BufferedViewport) ShouldLoadDown() bool {
	return bv.buffer.ShouldLoadDown(bv.LastVisibleLine())
}

func (bv *BufferedViewport) ShouldLoadUp() bool {
	return bv.buffer.ShouldLoadUp()
}

func (bv *BufferedViewport) ShouldLoadUpFor(line int) bool {
	return bv.buffer.ShouldLoadUpFor(line)
}

func (bv *BufferedViewport) CalculateNewBufferRangeDown(targetLine int) {
	bv.cursorLine = targetLine
	bv.buffer.CalculateNewBufferRangeDown(targetLine, bv.viewport.VisibleLineCount())
}

func (bv *BufferedViewport) CalculateNewBufferRangeUp(targetLine int) {
	bv.cursorLine = targetLine
	bv.buffer.CalculateNewBufferRangeUp(targetLine, bv.viewport.VisibleLineCount())
}

func (bv *BufferedViewport) bufferRangeDownFor(targetLine int) (from, to int) {
	buffer := bv.buffer
	buffer.CalculateNewBufferRangeDown(targetLine, bv.viewport.VisibleLineCount())
	return buffer.GetRange()
}

func (bv *BufferedViewport) bufferRangeUpFor(targetLine int) (from, to int) {
	buffer := bv.buffer
	buffer.CalculateNewBufferRangeUp(targetLine, bv.viewport.VisibleLineCount())
	return buffer.GetRange()
}

func (bv *BufferedViewport) setBufferRange(from, to int) {
	bv.buffer.From = from
	bv.buffer.To = to
}

// --- Cursor and position methods ---

func (bv *BufferedViewport) CursorLine() int {
	return bv.cursorLine
}

func (bv *BufferedViewport) SetCursorLine(line int) {
	bv.cursorLine = line
}

func (bv *BufferedViewport) CursorInViewport() int {
	return bv.cursorLine - bv.buffer.From
}

func (bv *BufferedViewport) LastVisibleLine() int {
	return bv.buffer.From + bv.viewport.YOffset() + bv.viewport.VisibleLineCount()
}

func (bv *BufferedViewport) FirstVisibleLine() int {
	return bv.buffer.From + bv.viewport.YOffset() + 1
}

// --- Visual selection methods ---

// SetVisualMode toggles line-wise visual selection. anchorLine is the fixed
// end of the range when entering (active=true); the cursor extends the other
// end as it moves. anchorLine is ignored when active is false.
func (bv *BufferedViewport) SetVisualMode(active bool, anchorLine int) {
	bv.visualMode = active
	bv.visualAnchor = anchorLine
	bv.updateCursorHighlight()
}

// VisualMode reports whether line-wise visual selection is currently active.
func (bv *BufferedViewport) VisualMode() bool {
	return bv.visualMode
}

// SelectionRange returns the absolute, half-open [from, to) line range
// currently selected in visual mode, ordered regardless of whether the
// cursor is above or below the anchor.
func (bv *BufferedViewport) SelectionRange() (from, to int) {
	lo, hi := bv.visualAnchor, bv.cursorLine
	if lo > hi {
		lo, hi = hi, lo
	}
	return lo, hi + 1
}

// --- Boundary check methods ---

func (bv *BufferedViewport) IsRealLastLine() bool {
	return bv.cursorLine == bv.buffer.TotalLines-1
}

func (bv *BufferedViewport) IsRealFirstLine() bool {
	return bv.cursorLine == 0
}

func (bv *BufferedViewport) IsLastVisibleLine() bool {
	return bv.LastVisibleLine() == bv.cursorLine
}

func (bv *BufferedViewport) IsFirstVisibleLine() bool {
	return bv.CursorInViewport() == bv.viewport.YOffset()-1
}

func (bv *BufferedViewport) IsVisible(line int) bool {
	firstVisible := bv.FirstVisibleLine()
	lastVisible := bv.LastVisibleLine()
	log.Debug("IsVisible", "first", firstVisible, "last", lastVisible)
	return line >= firstVisible && line < lastVisible
}

// --- Scroll methods ---

func (bv *BufferedViewport) ScrollDown(line int) {
	bv.cursorLine++
	if bv.IsLastVisibleLine() {
		bv.viewport.ScrollDown(1)
	}
	bv.updateCursorHighlight()
}

func (bv *BufferedViewport) ScrollUp(line int) {
	bv.cursorLine--
	if bv.IsFirstVisibleLine() {
		bv.viewport.ScrollUp(1)
	}
	bv.updateCursorHighlight()
}

func (bv *BufferedViewport) AdjustForScrollUpTo(targetLine int) {
	oldYOffset := bv.viewport.YOffset()
	oldCursor := bv.cursorLine
	bv.cursorLine = targetLine - 1
	YOffset := max(0, bv.cursorLine-bv.buffer.From)
	bv.SetYOffset(YOffset)
	log.Debug("AdjustForScrollUpTo",
		"targetLine", targetLine,
		"oldCursor", oldCursor, "newCursor", bv.cursorLine,
		"oldYOffset", oldYOffset, "newYOffset", YOffset,
		"bufferFrom", bv.buffer.From,
		"visibleLines", bv.viewport.VisibleLineCount(),
	)
}

func (bv *BufferedViewport) AdjustForScrollDownTo(targetLine int) {
	oldYOffset := bv.viewport.YOffset()
	oldCursor := bv.cursorLine
	YOffset := targetLine - bv.viewport.VisibleLineCount() - bv.buffer.From
	bv.SetYOffset(YOffset)
	bv.cursorLine = targetLine - 1
	log.Debug("AdjustForScrollDownTo",
		"targetLine", targetLine,
		"oldCursor", oldCursor, "newCursor", bv.cursorLine,
		"oldYOffset", oldYOffset, "newYOffset", YOffset,
		"bufferFrom", bv.buffer.From,
		"visibleLines", bv.viewport.VisibleLineCount(),
	)
}

// --- Rendering helpers ---

func (bv *BufferedViewport) updateCursorHighlight() {
	cursorRelative := bv.cursorLine - bv.buffer.From
	selStart, selEnd := cursorRelative, cursorRelative
	if bv.visualMode {
		anchorRelative := bv.visualAnchor - bv.buffer.From
		selStart, selEnd = min(cursorRelative, anchorRelative), max(cursorRelative, anchorRelative)
	}
	bv.viewport.LeftGutterFunc = bv.createGutterFunc(cursorRelative, bv.visualMode, selStart, selEnd)
	bv.applyLineHighlight()
}

// applyLineHighlight rebuilds the bounded viewport buffer from rawLines after
// cursor or visual-selection state changes. This does not reread or restyle the
// whole log file; only the currently loaded buffer window is involved.
func (bv *BufferedViewport) applyLineHighlight() {
	bv.pushContent()
}

// pushContent overlays either the normal cursor line or the visual selection
// onto a copy of rawLines. Both use the same subtle Neovim-style row background
// and keep embedded token colors, especially search matches.
func (bv *BufferedViewport) pushContent() {
	display := make([]string, len(bv.rawLines))
	copy(display, bv.rawLines)
	bv.applyCurrentSearchMatch(display)

	cursorRelative := bv.cursorLine - bv.buffer.From
	if !bv.visualMode {
		if cursorRelative >= 0 && cursorRelative < len(display) {
			display[cursorRelative] = renderLineBackground(
				display[cursorRelative],
				bv.cursorLineRenderWidth(),
				styles.GetTheme().SelectedBackground,
			)
		}
		bv.viewport.SetContentLines(display)
		return
	}

	anchorRelative := bv.visualAnchor - bv.buffer.From
	selStart, selEnd := min(cursorRelative, anchorRelative), max(cursorRelative, anchorRelative)
	width := bv.cursorLineRenderWidth()
	background := styles.GetTheme().SelectedBackground
	for i := max(0, selStart); i <= selEnd && i < len(display); i++ {
		display[i] = renderLineBackground(display[i], width, background)
	}
	bv.viewport.SetContentLines(display)
}

func (bv *BufferedViewport) applyCurrentSearchMatch(lines []string) {
	match := bv.currentSearchMatch
	if match.Line <= 0 || match.Col < 0 || bv.currentSearchMatchWidth <= 0 {
		return
	}

	lineIndex := match.Line - 1 - bv.buffer.From
	if lineIndex < 0 || lineIndex >= len(lines) {
		return
	}

	matchEnd := min(match.Col+bv.currentSearchMatchWidth, ansi.StringWidth(lines[lineIndex]))
	if matchEnd <= match.Col {
		return
	}
	lines[lineIndex] = lipgloss.StyleRanges(
		lines[lineIndex],
		lipgloss.NewRange(match.Col, matchEnd, styles.GetTheme().S().TextCurrentMatchStyle),
	)
}

// cursorLineRenderWidth includes the horizontally hidden prefix so cutting the
// line at XOffset still leaves enough highlighted cells to fill the viewport.
// Soft-wrapped content never uses XOffset.
func (bv *BufferedViewport) cursorLineRenderWidth() int {
	gutterWidth := 0
	if bv.viewport.LeftGutterFunc != nil {
		gutterWidth = lipgloss.Width(bv.viewport.LeftGutterFunc(viewport.GutterContext{}))
	}
	width := max(0, bv.viewport.Width()-bv.viewport.Style.GetHorizontalFrameSize()-gutterWidth)
	if !bv.viewport.SoftWrap {
		width += bv.viewport.XOffset()
	}
	return width
}

// renderLineBackground paints a minimum-width row while preserving embedded
// ANSI foregrounds and backgrounds. When an embedded SGR resets its background
// (as Lipgloss search spans do), the cursor background is reasserted for the
// rest of the row instead of ending at the highlighted match.
func renderLineBackground(line string, minWidth int, background color.Color) string {
	if padding := minWidth - ansi.StringWidth(line); padding > 0 {
		line += strings.Repeat(" ", padding)
	}

	backgroundSequence := ansi.NewStyle().BackgroundColor(background).String()
	var rendered strings.Builder
	rendered.Grow(len(line) + len(backgroundSequence) + len(ansi.ResetStyle))
	rendered.WriteString(backgroundSequence)

	parser := ansi.GetParser()
	defer ansi.PutParser(parser)
	for len(line) > 0 {
		sequenceStart := nextANSISequence(line)
		if sequenceStart < 0 {
			rendered.WriteString(line)
			break
		}
		rendered.WriteString(line[:sequenceStart])
		line = line[sequenceStart:]

		sequence, _, consumed, _ := ansi.DecodeSequence(line, ansi.NormalState, parser)
		if consumed == 0 {
			rendered.WriteByte(line[0])
			line = line[1:]
			continue
		}
		rendered.WriteString(sequence)
		if isSGRSequence(sequence) && sgrLeavesDefaultBackground(parser.Params()) {
			rendered.WriteString(backgroundSequence)
		}
		line = line[consumed:]
	}
	rendered.WriteString(ansi.ResetStyle)
	return rendered.String()
}

func nextANSISequence(line string) int {
	escape := strings.IndexByte(line, '\x1b')
	csi := strings.IndexByte(line, '\x9b')
	switch {
	case escape < 0:
		return csi
	case csi < 0:
		return escape
	default:
		return min(escape, csi)
	}
}

func isSGRSequence(sequence string) bool {
	if len(sequence) < 2 || sequence[len(sequence)-1] != 'm' {
		return false
	}
	return sequence[0] == '\x9b' || len(sequence) >= 3 && sequence[0] == '\x1b' && sequence[1] == '['
}

// sgrLeavesDefaultBackground reports whether an SGR sequence's final
// background state is the terminal default. Extended foreground colors are
// skipped as a unit so an RGB component equal to 0 or 49 is not mistaken for a
// reset code.
func sgrLeavesDefaultBackground(params ansi.Params) bool {
	if len(params) == 0 {
		return true
	}

	const (
		backgroundUnchanged = iota
		backgroundDefault
		backgroundExplicit
	)
	state := backgroundUnchanged

	for i := 0; i < len(params); i++ {
		code := params[i].Param(0)
		if params[i].HasMore() {
			for i < len(params)-1 && params[i].HasMore() {
				i++
			}
			switch code {
			case 0, 49:
				state = backgroundDefault
			case 48:
				state = backgroundExplicit
			}
			continue
		}

		switch code {
		case 0, 49:
			state = backgroundDefault
		case 48:
			state = backgroundExplicit
			i += extendedColorParameterCount(params[i+1:])
		case 38, 58:
			i += extendedColorParameterCount(params[i+1:])
		default:
			if code >= 40 && code <= 47 || code >= 100 && code <= 107 {
				state = backgroundExplicit
			}
		}
	}

	return state == backgroundDefault
}

func extendedColorParameterCount(params ansi.Params) int {
	if len(params) == 0 {
		return 0
	}
	if params[0].HasMore() {
		count := 1
		for count < len(params) && params[count-1].HasMore() {
			count++
		}
		return count
	}

	switch params[0].Param(0) {
	case 2:
		return min(4, len(params)) // mode + red + green + blue
	case 5:
		return min(2, len(params)) // mode + palette index
	default:
		return 1
	}
}

func (bv *BufferedViewport) createGutterFunc(cursorLine int, visualMode bool, selStart, selEnd int) viewport.GutterFunc {
	g := styles.GetTheme().S().Gutter
	return func(ctx viewport.GutterContext) string {
		isSelected := visualMode && ctx.Index >= selStart && ctx.Index <= selEnd
		isCursor := ctx.Index == cursorLine

		if ctx.Soft {
			if isSelected {
				return g.VisualLine.Render("    ") + g.VisualSeparator.Render("│ ")
			}
			if isCursor && !visualMode {
				return g.CurrentLine.Render("    ") + g.CurrentSeparator.Render("│ ")
			}
			return g.SoftWrap.Render("    ") + g.Separator.Render("│ ")
		}

		if ctx.Index >= ctx.TotalLines {
			return g.SoftWrap.Render("  ~ ") + g.Separator.Render("│ ")
		}

		if isSelected {
			lineNum := fmt.Sprintf("%4d", ctx.Index+1+bv.buffer.From)
			return g.VisualLine.Render(lineNum) + g.VisualSeparator.Render("│ ")
		}

		if isCursor && !visualMode {
			lineNum := fmt.Sprintf("%4d", ctx.Index+1+bv.buffer.From)
			return g.CurrentLine.Render(lineNum) + g.CurrentSeparator.Render("|") + g.CurrentMarker.Render("›")
		}

		// Absolute line numbers
		lineNum := fmt.Sprintf("%4d", ctx.Index+1+bv.buffer.From)
		return g.LineNumber.Render(lineNum) + g.Separator.Render("│ ")
	}
}
