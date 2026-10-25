package block

import (
	"strings"

	"charm.land/lipgloss/v2"
)

// Chrome is the border and horizontal padding a style adds around content of
// one width. It is captured once from a probe render, then applied to each
// frame by concatenation instead of by re-rendering the style.
type Chrome struct {
	top, bottom    string
	left, right    string
	contentWidth   int
	width          int
	hasTop, hasBot bool
}

// NewChrome renders a probe of contentWidth through style and lifts off what
// lipgloss added around it. It reports false for styles it cannot reproduce
// by concatenation, such as more than one line of vertical padding.
func NewChrome(style lipgloss.Style, contentWidth int) (Chrome, bool) {
	if contentWidth <= 0 {
		return Chrome{}, false
	}
	probe := strings.Repeat("X", contentWidth)
	rendered := style.Render(probe)
	lines := strings.Split(rendered, "\n")
	at := -1
	for i, line := range lines {
		if strings.Contains(line, probe) {
			at = i
			break
		}
	}
	if at < 0 || at > 1 || len(lines)-1-at > 1 {
		return Chrome{}, false
	}
	col := strings.Index(lines[at], probe)
	c := Chrome{
		left:         lines[at][:col],
		right:        lines[at][col+len(probe):],
		contentWidth: contentWidth,
		width:        lipgloss.Width(rendered),
	}
	if at == 1 {
		c.hasTop, c.top = true, lines[0]
	}
	if at == len(lines)-2 {
		c.hasBot, c.bottom = true, lines[len(lines)-1]
	}
	return c, true
}

// Wrap frames b. Every line must already be exactly the probe width, as it is
// after a viewport window or a Place to that width; otherwise it reports false
// and the caller renders through lipgloss instead.
func (c Chrome) Wrap(b Block) (Block, bool) {
	for _, w := range b.Widths {
		if w != c.contentWidth {
			return Block{}, false
		}
	}
	n := b.Height()
	if c.hasTop {
		n++
	}
	if c.hasBot {
		n++
	}
	lines := make([]string, 0, n)
	widths := make([]int, 0, n)
	if c.hasTop {
		lines = append(lines, c.top)
		widths = append(widths, c.width)
	}
	for _, line := range b.Lines {
		lines = append(lines, c.left+line+c.right)
		widths = append(widths, c.width)
	}
	if c.hasBot {
		lines = append(lines, c.bottom)
		widths = append(widths, c.width)
	}
	return Block{Lines: lines, Widths: widths, Width: c.width}, true
}
