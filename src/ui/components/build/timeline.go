package build

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/stagegraph"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"github.com/charmbracelet/x/ansi"
)

const (
	timelineMinBarWidth   = 6
	timelineDurationWidth = 8
	timelineGapWidth      = 2
	timelineMinHeight     = 4
	timelineMaxHeight     = 11
)

// renderTimeline draws the Build page's Omaci-inspired Stage Timeline View.
// Every stage and parallel branch sits on one shared horizontal time axis with
// tree connectors, status indicators, monospace block bars, a live `now` marker
// while running, and right-aligned durations. All columns scale cleanly so rows
// never wrap or overflow horizontally.
func (m *Model) renderTimeline() string {
	g := m.currentStageGraph()
	contentW := m.timelineContentWidth()
	nameW, barW, durW := timelineColumnWidths(contentW)
	layout := stagegraph.ComputeTimeline(g, barW, 0)

	t := styles.GetTheme()
	lines := make([]string, 0, len(layout.Rows)+2)
	tickCols := buildTickColSet(layout, barW)

	if barW >= timelineMinBarWidth {
		lines = append(lines, renderTimelineAxisHeader(layout, nameW, barW, durW, t))
		lines = append(lines, renderTimelineHeaderDivider(contentW, nameW, barW, tickCols, t))
	}

	var pulseRunes []rune
	if m.pulseBar != nil {
		pulseRunes = []rune(m.pulseBar.View())
	}
	for i := range layout.Rows {
		row := &layout.Rows[i]
		focused := row.Navigable && row.NavIndex == m.focusedStageIdx
		lines = append(lines, m.renderTimelineRow(row, layout, tickCols, nameW, barW, durW, focused, pulseRunes, t))
	}

	return strings.Join(lines, "\n")
}

func buildTickColSet(layout stagegraph.TimelineLayout, barW int) []bool {
	if barW <= 0 {
		return nil
	}
	cols := make([]bool, barW)
	for _, tick := range layout.Ticks {
		if tick.Label == "now" {
			continue
		}
		if tick.Col >= 0 && tick.Col < barW {
			cols[tick.Col] = true
		}
	}
	return cols
}

func renderTimelineHeaderDivider(contentW, nameW, barW int, tickCols []bool, t *styles.Theme) string {
	rule := make([]rune, contentW)
	for i := range rule {
		rule[i] = '─'
	}
	barOffset := 4 + nameW + timelineGapWidth
	for col, hasTick := range tickCols {
		if !hasTick {
			continue
		}
		pos := barOffset + col
		if pos >= 0 && pos < contentW {
			rule[pos] = '┬'
		}
	}
	return t.S().Base.Foreground(t.FaintBorder).Render(string(rule))
}

func (m *Model) timelineContentWidth() int {
	// Framed stages panel reserves 2 columns for left/right borders and 1 space
	// of right breathing room inside the frame.
	w := m.wWidth - 2
	if w <= 0 {
		return 80
	}
	return w
}

func (m *Model) stagesPanelMeta() (note, count string) {
	nav := m.navigableStages()
	if len(nav) == 0 {
		return "", ""
	}
	var running, failed, paused, passed, pending int
	for _, st := range nav {
		act := st.Status.Activity(0)
		out := st.Status.Outcome()
		switch {
		case out == buildstatus.OutcomeFailure:
			failed++
		case act == buildstatus.ActivityRunning:
			running++
		case act == buildstatus.ActivityPaused:
			paused++
		case out == buildstatus.OutcomeSuccess:
			passed++
		default:
			pending++
		}
	}
	var parts []string
	if failed > 0 {
		parts = append(parts, fmt.Sprintf("%d failed", failed))
	}
	if running > 0 {
		parts = append(parts, fmt.Sprintf("%d running", running))
	}
	if paused > 0 {
		parts = append(parts, fmt.Sprintf("%d paused", paused))
	}
	if pending > 0 {
		parts = append(parts, fmt.Sprintf("%d to come", pending))
	}
	if passed > 0 {
		parts = append(parts, fmt.Sprintf("%d passed", passed))
	}
	return strings.Join(parts, ", "), fmt.Sprintf("%d", len(nav))
}

func timelineColumnWidths(contentW int) (nameW, barW, durW int) {
	// Left fixed prefix: "▸ " (2) + status icon + " " (2) = 4 cells.
	const prefixW = 4
	durW = timelineDurationWidth
	if contentW < 24 {
		durW = 6
	}

	switch {
	case contentW >= 100:
		nameW = clampInt(contentW*28/100, 22, 34)
	case contentW >= 70:
		nameW = clampInt(contentW*30/100, 16, 26)
	case contentW >= 40:
		nameW = clampInt(contentW*38/100, 12, 20)
	default:
		nameW = max(6, contentW-prefixW-durW-1)
	}

	barW = contentW - prefixW - nameW - timelineGapWidth - durW - 1
	if barW < timelineMinBarWidth {
		barW = 0
		nameW = max(4, contentW-prefixW-durW-1)
	}
	return nameW, barW, durW
}

func renderTimelineAxisHeader(layout stagegraph.TimelineLayout, nameW, barW, durW int, t *styles.Theme) string {
	subtle := t.S().Base.Foreground(t.SubtleText)
	leftPad := 4 + nameW + timelineGapWidth
	stageLabel := "  STAGE"
	if len(stageLabel) > leftPad {
		stageLabel = ansi.Truncate(stageLabel, leftPad, "")
	}
	left := subtle.Render(stageLabel + strings.Repeat(" ", max(0, leftPad-ansi.StringWidth(stageLabel))))

	// Lay out tick labels onto a rune canvas of exactly barW cells.
	axis := make([]rune, barW)
	for i := range axis {
		axis[i] = ' '
	}
	nowStart, nowEnd := -1, -1
	for _, tick := range layout.Ticks {
		runes := []rune(tick.Label)
		for j, r := range runes {
			pos := tick.Col + j
			if pos >= 0 && pos < barW {
				axis[pos] = r
			}
		}
		if tick.Label == "now" {
			nowStart = tick.Col
			nowEnd = min(barW, tick.Col+len(runes))
		}
	}

	var axisRendered string
	if nowStart >= 0 && nowEnd > nowStart {
		nowStyle := t.S().Base.Foreground(t.Warning).Bold(true)
		axisRendered = subtle.Render(string(axis[:nowStart])) +
			nowStyle.Render(string(axis[nowStart:nowEnd])) +
			subtle.Render(string(axis[nowEnd:]))
	} else {
		axisRendered = subtle.Render(string(axis))
	}

	durLabel := "DUR"
	durHeader := subtle.Render(strings.Repeat(" ", max(0, durW-len(durLabel))) + durLabel)

	return left + axisRendered + " " + durHeader
}

func (m *Model) renderTimelineRow(
	row *stagegraph.TimelineRow,
	layout stagegraph.TimelineLayout,
	tickCols []bool,
	nameW, barW, durW int,
	focused bool,
	pulseRunes []rune,
	t *styles.Theme,
) string {
	rowBase := t.S().Base
	if focused {
		rowBase = rowBase.Background(t.SelectedBackground)
	}

	pointer := rowBase.Render("  ")
	if focused {
		pointer = rowBase.Foreground(t.StageFocusedBorder).Bold(true).Render("▸ ")
	}

	// Place tree connectors before the status icon so parallel children hang
	// directly beneath their parent stage rather than behind a flat column of icons.
	labelColW := nameW + 2
	label := renderTimelineLabel(row, labelColW, focused, t)
	dur := renderTimelineDuration(row.Node, durW, focused, t)

	if barW < timelineMinBarWidth {
		line := pointer + label + " " + dur
		return ansi.Truncate(line, m.timelineContentWidth(), "…")
	}

	track := renderTimelineTrack(row, layout, tickCols, barW, pulseRunes, t)
	return pointer + label + strings.Repeat(" ", timelineGapWidth) + track + " " + dur
}

func renderTimelineLabel(row *stagegraph.TimelineRow, colW int, focused bool, t *styles.Theme) string {
	rowBase := t.S().Base
	if focused {
		rowBase = rowBase.Background(t.SelectedBackground)
	}

	prefix := row.TreePrefix
	prefixW := ansi.StringWidth(prefix)
	icon := timelineStatusIcon(row.Node.Status, focused, t)
	iconW := ansi.StringWidth(icon) + 1 // icon + trailing space

	availNameW := max(0, colW-prefixW-iconW)
	name := ansi.Truncate(row.Node.Name, availNameW, "…")
	usedW := prefixW + iconW + ansi.StringWidth(name)
	pad := strings.Repeat(" ", max(0, colW-usedW))

	treeStyle := rowBase.Foreground(t.SubtleText)
	nameStyle := rowBase
	switch {
	case focused:
		nameStyle = nameStyle.Bold(true).Foreground(t.FgBase)
	case row.Node.IsWrapper && row.Node.IsGroup():
		nameStyle = nameStyle.Bold(true).Foreground(t.SubtleText)
	case row.Node.Status.Outcome() == buildstatus.OutcomeFailure:
		nameStyle = nameStyle.Foreground(t.Error)
	case row.Node.Status.Outcome() == buildstatus.OutcomeNotBuilt:
		nameStyle = nameStyle.Foreground(t.SubtleText)
	}

	return treeStyle.Render(prefix) + icon + rowBase.Render(" ") + nameStyle.Render(name) + rowBase.Render(pad)
}

func renderTimelineTrack(
	row *stagegraph.TimelineRow,
	layout stagegraph.TimelineLayout,
	tickCols []bool,
	barW int,
	pulseRunes []rune,
	t *styles.Theme,
) string {
	status := row.Node.Status
	activity := status.Activity(0)

	// Paused stages surface the explicit approval prompt so users immediately
	// see that Jenkins is waiting for input.
	if activity == buildstatus.ActivityPaused {
		label := "Waiting for approval"
		if barW >= len(label) {
			pad := strings.Repeat(" ", barW-len(label))
			return t.S().Base.Foreground(t.Warning).Render(label) + pad
		}
		return t.S().Base.Foreground(t.Warning).Render(ansi.Truncate(label, barW, "…"))
	}

	nowCol := -1
	if layout.Active && layout.NowCol >= 0 && layout.NowCol < barW {
		nowCol = layout.NowCol
	}

	if !row.HasBar || row.BarLen <= 0 {
		return renderEmptyTrack(barW, nowCol, tickCols, t)
	}

	barStart := clampInt(row.BarStart, 0, barW-1)
	barEnd := clampInt(barStart+row.BarLen, barStart+1, barW)

	var b strings.Builder
	// Before bar.
	b.WriteString(renderGapSegment(0, barStart, nowCol, tickCols, t))
	// Bar itself.
	b.WriteString(renderBarSegment(row, barEnd-barStart, pulseRunes, t))
	// After bar.
	b.WriteString(renderGapSegment(barEnd, barW, nowCol, tickCols, t))
	return b.String()
}

func renderEmptyTrack(barW, nowCol int, tickCols []bool, t *styles.Theme) string {
	return renderGapSegment(0, barW, nowCol, tickCols, t)
}

func renderGapSegment(from, to, nowCol int, tickCols []bool, t *styles.Theme) string {
	if to <= from {
		return ""
	}
	gridStyle := t.S().Base.Foreground(t.FaintBorder)
	nowStyle := t.S().Base.Foreground(t.Warning)

	var b strings.Builder
	spanStart := from
	flushSpaces := func(end int) {
		if end <= spanStart {
			return
		}
		b.WriteString(strings.Repeat(" ", end-spanStart))
	}

	for col := from; col < to; col++ {
		switch {
		case col == nowCol:
			flushSpaces(col)
			b.WriteString(nowStyle.Render("│"))
			spanStart = col + 1
		case col < len(tickCols) && tickCols[col]:
			flushSpaces(col)
			b.WriteString(gridStyle.Render("│"))
			spanStart = col + 1
		}
	}
	flushSpaces(to)
	return b.String()
}

func renderBarSegment(row *stagegraph.TimelineRow, width int, pulseRunes []rune, t *styles.Theme) string {
	if width <= 0 {
		return ""
	}
	status := row.Node.Status
	activity := status.Activity(0)
	outcome := status.Outcome()

	barColor := timelineBarColor(status, t)
	if row.Node.IsWrapper && row.Node.IsGroup() {
		// Group headings use a light rule so the half-block branch bars below stand out cleanly.
		glyph := "─"
		if activity == buildstatus.ActivityRunning {
			glyph = "╌"
		}
		return t.S().Base.Foreground(barColor).Render(strings.Repeat(glyph, width))
	}

	if activity == buildstatus.ActivityRunning {
		return t.S().Base.Foreground(barColor).Render(runningBarPattern(width, pulseRunes))
	}

	// Lower half-block ("▄") leaves a 50% vertical gap between adjacent rows so
	// concurrent parallel bars never visually fuse into a single solid block.
	glyph := "▄"
	if outcome == buildstatus.OutcomeAborted || activity == buildstatus.ActivityQueued || activity == buildstatus.ActivityPending {
		glyph = "┄"
	}
	return t.S().Base.Foreground(barColor).Render(strings.Repeat(glyph, width))
}

func runningBarPattern(width int, pulseRunes []rune) string {
	if width <= 0 {
		return ""
	}
	if len(pulseRunes) == 0 {
		return strings.Repeat("▄", width)
	}
	var b strings.Builder
	b.Grow(width * 3)
	for i := range width {
		b.WriteRune(separatedPulseRune(pulseRunes[i%len(pulseRunes)]))
	}
	return b.String()
}

// separatedPulseRune maps full-height pulse glyphs onto lower-block heights
// (max 5/8 cell) so concurrently running parallel rows stay visually separated.
func separatedPulseRune(r rune) rune {
	switch r {
	case '█':
		return '▅'
	case '▓':
		return '▄'
	case '▒':
		return '▃'
	case '░':
		return '▂'
	default:
		return '▄'
	}
}

func timelineBarColor(status buildstatus.PipelineStatus, t *styles.Theme) color.Color {
	activity := status.Activity(0)
	outcome := status.Outcome()
	switch {
	case outcome == buildstatus.OutcomeFailure:
		return t.Error
	case activity == buildstatus.ActivityRunning:
		return t.Running
	case activity == buildstatus.ActivityPaused, outcome == buildstatus.OutcomeUnstable:
		return t.Warning
	case outcome == buildstatus.OutcomeSuccess:
		return t.Success
	case outcome == buildstatus.OutcomeAborted:
		return t.Grey
	default:
		return t.SubtleText
	}
}

func timelineStatusIcon(status buildstatus.PipelineStatus, focused bool, t *styles.Theme) string {
	base := t.S().Base
	if focused {
		base = base.Background(t.SelectedBackground)
	}
	activity := status.Activity(0)
	outcome := status.Outcome()
	switch {
	case outcome == buildstatus.OutcomeFailure:
		return base.Foreground(t.Error).Render(styles.FailureIcon)
	case activity == buildstatus.ActivityRunning:
		return base.Foreground(t.Running).Render(styles.WaitingIcon)
	case activity == buildstatus.ActivityPaused:
		return base.Foreground(t.Warning).Render(styles.WaitingIcon)
	case outcome == buildstatus.OutcomeUnstable:
		return base.Foreground(t.Warning).Render(styles.WaitingIcon)
	case outcome == buildstatus.OutcomeSuccess:
		return base.Foreground(t.Success).Render(styles.SuccessIcon)
	case outcome == buildstatus.OutcomeAborted:
		return base.Foreground(t.Grey).Render(styles.NotBuildedIcon)
	case outcome == buildstatus.OutcomeNotBuilt:
		return base.Foreground(t.Grey).Render(styles.NotBuildedIcon)
	default:
		return base.Foreground(t.Grey).Render(styles.UknownStateIcon)
	}
}

func renderTimelineDuration(node stagegraph.StageNode, durW int, focused bool, t *styles.Theme) string {
	var text string
	if node.Status.Outcome() == buildstatus.OutcomeNotBuilt {
		text = "-"
	} else {
		text = util.FormatDuration(node.DurationMillis)
	}
	if ansi.StringWidth(text) > durW {
		text = ansi.Truncate(text, durW, "…")
	}
	padded := strings.Repeat(" ", max(0, durW-ansi.StringWidth(text))) + text
	style := t.S().Base.Foreground(t.SubtleText)
	if focused {
		style = style.Foreground(t.FgBase).Bold(true)
	}
	return style.Render(padded)
}

func (m *Model) timelineViewportHeight(content string) int {
	lines := strings.Count(content, "\n") + 1
	if len(m.pipelineRun.Stages) == 0 {
		return max(1, min(lines, timelineMaxHeight))
	}
	maxH := timelineMaxHeight
	if m.wHeight > 0 {
		maxH = clampInt(m.wHeight*28/100, timelineMinHeight, timelineMaxHeight)
	}
	return max(1, min(lines, maxH))
}

func (m *Model) focusedTimelineLineIndex() int {
	g := m.currentStageGraph()
	rows := stagegraph.Flatten(g)
	_, barW, _ := timelineColumnWidths(m.timelineContentWidth())
	headerOffset := 0
	if barW >= timelineMinBarWidth {
		// Axis label row + horizontal divider row.
		headerOffset = 2
	}
	for i := range rows {
		if rows[i].Navigable && rows[i].NavIndex == m.focusedStageIdx {
			return i + headerOffset
		}
	}
	return 0
}

func (m *Model) ensureFocusedStageVisible() {
	if m.stageView == nil || len(m.pipelineRun.Stages) == 0 {
		return
	}
	h := m.stageView.Height()
	if h <= 0 {
		return
	}
	line := m.focusedTimelineLineIndex()
	y := m.stageView.YOffset()
	if line < y {
		m.stageView.SetYOffset(max(0, line-1))
	} else if line >= y+h {
		m.stageView.SetYOffset(line - h + 1)
	}
}

func clampInt(v, low, high int) int {
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
