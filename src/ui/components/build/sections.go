package build

import (
	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/ui/block"
	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"
)

// sectionKey is what every cached section depends on. rev changes on any
// message other than an animation frame, so a section only needs to add the
// animation state it shows. rev plays the part of the version Crush's list
// items bump when they change (Version in internal/ui/list/item.go).
type sectionKey struct {
	rev           uint64
	theme         *styles.Theme
	width, height int
}

type spinnerKey struct {
	sectionKey
	spinner string
}

// pageSections caches the unified build page by section. An animation frame
// re-renders only the sections that show a spinner or pulse: the compact build
// info bar (when running), the running stage's timeline bar, the running step's
// row, and the inline log pane (only while actively loading a log). Static
// sections—including a loaded log pane—are reused verbatim.
type pageSections struct {
	rev     uint64
	info    block.Memo[spinnerKey]
	flow    block.Memo[spinnerKey]
	details detailsSections
	log     block.Memo[spinnerKey]
}

// invalidate marks every section stale. Any state change other than an
// animation frame calls it.
func (p *pageSections) invalidate() {
	p.rev++
}

// reset drops every cached section, for a page that was left.
func (p *pageSections) reset() {
	*p = pageSections{rev: p.rev + 1}
}

func (m *Model) renderSections() string {
	key := sectionKey{rev: m.sections.rev, theme: styles.GetTheme(), width: m.wWidth, height: m.wHeight}
	header := m.spinnerModel.View()
	frames := len(m.spinnerModel.Spinner.Frames)

	if m.logPane != nil && m.logPane.zoomed {
		logKey := spinnerKey{sectionKey: key}
		if m.logPane.isAnimating() {
			logKey.spinner = header
		}
		m.sections.log.Keep = frames
		logBlock := m.sections.log.Get(logKey, func() block.Block {
			return block.Measure(m.renderLogPanel(header))
		})
		return block.Place(m.wWidth, m.wHeight, lipgloss.Left, lipgloss.Top, logBlock).String()
	}

	infoKey := spinnerKey{sectionKey: key}
	if m.isRunning() {
		infoKey.spinner = header
	}
	m.sections.info.Keep = frames
	top := m.sections.info.Get(infoKey, func() block.Block {
		return block.Measure(m.renderBuildInfo(header))
	})

	flowKey := spinnerKey{sectionKey: key}
	if m.hasRunningStage() {
		flowKey.spinner = m.pulseBar.View()
	}
	m.sections.flow.Keep = m.pulseBar.Frames()
	flow := m.sections.flow.Get(flowKey, func() block.Block {
		return block.Measure(m.renderStagesPanel())
	})

	stepsW, logW := m.bottomPaneWidths()
	var bottom block.Block
	switch {
	case stepsW > 0 && logW > 0:
		stepsBlock := block.Measure("")
		if m.stageDetails != nil {
			stepsBlock = m.stageDetails.cachedBlock(&m.sections.details, key)
		}
		logBlock := m.cachedLogBlock(key, header, frames)
		bottom = block.JoinHorizontal(lipgloss.Top, stepsBlock, logBlock)
	case logW > 0:
		bottom = m.cachedLogBlock(key, header, frames)
	case stepsW > 0 && m.stageDetails != nil:
		bottom = m.stageDetails.cachedBlock(&m.sections.details, key)
	default:
		bottom = block.Measure("")
	}

	page := block.JoinVertical(lipgloss.Left, top, flow, bottom)
	return block.Place(m.wWidth, m.wHeight, lipgloss.Left, lipgloss.Top, page).String()
}

func (m *Model) cachedLogBlock(key sectionKey, header string, frames int) block.Block {
	if m.logPane == nil {
		return block.Measure("")
	}
	logKey := spinnerKey{sectionKey: key}
	if m.logPane.isAnimating() {
		logKey.spinner = header
	}
	m.sections.log.Keep = frames
	return m.sections.log.Get(logKey, func() block.Block {
		return block.Measure(m.renderLogPanel(header))
	})
}

// detailsSections caches the steps pane: its metadata header, one row per
// step, and the outer frame. Only a running step's row changes between frames.
type detailsSections struct {
	pane    block.Memo[spinnerKey]
	empty   block.Memo[sectionKey]
	header  block.Memo[sectionKey]
	missing block.Memo[sectionKey]
	steps   []block.Memo[spinnerKey]

	chromeKey sectionKey
	chromeTop string
	chromeBot string
	chromeL   string
	chromeR   string
	chromeOK  bool

	syncedKey     sectionKey
	syncedLines   int
	syncedLongest int
}

// cachedBlock draws the same framed steps pane as render from cached sections.
func (s *StageDetails) cachedBlock(c *detailsSections, page sectionKey) block.Block {
	key := sectionKey{rev: page.rev, theme: page.theme, width: s.wWidth, height: s.wHeight}
	if s.pipelineNode == nil {
		return c.empty.Get(key, func() block.Block {
			return block.Measure(s.render(s.spinnerModel.View()))
		})
	}
	c.pane.Keep = len(s.spinnerModel.Spinner.Frames)
	spinnerKeyVal := spinnerKey{sectionKey: key}
	if s.hasRunningStep() {
		spinnerKeyVal.spinner = s.spinnerModel.View()
	}
	return c.pane.Get(spinnerKeyVal, func() block.Block {
		content := s.contentBlock(c, key)
		s.syncViewport(c, key, content)
		window, ok := s.viewportWindow(c, key, content)
		if !ok {
			return block.Measure(s.render(s.spinnerModel.View()))
		}
		return window
	})
}

func (s *StageDetails) hasRunningStep() bool {
	if s.pipelineNode == nil {
		return false
	}
	for _, node := range s.pipelineNode.StageFlowNodes {
		if buildstatus.PipelineStatus(node.Status).IsExecuting() {
			return true
		}
	}
	return false
}

// contentBlock is renderNodeDetails built from cached parts.
func (s *StageDetails) contentBlock(c *detailsSections, key sectionKey) block.Block {
	header := c.header.Get(key, func() block.Block {
		return block.Measure(s.renderNodeDetailsHeader())
	})
	nodes := s.pipelineNode.StageFlowNodes
	if len(nodes) == 0 {
		iw, ih := s.innerSize()
		missing := c.missing.Get(key, func() block.Block {
			t := styles.GetTheme()
			return block.Place(iw, max(1, ih-stepHeaderLines), lipgloss.Center, lipgloss.Center,
				block.Measure(t.S().Base.Foreground(t.Grey).Italic(true).Render("Pipeline Steps not available")))
		})
		return block.JoinVertical(lipgloss.Left, header, missing)
	}
	if len(c.steps) != len(nodes) {
		c.steps = make([]block.Memo[spinnerKey], len(nodes))
		for i := range c.steps {
			c.steps[i].Keep = len(s.spinnerModel.Spinner.Frames)
		}
	}
	sp := s.spinnerModel.View()
	parts := make([]block.Block, 0, len(nodes)+1)
	parts = append(parts, header)
	for i, node := range nodes {
		stepKey := spinnerKey{sectionKey: key}
		if buildstatus.PipelineStatus(node.Status).IsExecuting() {
			stepKey.spinner = sp
		}
		parts = append(parts, c.steps[i].Get(stepKey, func() block.Block {
			return block.Measure(s.renderFlowNode(i+1, node, i == s.selectedFlowNodeIdx, sp))
		}))
	}
	return block.Concat(parts...)
}

func (s *StageDetails) syncViewport(c *detailsSections, key sectionKey, content block.Block) {
	if c.syncedKey == key && c.syncedLines == content.Height() && c.syncedLongest == content.Width {
		return
	}
	iw, ih := s.innerSize()
	s.viewport.SetWidth(iw)
	s.viewport.SetHeight(ih)
	s.viewport.Style = lipgloss.NewStyle()
	y, x := s.viewport.YOffset(), s.viewport.XOffset()
	s.viewport.SetContentLines(append([]string(nil), content.Lines...))
	s.viewport.SetYOffset(y)
	s.viewport.SetXOffset(x)
	c.syncedKey, c.syncedLines, c.syncedLongest = key, content.Height(), content.Width
}

func (s *StageDetails) viewportWindow(c *detailsSections, key sectionKey, content block.Block) (block.Block, bool) {
	vp := s.viewport
	if vp.SoftWrap || vp.FillHeight || vp.StyleLineFunc != nil || hasGutter(vp) {
		return block.Block{}, false
	}
	iw, ih := s.innerSize()
	if iw <= 0 || ih <= 0 || vp.XOffset() != 0 || content.Width > iw {
		return block.Block{}, false
	}
	if !c.chromeOK || c.chromeKey != key {
		opts := s.frameOpts()
		t := styles.GetTheme()
		borderColor := t.Border
		if opts.Active {
			borderColor = t.StageFocusedBorder
		}
		borderStyle := t.S().Base.Foreground(borderColor)
		titleStyle := t.S().Base.Bold(true).Foreground(t.FgBase)
		if opts.Active {
			titleStyle = titleStyle.Foreground(borderColor)
		}
		noteStyle := t.S().Base.Foreground(t.SubtleText)
		c.chromeTop = renderFrameTopBorder(iw, opts.Title, opts.Note, opts.Right, borderStyle, titleStyle, noteStyle)
		c.chromeBot = borderStyle.Render("╰" + repeatRule(iw) + "╯")
		c.chromeL = borderStyle.Render("│")
		c.chromeR = borderStyle.Render("│")
		c.chromeKey = key
		c.chromeOK = true
	}
	start := min(vp.YOffset(), content.Height())
	end := min(start+ih, content.Height())
	visible := block.Block{Lines: content.Lines[start:end], Widths: content.Widths[start:end], Width: iw}
	fitted := block.Fit(visible, iw, ih)

	outerW := iw + 2
	lines := make([]string, 0, ih+2)
	widths := make([]int, 0, ih+2)
	lines = append(lines, c.chromeTop)
	widths = append(widths, outerW)
	for _, line := range fitted.Lines {
		lines = append(lines, c.chromeL+line+c.chromeR)
		widths = append(widths, outerW)
	}
	lines = append(lines, c.chromeBot)
	widths = append(widths, outerW)
	return block.Block{Lines: lines, Widths: widths, Width: outerW}, true
}

func repeatRule(n int) string {
	if n <= 0 {
		return ""
	}
	buf := make([]rune, n)
	for i := range buf {
		buf[i] = '─'
	}
	return string(buf)
}

func hasGutter(vp *viewport.Model) bool {
	return vp.LeftGutterFunc != nil && vp.LeftGutterFunc(viewport.GutterContext{}) != ""
}
