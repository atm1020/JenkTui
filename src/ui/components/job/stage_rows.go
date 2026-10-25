package job

import (
	"strings"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/ui/styles"

	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

// stageStamp is the visible stage data that invalidates a cached row.
type stageStamp struct {
	name     string
	status   string
	duration int64
}

// windowCache is the fitted visible window. Unchanged lines are reused so an
// animation frame does not remeasure the whole stage document.
type windowCache struct {
	valid         bool
	width, height int
	x, y, longest int
	lines         []string
	src           []string
	fitted        []bool
	blank         string
	text          string
}

func (svm *StageViewModel) renderStageRows(width, height int) string {
	boxWidth := styles.StageBoxContentWidth(width)
	if boxWidth != svm.cachedBoxWidth {
		// Cached rows were rendered for a specific box width.
		svm.ClearCache()
		svm.cachedBoxWidth = boxWidth
	}
	svm.syncRowTheme()
	svm.assembleRows(boxWidth)
	if !svm.useFastWindow() {
		return svm.renderThroughViewport()
	}
	svm.syncViewportScrollback()
	return svm.renderWindow()
}

func (svm *StageViewModel) syncRowTheme() {
	theme := styles.GetCurrentThemeType().String()
	if theme == svm.cachedTheme {
		return
	}
	if svm.cachedTheme != "" {
		svm.stageRenderCache = make(map[string]*StageRenderCache)
		svm.window = windowCache{}
	}
	svm.cachedTheme = theme
}

func (svm *StageViewModel) assembleRows(boxWidth int) {
	if svm.stageRenderCache == nil {
		svm.stageRenderCache = make(map[string]*StageRenderCache)
	}
	var spinner, pulse string
	animLoaded := false

	svm.lines = svm.lines[:0]
	longest := 0
	for idx, run := range svm.pipelineRuns {
		lines, widths := svm.rowLines(run, idx, boxWidth, &spinner, &pulse, &animLoaded)
		svm.lines = append(svm.lines, lines...)
		for _, width := range widths {
			if width > longest {
				longest = width
			}
		}
	}
	// The historical row join wrote a newline after every row, which leaves one
	// trailing empty line on the document rather than a blank line between rows.
	if len(svm.pipelineRuns) > 0 {
		svm.lines = append(svm.lines, "")
	}
	if longest != svm.longest || len(svm.lines) != svm.syncedCount {
		svm.scrollDirty = true
	}
	svm.longest = longest
}

func (svm *StageViewModel) rowLines(run *gojenkins.PipelineRun, idx, boxWidth int, spinner, pulse *string, animLoaded *bool) ([]string, []int) {
	if run == nil {
		svm.rowRenders++
		return svm.renderRowLines(nil, idx, boxWidth)
	}
	selected := idx == svm.selectedIndex
	entry := svm.stageRenderCache[run.ID]
	if entry.matches(run, selected, svm, spinner, pulse, animLoaded) {
		return entry.lines, entry.widths
	}
	lines, widths := svm.renderRowLines(run, idx, boxWidth)
	svm.rowRenders++
	svm.stageRenderCache[run.ID] = newRowCache(run, selected, lines, widths, svm, spinner, pulse, animLoaded)
	return lines, widths
}

func (c *StageRenderCache) matches(run *gojenkins.PipelineRun, selected bool, svm *StageViewModel, spinner, pulse *string, animLoaded *bool) bool {
	if c == nil || !c.ready || run == nil {
		return false
	}
	if c.selected != selected || c.status != run.Status || c.start != run.StartTime || c.end != run.EndTime || c.duration != run.Duration {
		return false
	}
	if c.hasArtifacts != runHasArtifacts(run) || len(c.stages) != len(run.Stages) {
		return false
	}
	for i := range run.Stages {
		stage := &run.Stages[i]
		if c.stages[i].name != stage.Name || c.stages[i].status != stage.Status || c.stages[i].duration != stage.Duration {
			return false
		}
	}
	if !c.showsSpinner && !c.showsPulse {
		return true
	}
	svm.loadAnimation(spinner, pulse, animLoaded)
	if c.showsSpinner && c.spinner != *spinner {
		return false
	}
	if c.showsPulse && c.pulse != *pulse {
		return false
	}
	return true
}

func newRowCache(run *gojenkins.PipelineRun, selected bool, lines []string, widths []int, svm *StageViewModel, spinner, pulse *string, animLoaded *bool) *StageRenderCache {
	showsSpinner, showsPulse := rowShowsAnimation(run)
	entry := &StageRenderCache{
		Content:      strings.Join(lines, "\n"),
		lines:        lines,
		widths:       widths,
		ready:        true,
		selected:     selected,
		status:       run.Status,
		start:        run.StartTime,
		end:          run.EndTime,
		duration:     run.Duration,
		hasArtifacts: runHasArtifacts(run),
		stages:       make([]stageStamp, len(run.Stages)),
		showsSpinner: showsSpinner,
		showsPulse:   showsPulse,
	}
	for i := range run.Stages {
		entry.stages[i] = stageStamp{
			name:     run.Stages[i].Name,
			status:   run.Stages[i].Status,
			duration: run.Stages[i].Duration,
		}
	}
	if showsSpinner || showsPulse {
		svm.loadAnimation(spinner, pulse, animLoaded)
		entry.spinner = *spinner
		entry.pulse = *pulse
	}
	return entry
}

func (svm *StageViewModel) loadAnimation(spinner, pulse *string, loaded *bool) {
	if *loaded {
		return
	}
	*spinner = svm.spinnerModel.View()
	if svm.pulseBar != nil {
		*pulse = svm.pulseBar.View()
	}
	*loaded = true
}

func (svm *StageViewModel) renderRowLines(run *gojenkins.PipelineRun, idx, boxWidth int) ([]string, []int) {
	lines := strings.Split(svm.renderRun(run, idx, boxWidth), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	widths := make([]int, len(lines))
	for i, line := range lines {
		widths[i] = ansi.StringWidth(line)
	}
	return lines, widths
}

func rowShowsAnimation(run *gojenkins.PipelineRun) (spinner, pulse bool) {
	if run == nil {
		return false, false
	}
	spinner = buildstatus.PipelineStatus(run.Status).IsExecuting()
	for i := range run.Stages {
		if buildstatus.PipelineStatus(run.Stages[i].Status).Activity(0) == buildstatus.ActivityRunning {
			pulse = true
			break
		}
	}
	return spinner, pulse
}

func runHasArtifacts(run *gojenkins.PipelineRun) bool {
	return run != nil && run.URLs != nil && run.URLs["artifacts"] != nil
}

// syncViewportScrollback keeps scroll limits aligned with the document. Animation
// frames skip it: line count and the longest row width do not change when only
// a spinner or pulse glyph changes.
func (svm *StageViewModel) syncViewportScrollback() {
	if !svm.scrollDirty && svm.syncedCount == len(svm.lines) && svm.syncedLongest == svm.longest {
		return
	}
	clone := append([]string(nil), svm.lines...)
	y, x := svm.viewport.YOffset(), svm.viewport.XOffset()
	svm.viewport.SetContentLines(clone)
	svm.viewport.SetYOffset(y)
	svm.viewport.SetXOffset(x)
	svm.syncedCount = len(svm.lines)
	svm.syncedLongest = svm.longest
	svm.scrollDirty = false
}

func (svm *StageViewModel) useFastWindow() bool {
	if svm.viewport.SoftWrap || svm.viewport.FillHeight || svm.viewport.StyleLineFunc != nil {
		return false
	}
	if !svm.plainKnown {
		style := svm.viewport.Style
		svm.plainStyle = style.GetHorizontalFrameSize() == 0 &&
			style.GetVerticalFrameSize() == 0 &&
			style.GetWidth() == 0 &&
			style.GetHeight() == 0 &&
			style.Render("x") == "x"
		svm.plainKnown = true
	}
	return svm.plainStyle
}

func (svm *StageViewModel) renderThroughViewport() string {
	svm.scrollDirty = true
	svm.syncViewportScrollback()
	svm.window = windowCache{}
	return svm.viewport.View()
}

func (svm *StageViewModel) renderWindow() string {
	vp := svm.viewport
	w, h := vp.Width(), vp.Height()
	if sw := vp.Style.GetWidth(); sw != 0 {
		w = min(w, sw)
	}
	if sh := vp.Style.GetHeight(); sh != 0 {
		h = min(h, sh)
	}
	if w == 0 || h == 0 {
		return ""
	}
	contentW := w - vp.Style.GetHorizontalFrameSize()
	contentH := h - vp.Style.GetVerticalFrameSize()
	if contentW <= 0 || contentH <= 0 {
		return ""
	}
	x, y := vp.XOffset(), vp.YOffset()
	if !svm.window.valid || svm.window.width != contentW || svm.window.height != contentH || svm.window.x != x || svm.window.y != y || svm.window.longest != svm.longest {
		svm.window = windowCache{
			valid:   true,
			width:   contentW,
			height:  contentH,
			x:       x,
			y:       y,
			longest: svm.longest,
			lines:   make([]string, contentH),
			src:     make([]string, contentH),
			fitted:  make([]bool, contentH),
			blank:   strings.Repeat(" ", contentW),
		}
	}
	start := min(y, len(svm.lines))
	end := min(start+contentH, len(svm.lines))
	win := &svm.window
	dirty := win.text == ""
	for i := range contentH {
		if doc := start + i; doc < end {
			src := svm.lines[doc]
			if !win.fitted[i] || win.src[i] != src {
				win.lines[i] = fitStageLine(src, x, contentW, svm.longest, win.blank)
				win.src[i] = src
				win.fitted[i] = true
				dirty = true
			}
			continue
		}
		if !win.fitted[i] || win.lines[i] != win.blank {
			win.lines[i] = win.blank
			win.src[i] = ""
			win.fitted[i] = true
			dirty = true
		}
	}
	if !dirty {
		return win.text
	}
	size := 0
	for _, line := range win.lines {
		size += len(line) + 1
	}
	var b strings.Builder
	b.Grow(size)
	for i, line := range win.lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(line)
	}
	win.text = b.String()
	return win.text
}

func fitStageLine(line string, xOffset, contentWidth, longest int, blank string) string {
	if xOffset != 0 || longest > contentWidth {
		line = ansi.Cut(line, xOffset, xOffset+contentWidth)
	}
	if strings.Contains(line, "\t") {
		line = strings.ReplaceAll(line, "\t", "    ")
	}
	width := ansi.StringWidth(line)
	if width == 0 {
		return blank
	}
	if width < contentWidth {
		line += blank[:contentWidth-width]
	}
	return line
}

func (svm *StageViewModel) pruneRunCache() {
	if len(svm.stageRenderCache) == 0 {
		return
	}
	for id := range svm.stageRenderCache {
		if _, ok := svm.runIndex[id]; !ok {
			delete(svm.stageRenderCache, id)
		}
	}
}
