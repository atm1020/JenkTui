// Package block composes rendered sections whose cell sizes are already
// known. A page renders each section once, keeps it as a Block, and rebuilds
// only the sections whose inputs changed. Joining and placing Blocks gives
// the same bytes as lipgloss's JoinHorizontal, JoinVertical, and Place,
// without measuring the unchanged lines again.
package block

import (
	"math"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// Block is rendered text split into lines, with each line's cell width.
type Block struct {
	Lines  []string
	Widths []int
	// Width is the widest line.
	Width int
}

// Measure splits text into a Block the way lipgloss reads a text block: tabs
// become four spaces and CRLF becomes LF.
func Measure(text string) Block {
	text = strings.ReplaceAll(text, "\t", "    ")
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	widths := make([]int, len(lines))
	widest := 0
	for i, line := range lines {
		widths[i] = ansi.StringWidth(line)
		widest = max(widest, widths[i])
	}
	return Block{Lines: lines, Widths: widths, Width: widest}
}

func (b Block) Height() int {
	return len(b.Lines)
}

func (b Block) String() string {
	size := len(b.Lines)
	for _, line := range b.Lines {
		size += len(line)
	}
	var s strings.Builder
	s.Grow(size)
	for i, line := range b.Lines {
		if i > 0 {
			s.WriteByte('\n')
		}
		s.WriteString(line)
	}
	return s.String()
}

// JoinHorizontal matches lipgloss.JoinHorizontal.
func JoinHorizontal(pos lipgloss.Position, blocks ...Block) Block {
	if len(blocks) == 0 {
		return Block{Lines: []string{""}, Widths: []int{0}}
	}
	if len(blocks) == 1 {
		return blocks[0]
	}
	height := 0
	width := 0
	for _, b := range blocks {
		height = max(height, b.Height())
		width += b.Width
	}
	lines := make([]string, height)
	widths := make([]int, height)
	var s strings.Builder
	for row := range height {
		s.Reset()
		for _, b := range blocks {
			i := row - topGap(height-b.Height(), pos)
			lineWidth := 0
			if i >= 0 && i < b.Height() {
				s.WriteString(b.Lines[i])
				lineWidth = b.Widths[i]
			}
			s.WriteString(spaces(b.Width - lineWidth))
		}
		lines[row] = s.String()
		widths[row] = width
	}
	return Block{Lines: lines, Widths: widths, Width: width}
}

// topGap is how many of gap filler lines lipgloss.JoinHorizontal puts above a
// shorter block.
func topGap(gap int, pos lipgloss.Position) int {
	switch {
	case gap <= 0, pos == lipgloss.Top:
		return 0
	case pos == lipgloss.Bottom:
		return gap
	}
	return int(math.Round(float64(gap) * clamp(pos)))
}

// JoinVertical matches lipgloss.JoinVertical.
func JoinVertical(pos lipgloss.Position, blocks ...Block) Block {
	if len(blocks) == 0 {
		return Block{Lines: []string{""}, Widths: []int{0}}
	}
	if len(blocks) == 1 {
		return blocks[0]
	}
	width, height := 0, 0
	for _, b := range blocks {
		width = max(width, b.Width)
		height += b.Height()
	}
	lines := make([]string, 0, height)
	widths := make([]int, 0, height)
	for _, b := range blocks {
		for i, line := range b.Lines {
			lines = append(lines, padLine(line, width-b.Widths[i], pos, true))
			widths = append(widths, max(width, b.Widths[i]))
		}
	}
	return Block{Lines: lines, Widths: widths, Width: width}
}

// Place matches lipgloss.Place with default whitespace.
func Place(width, height int, hPos, vPos lipgloss.Position, b Block) Block {
	return placeVertical(height, vPos, placeHorizontal(width, hPos, b))
}

func placeHorizontal(width int, pos lipgloss.Position, b Block) Block {
	gap := width - b.Width
	if gap <= 0 {
		return b
	}
	lines := make([]string, b.Height())
	widths := make([]int, b.Height())
	for i, line := range b.Lines {
		lines[i] = padLine(line, gap+max(0, b.Width-b.Widths[i]), pos, false)
		widths[i] = width
	}
	return Block{Lines: lines, Widths: widths, Width: width}
}

func placeVertical(height int, pos lipgloss.Position, b Block) Block {
	gap := height - b.Height()
	if gap <= 0 {
		return b
	}
	top := 0
	switch {
	case pos == lipgloss.Bottom:
		top = gap
	case pos != lipgloss.Top:
		top = gap - int(math.Round(float64(gap)*clamp(pos)))
	}
	empty := spaces(b.Width)
	lines := make([]string, 0, height)
	widths := make([]int, 0, height)
	for range top {
		lines = append(lines, empty)
		widths = append(widths, b.Width)
	}
	lines = append(lines, b.Lines...)
	widths = append(widths, b.Widths...)
	for range gap - top {
		lines = append(lines, empty)
		widths = append(widths, b.Width)
	}
	return Block{Lines: lines, Widths: widths, Width: b.Width}
}

// padLine adds gap spaces around line the way lipgloss splits a horizontal
// gap for pos. JoinVertical rounds the left share and Place rounds the right
// one, so an odd centered gap leans opposite ways.
func padLine(line string, gap int, pos lipgloss.Position, roundLeft bool) string {
	if gap <= 0 {
		return line
	}
	switch pos {
	case lipgloss.Left:
		return line + spaces(gap)
	case lipgloss.Right:
		return spaces(gap) + line
	}
	rounded := int(math.Round(float64(gap) * clamp(pos)))
	left, right := gap-rounded, rounded
	if roundLeft {
		left, right = rounded, gap-rounded
	}
	return spaces(left) + line + spaces(right)
}

func clamp(pos lipgloss.Position) float64 {
	return math.Min(1, math.Max(0, float64(pos)))
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.Repeat(" ", n)
}

// Fit pads every line to width and adds blank lines up to height. For a
// Block no wider than width it matches
// lipgloss.NewStyle().Width(width).Height(height).Render, which is how a
// viewport sizes its visible lines.
func Fit(b Block, width, height int) Block {
	n := max(b.Height(), height)
	lines := make([]string, n)
	widths := make([]int, n)
	blank := spaces(width)
	for i := range n {
		if i < b.Height() {
			lines[i] = b.Lines[i] + spaces(width-b.Widths[i])
			widths[i] = max(width, b.Widths[i])
			continue
		}
		lines[i] = blank
		widths[i] = width
	}
	return Block{Lines: lines, Widths: widths, Width: max(width, b.Width)}
}

// Concat stacks blocks the way joining their strings with "\n" does: lines are
// not padded to a common width.
func Concat(blocks ...Block) Block {
	n := 0
	for _, b := range blocks {
		n += b.Height()
	}
	out := Block{Lines: make([]string, 0, n), Widths: make([]int, 0, n)}
	for _, b := range blocks {
		out.Lines = append(out.Lines, b.Lines...)
		out.Widths = append(out.Widths, b.Widths...)
		out.Width = max(out.Width, b.Width)
	}
	return out
}
