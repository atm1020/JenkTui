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

// pageSections caches the build page by section. An animation frame
// re-renders only the sections that show a spinner or pulse: the build info
// header, the running stage's box, and the running step's box. Everything else is reused and joined by known
// widths.
//
// This is Crush's model of caching each chat item and re-rendering only the
// ones whose version changed. It replaced a cache that rendered the page with
// placeholder glyphs and spliced spinners in. That cache fell back to a full
// render whenever page text or a style touched a placeholder, and every new
// animated page would have needed its own placeholders.
type pageSections struct {
	rev       uint64
	info      block.Memo[spinnerKey]
	top       block.Memo[spinnerKey]
	artifacts block.Memo[sectionKey]
	flow      block.Memo[spinnerKey]
	details   detailsSections
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
	infoKey := spinnerKey{sectionKey: key}
	if m.isRunning() {
		infoKey.spinner = header
	}
	frames := len(m.spinnerModel.Spinner.Frames)
	m.sections.info.Keep, m.sections.top.Keep = frames, frames
	top := m.sections.top.Get(infoKey, func() block.Block {
		info := m.sections.info.Get(infoKey, func() block.Block {
			return block.Measure(m.renderBuildInfo(header))
		})
		artifacts := m.sections.artifacts.Get(key, func() block.Block {
			return block.Measure(m.renderArtifacts())
		})
		return block.JoinHorizontal(lipgloss.Center, info, artifacts)
	})
	flowKey := spinnerKey{sectionKey: key}
	if m.hasRunningStage() {
		flowKey.spinner = m.pulseBar.View()
	}
	m.sections.flow.Keep = m.pulseBar.Frames()
	flow := m.sections.flow.Get(flowKey, func() block.Block {
		m.updateStageViewLayout()
		return block.Measure(m.stageView.View())
	})
	details := block.Measure("")
	if m.stageDetails != nil {
		details = m.stageDetails.cachedBlock(&m.sections.details, key)
	}
	page := block.JoinVertical(lipgloss.Left, top, flow, details)
	return block.Place(m.wWidth, m.wHeight, lipgloss.Center, lipgloss.Top, page).String()
}

// detailsSections caches the stage details pane: its header, one box per
// step, and the border. Only a running step's box changes between frames.
type detailsSections struct {
	pane    block.Memo[spinnerKey]
	empty   block.Memo[sectionKey]
	header  block.Memo[sectionKey]
	missing block.Memo[sectionKey]
	steps   []block.Memo[spinnerKey]

	chromeKey sectionKey
	chrome    block.Chrome
	chromeOK  bool

	syncedKey     sectionKey
	syncedLines   int
	syncedLongest int
}

// cachedBlock draws the same pane as render from cached sections. A viewport
// or border it cannot reproduce falls back to render.
func (s *StageDetails) cachedBlock(c *detailsSections, page sectionKey) block.Block {
	key := sectionKey{rev: page.rev, theme: page.theme, width: s.wWidth, height: s.wHeight}
	if s.pipelineNode == nil {
		return c.empty.Get(key, func() block.Block {
			return block.Measure(lipgloss.Place(s.wWidth, s.wHeight, lipgloss.Center, lipgloss.Center, ""))
		})
	}
	// Keyed by the step spinner too, so a running step's frames are each
	// built once. The viewport only moves on a key press, which changes rev.
	c.pane.Keep = len(s.spinnerModel.Spinner.Frames)
	return c.pane.Get(spinnerKey{sectionKey: key, spinner: s.spinnerModel.View()}, func() block.Block {
		content := s.contentBlock(c, key)
		s.syncViewport(c, key, content)
		window, ok := s.viewportWindow(c, key, content)
		if !ok {
			return block.Measure(s.View())
		}
		return block.Place(s.wWidth, s.wHeight, lipgloss.Left, lipgloss.Top, window)
	})
}

// contentBlock is renderNodeDetails built from cached parts.
func (s *StageDetails) contentBlock(c *detailsSections, key sectionKey) block.Block {
	header := c.header.Get(key, func() block.Block {
		return block.Measure(s.renderNodeDetailsHeader())
	})
	nodes := s.pipelineNode.StageFlowNodes
	if len(nodes) == 0 {
		missing := c.missing.Get(key, func() block.Block {
			t := styles.GetTheme()
			return block.Place(s.wWidth, s.wHeight-header.Height(), lipgloss.Center, lipgloss.Center,
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
	spinner := s.spinnerModel.View()
	parts := make([]block.Block, 0, len(nodes)+1)
	for i, node := range nodes {
		stepKey := spinnerKey{sectionKey: key}
		if buildstatus.PipelineStatus(node.Status).IsExecuting() {
			stepKey.spinner = spinner
		}
		parts = append(parts, c.steps[i].Get(stepKey, func() block.Block {
			return block.Measure(s.renderFlowNode(i+1, node, i == s.selectedFlowNodeIdx, spinner))
		}))
	}
	// renderNodeDetails ends every box with a newline.
	parts = append(parts, block.Measure(""))
	return block.JoinVertical(lipgloss.Left, header, block.Concat(parts...))
}

// syncViewport keeps the viewport's scroll limits in step with the content.
// A frame that only moves a spinner keeps the line count and width, so it
// skips this.
func (s *StageDetails) syncViewport(c *detailsSections, key sectionKey, content block.Block) {
	if c.syncedKey == key && c.syncedLines == content.Height() && c.syncedLongest == content.Width {
		return
	}
	y, x := s.viewport.YOffset(), s.viewport.XOffset()
	s.viewport.SetContentLines(append([]string(nil), content.Lines...))
	s.viewport.SetYOffset(y)
	s.viewport.SetXOffset(x)
	s.viewport.Style = s.borderStyle()
	c.syncedKey, c.syncedLines, c.syncedLongest = key, content.Height(), content.Width
}

// viewportWindow is viewport.View for content: the visible lines, sized to
// the viewport, inside its border.
func (s *StageDetails) viewportWindow(c *detailsSections, key sectionKey, content block.Block) (block.Block, bool) {
	vp := s.viewport
	if vp.SoftWrap || vp.FillHeight || vp.StyleLineFunc != nil || hasGutter(vp) {
		return block.Block{}, false
	}
	w, h := vp.Width(), vp.Height()
	if sw := vp.Style.GetWidth(); sw != 0 {
		w = min(w, sw)
	}
	if w == 0 || h == 0 || w != vp.Width() || vp.Style.GetHeight() != 0 {
		return block.Block{}, false
	}
	contentW := w - vp.Style.GetHorizontalFrameSize()
	contentH := h - vp.Style.GetVerticalFrameSize()
	if contentW <= 0 || contentH <= 0 || vp.XOffset() != 0 || content.Width > contentW {
		return block.Block{}, false
	}
	if !c.chromeOK || c.chromeKey != key {
		c.chrome, c.chromeOK = block.NewChrome(vp.Style.UnsetWidth().UnsetHeight(), contentW)
		c.chromeKey = key
	}
	if !c.chromeOK {
		return block.Block{}, false
	}
	start := min(vp.YOffset(), content.Height())
	end := min(start+contentH, content.Height())
	visible := block.Block{Lines: content.Lines[start:end], Widths: content.Widths[start:end], Width: contentW}
	return c.chrome.Wrap(block.Fit(visible, contentW, contentH))
}

// hasGutter reports a gutter that draws something. The default NoGutter is
// set but empty.
func hasGutter(vp *viewport.Model) bool {
	return vp.LeftGutterFunc != nil && vp.LeftGutterFunc(viewport.GutterContext{}) != ""
}

// borderStyle is the pane border render sets on the viewport.
func (s *StageDetails) borderStyle() lipgloss.Style {
	t := styles.GetTheme()
	style := t.S().SelectedBorder.
		BorderTop(false).
		BorderBottom(true).
		BorderLeft(false).
		BorderRight(false).
		Width(s.wWidth)
	if s.isFocused {
		style = style.BorderForeground(t.StageFocusedBorder)
	}
	return style
}
