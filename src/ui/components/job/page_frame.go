package job

import (
	"strings"

	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/lipgloss/v2"
)

// pageFrame remembers layout measurements and rendered sections whose inputs
// have not changed. Animation frames reuse those sections instead of repeating
// terminal-width, border, health-panel, and layout work.
type pageFrame struct {
	layoutKey    layoutKey
	layout       pageLayout
	hasLayout    bool
	layoutBuilds int

	healthKey healthKey
	health    framedSection
	hasHealth bool

	jobKey jobPanelKey
	job    framedSection
	hasJob bool

	stageContentKey stageContentKey
	stageContent    string
	hasStageContent bool

	stageChrome panelChromeKey
	stage       framedSection
	hasStage    bool

	page       string
	pageWidth  int
	pageHeight int
	hasPage    bool

	stylesTheme  string
	stylesActive ActiveView
	stylesOK     bool
	jobStyle     lipgloss.Style
	stageStyle   lipgloss.Style

	chromes      map[panelChromeKey]panelChrome
	chromeBuilds int

	hits           int
	fallbacks      int
	fallbackReason string
}

type layoutKey struct {
	width, height, healthHeight int
}

type pageLayout struct {
	jobListWidth  int
	stageWidth    int
	panelHeight   int
	jobContentW   int
	stageContentW int
}

func (l pageLayout) usable() bool {
	return l.panelHeight > 0 && l.jobContentW > 0 && l.stageContentW > 0
}

type healthKey struct {
	theme         string
	name          string
	color         string
	description   string
	score         int64
	favorite      bool
	inProgress    bool
	missingReport bool
	spinner       string
}

type jobPanelKey struct {
	theme    string
	contentW int
	contentH int
	selected bool
	revision uint64
}

type stageContentKey struct {
	theme      string
	width      int
	height     int
	revision   uint64
	inProgress bool
	spinner    string
	pulse      string
}

type panelChromeKey struct {
	theme        string
	selected     bool
	omitLeft     bool
	contentWidth int
}

// framedSection is a rendered block whose cell size is already known, so later
// frames can join it without measuring ANSI width.
type framedSection struct {
	text   string
	lines  []string
	width  int
	height int
}

// panelChrome is the border and padding lipgloss would wrap around content of
// a known width. It is captured once per style and width, then applied by
// concatenation.
type panelChrome struct {
	top, bottom   string
	left, right   string
	width         int
	verticalExtra int
	hasTop        bool
	hasBottom     bool
}

func (m *Model) renderCachedJobPage() (string, bool) {
	// Loading placeholders can be wider than the content box. The direct
	// composer keeps that overflow; animation frames are not in this state.
	if m.loading {
		return "", false
	}
	theme := styles.GetCurrentThemeType().String()
	health, healthHit := m.cachedHealth(theme)
	layout := m.frame.layoutFor(m.wWidth, m.wHeight, health.height)
	if !layout.usable() {
		m.frame.noteFallback("layout")
		m.frame.hasPage = false
		return "", false
	}

	jobStyle, stageStyle := m.frame.panelStyles(m.activeView, theme)
	job, jobHit := m.cachedJobPanel(theme, layout, jobStyle)
	if !jobHit && !m.frame.hasJob {
		m.frame.hasPage = false
		return "", false
	}
	stageRaw, stageHit := m.stageRawContent(layout, theme)
	stageKey := panelChromeKey{
		theme: theme, selected: m.activeView != JobListView, omitLeft: true, contentWidth: layout.stageContentW,
	}
	if healthHit && jobHit && stageHit && m.frame.hasPage &&
		m.frame.pageWidth == m.wWidth && m.frame.pageHeight == m.wHeight &&
		m.frame.stageChrome == stageKey {
		m.frame.hits++
		return m.frame.page, true
	}

	stage, ok := m.frame.stageSection(stageKey, stageStyle, stageRaw, layout.stageContentW, layout.panelHeight, stageHit)
	if !ok {
		m.frame.hasPage = false
		return "", false
	}
	page := composePage(m.wWidth, m.wHeight, health, job, stage)
	m.frame.page = page
	m.frame.pageWidth = m.wWidth
	m.frame.pageHeight = m.wHeight
	m.frame.hasPage = true
	m.frame.hits++
	return page, true
}

func (m *Model) cachedHealth(theme string) (framedSection, bool) {
	key := m.healthCacheKey(theme)
	if m.frame.hasHealth && m.frame.healthKey == key {
		return m.frame.health, true
	}
	section := measureSection(m.renderHealthStatus())
	m.frame.healthKey = key
	m.frame.health = section
	m.frame.hasHealth = true
	return section, false
}

func (m *Model) healthCacheKey(theme string) healthKey {
	details := m.job.GetDetails()
	key := healthKey{
		theme:      theme,
		name:       m.job.GetName(),
		color:      details.Color,
		favorite:   m.isFavorite,
		inProgress: m.isBuildInProgress,
	}
	if m.animating {
		key.spinner = m.spinnerModel.View()
	}
	if len(details.HealthReport) == 0 {
		key.missingReport = true
		return key
	}
	report := details.HealthReport[0]
	key.score = report.Score
	key.description = report.Description
	return key
}

func (m *Model) currentHealthHeight() int {
	if m.job == nil {
		return lipgloss.Height(m.renderHealthStatus())
	}
	section, _ := m.cachedHealth(styles.GetCurrentThemeType().String())
	return section.height
}

func (f *pageFrame) layoutFor(width, height, healthHeight int) pageLayout {
	key := layoutKey{width: width, height: height, healthHeight: healthHeight}
	if f.hasLayout && f.layoutKey == key {
		return f.layout
	}
	jobListWidth, stageWidth := calcWidths(width)
	f.layoutBuilds++
	f.layoutKey = key
	f.layout = pageLayout{
		jobListWidth:  jobListWidth,
		stageWidth:    stageWidth,
		panelHeight:   height - panelGutter - healthHeight,
		jobContentW:   jobListWidth - 2*panelGutter,
		stageContentW: stageWidth - 2*panelGutter,
	}
	f.hasLayout = true
	return f.layout
}

func (f *pageFrame) panelStyles(active ActiveView, theme string) (lipgloss.Style, lipgloss.Style) {
	if f.stylesOK && f.stylesTheme == theme && f.stylesActive == active {
		return f.jobStyle, f.stageStyle
	}
	border := styles.GetTheme().S()
	if active == JobListView {
		f.jobStyle = border.SelectedBorder
		f.stageStyle = border.DefaultBorder.BorderLeft(false)
	} else {
		f.jobStyle = border.DefaultBorder
		f.stageStyle = border.SelectedBorder.BorderLeft(false)
	}
	f.stylesTheme = theme
	f.stylesActive = active
	f.stylesOK = true
	return f.jobStyle, f.stageStyle
}

func (m *Model) cachedJobPanel(theme string, layout pageLayout, style lipgloss.Style) (framedSection, bool) {
	key := m.jobPanelCacheKey(theme, layout)
	if m.frame.hasJob && m.frame.jobKey == key {
		return m.frame.job, true
	}
	raw := m.jobPanelContent(layout)
	framed, ok := m.frame.frameContent(jobChromeKey(theme, m.activeView, layout.jobContentW), style, raw, layout.jobContentW, layout.panelHeight)
	if !ok {
		m.frame.hasJob = false
		return framedSection{}, false
	}
	m.frame.jobKey = key
	m.frame.job = framed
	m.frame.hasJob = true
	return framed, false
}

func jobChromeKey(theme string, active ActiveView, contentWidth int) panelChromeKey {
	return panelChromeKey{theme: theme, selected: active == JobListView, contentWidth: contentWidth}
}

func (m *Model) jobPanelCacheKey(theme string, layout pageLayout) jobPanelKey {
	key := jobPanelKey{
		theme:    theme,
		contentW: layout.jobContentW,
		contentH: layout.panelHeight,
		selected: m.activeView == JobListView,
	}
	if m.jobList != nil {
		key.revision = m.jobList.renderRevision
	}
	return key
}

func (m *Model) jobPanelContent(layout pageLayout) string {
	return m.jobList.View(layout.jobContentW, layout.panelHeight)
}

func (m *Model) stageRawContent(layout pageLayout, theme string) (string, bool) {
	key := stageContentKey{
		theme:      theme,
		width:      layout.stageContentW,
		height:     layout.panelHeight,
		revision:   m.stageView.renderRevision,
		inProgress: m.stageView.hasInProgress,
	}
	if m.stageView.hasInProgress {
		key.spinner = m.stageView.spinnerModel.View()
		key.pulse = m.stageView.pulseBar.View()
	}
	if m.frame.hasStageContent && m.frame.stageContentKey == key {
		return m.frame.stageContent, true
	}
	raw := m.stageView.View(layout.stageContentW, layout.panelHeight)
	m.frame.stageContentKey = key
	m.frame.stageContent = raw
	m.frame.hasStageContent = true
	return raw, false
}

func (f *pageFrame) stageSection(key panelChromeKey, style lipgloss.Style, raw string, contentWidth, contentHeight int, rawHit bool) (framedSection, bool) {
	if rawHit && f.hasStage && f.stageChrome == key {
		return f.stage, true
	}
	framed, ok := f.frameContent(key, style, raw, contentWidth, contentHeight)
	if !ok {
		f.hasStage = false
		return framedSection{}, false
	}
	f.stageChrome = key
	f.stage = framed
	f.hasStage = true
	return framed, true
}

func (f *pageFrame) frameContent(key panelChromeKey, style lipgloss.Style, raw string, contentWidth, contentHeight int) (framedSection, bool) {
	chrome, ok := f.chromeFor(key, style)
	if !ok {
		f.noteFallback("chrome")
		return framedSection{}, false
	}
	framed, ok := chrome.apply(raw, contentHeight)
	if !ok {
		f.noteFallback("content-size")
		return framedSection{}, false
	}
	return framed, true
}

func (f *pageFrame) chromeFor(key panelChromeKey, style lipgloss.Style) (panelChrome, bool) {
	if f.chromes == nil {
		f.chromes = make(map[panelChromeKey]panelChrome)
	}
	if chrome, ok := f.chromes[key]; ok {
		return chrome, true
	}
	chrome, ok := newPanelChrome(style, key.contentWidth)
	if !ok {
		return panelChrome{}, false
	}
	f.chromeBuilds++
	f.chromes[key] = chrome
	return chrome, true
}

func (f *pageFrame) noteFallback(reason string) {
	f.fallbacks++
	f.fallbackReason = reason
}

func measureSection(text string) framedSection {
	return framedSection{
		text:   text,
		lines:  strings.Split(text, "\n"),
		width:  lipgloss.Width(text),
		height: lipgloss.Height(text),
	}
}

// newPanelChrome renders a probe of contentWidth through style once and lifts
// off the affixes lipgloss added. Later content of that width is wrapped
// without measuring it.
func newPanelChrome(style lipgloss.Style, contentWidth int) (panelChrome, bool) {
	if contentWidth <= 0 {
		return panelChrome{}, false
	}
	probe := strings.Repeat("X", contentWidth)
	rendered := style.Render(probe)
	lines := strings.Split(rendered, "\n")
	probeLine := -1
	for i, line := range lines {
		if strings.Contains(line, probe) {
			probeLine = i
			break
		}
	}
	if probeLine < 0 {
		return panelChrome{}, false
	}
	// Vertical padding would insert extra lines around the probe. The job page
	// borders do not use it; reject anything else so the caller can fall back.
	if probeLine > 1 || len(lines)-1-probeLine > 1 {
		return panelChrome{}, false
	}
	at := strings.Index(lines[probeLine], probe)
	chrome := panelChrome{
		left:          lines[probeLine][:at],
		right:         lines[probeLine][at+len(probe):],
		width:         lipgloss.Width(rendered),
		verticalExtra: len(lines) - 1,
	}
	if probeLine == 1 {
		chrome.hasTop = true
		chrome.top = lines[0]
	}
	if probeLine == len(lines)-2 {
		chrome.hasBottom = true
		chrome.bottom = lines[len(lines)-1]
	}
	return chrome, true
}

func (c panelChrome) apply(content string, contentHeight int) (framedSection, bool) {
	lines := strings.Split(content, "\n")
	if len(lines) != contentHeight || contentHeight <= 0 {
		return framedSection{}, false
	}
	var b strings.Builder
	b.Grow(len(content) + len(lines)*(len(c.left)+len(c.right)+1) + len(c.top) + len(c.bottom) + 2)
	if c.hasTop {
		b.WriteString(c.top)
		b.WriteByte('\n')
	}
	for i, line := range lines {
		b.WriteString(c.left)
		b.WriteString(line)
		b.WriteString(c.right)
		if i < len(lines)-1 || c.hasBottom {
			b.WriteByte('\n')
		}
	}
	if c.hasBottom {
		b.WriteString(c.bottom)
	}
	text := b.String()
	framed := framedSection{
		text:   text,
		lines:  strings.Split(text, "\n"),
		width:  c.width,
		height: contentHeight + c.verticalExtra,
	}
	if len(framed.lines) != framed.height {
		return framedSection{}, false
	}
	return framed, true
}

// composePage joins the health panel, job list, and stage view the same way
// View used to: horizontal join, vertical join, then top-left placement. Line
// widths are already known, so this does not call ANSI width measurement.
func composePage(winW, winH int, health, job, stage framedSection) string {
	panelHeight := max(job.height, stage.height)
	panelWidth := job.width + stage.width
	bodyWidth := max(health.width, panelWidth)
	bodyHeight := health.height + panelHeight

	placedWidth := bodyWidth
	horizontalGap := 0
	if winW > bodyWidth {
		placedWidth = winW
		horizontalGap = winW - bodyWidth
	}
	verticalGap := max(0, winH-bodyHeight)
	totalLines := bodyHeight + verticalGap

	healthPad := spaces(bodyWidth - health.width)
	panelPad := spaces(bodyWidth - panelWidth)
	edgePad := spaces(horizontalGap)
	emptyLine := spaces(placedWidth)

	var b strings.Builder
	b.Grow(len(health.text) + len(job.text) + len(stage.text) + totalLines*(horizontalGap+1))
	lineNo := 0
	last := totalLines - 1
	for i := range health.height {
		writeComposed(&b, lineAt(health, i), healthPad, edgePad, lineNo == last)
		lineNo++
	}
	for i := range panelHeight {
		b.WriteString(lineAt(job, i))
		b.WriteString(lineAt(stage, i))
		b.WriteString(panelPad)
		b.WriteString(edgePad)
		if lineNo != last {
			b.WriteByte('\n')
		}
		lineNo++
	}
	for range verticalGap {
		b.WriteString(emptyLine)
		if lineNo != last {
			b.WriteByte('\n')
		}
		lineNo++
	}
	return b.String()
}

func writeComposed(b *strings.Builder, line, extra, edge string, last bool) {
	b.WriteString(line)
	b.WriteString(extra)
	b.WriteString(edge)
	if !last {
		b.WriteByte('\n')
	}
}

func lineAt(section framedSection, i int) string {
	if i < len(section.lines) {
		return section.lines[i]
	}
	return spaces(section.width)
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}
