package build

import (
	"image/color"
	"strings"

	"github.com/atm1020/jenktui/src/ui/styles"

	"github.com/charmbracelet/x/ansi"
)

// panelFrameOpts describes an Omaci-inspired framed panel with its title,
// optional secondary note, and optional right-edge readout embedded directly
// into the top border line.
type panelFrameOpts struct {
	Title       string
	Note        string
	Right       string
	Width       int
	Height      int
	Active      bool
	BorderColor color.Color
}

// renderPanelFrame draws a single-line rounded frame around body with exact
// cell dimensions (Width × Height, or natural height when Height <= 0).
// Unstyled cells stay transparent so the terminal's OSC 11 BgBase shines through.
func renderPanelFrame(opts panelFrameOpts, body string) string {
	t := styles.GetTheme()
	width := max(4, opts.Width)
	innerW := width - 2

	borderColor := t.Border
	if opts.Active {
		borderColor = t.StageFocusedBorder
	}
	if opts.BorderColor != nil {
		borderColor = opts.BorderColor
	}

	borderStyle := t.S().Base.Foreground(borderColor)
	titleStyle := t.S().Base.Bold(true).Foreground(t.FgBase)
	if opts.Active {
		titleStyle = titleStyle.Foreground(borderColor)
	}
	noteStyle := t.S().Base.Foreground(t.SubtleText)

	topLine := renderFrameTopBorder(innerW, opts.Title, opts.Note, opts.Right, borderStyle, titleStyle, noteStyle)

	rawLines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	innerH := len(rawLines)
	if opts.Height > 0 {
		innerH = max(1, opts.Height-2)
	}

	leftBar := borderStyle.Render("│")
	rightBar := borderStyle.Render("│")
	blankInner := strings.Repeat(" ", innerW)

	lines := make([]string, 0, innerH+2)
	lines = append(lines, topLine)
	for i := range innerH {
		var line string
		if i < len(rawLines) {
			line = rawLines[i]
		}
		w := ansi.StringWidth(line)
		switch {
		case w > innerW:
			line = ansi.Truncate(line, innerW, "")
			if pad := innerW - ansi.StringWidth(line); pad > 0 {
				line += strings.Repeat(" ", pad)
			}
		case w < innerW:
			if w == 0 {
				line = blankInner
			} else {
				line += strings.Repeat(" ", innerW-w)
			}
		}
		lines = append(lines, leftBar+line+rightBar)
	}
	lines = append(lines, borderStyle.Render("╰"+strings.Repeat("─", innerW)+"╯"))
	return strings.Join(lines, "\n")
}

func renderFrameTopBorder(
	innerW int,
	title, note, right string,
	borderStyle, titleStyle, noteStyle interface{ Render(...string) string },
) string {
	var rightPart string
	rightW := 0
	if right != "" {
		// Keep at least a small title area before accepting the right readout.
		maxRight := max(0, innerW-8)
		if maxRight > 0 {
			r := right
			if ansi.StringWidth(r) > maxRight-2 {
				r = ansi.Truncate(r, max(0, maxRight-2), "…")
			}
			if ansi.StringWidth(r) > 0 {
				rightPart = " " + r + " "
				rightW = ansi.StringWidth(rightPart)
			}
		}
	}

	var leftPart string
	leftW := 0
	// Reserve 1 leading rule, at least 1 middle rule, and 1 trailing rule when right is present.
	maxLeft := innerW - rightW - 2
	if rightW > 0 {
		maxLeft--
	}
	if title != "" && maxLeft >= 3 {
		tText := ansi.Truncate(title, max(0, maxLeft-2), "…")
		leftPart = " " + titleStyle.Render(tText) + " "
		leftW = ansi.StringWidth(leftPart)

		if note != "" {
			rem := maxLeft - leftW - 3 // "─ " + note + " "
			if rem >= 3 {
				nText := ansi.Truncate(note, rem, "…")
				leftPart += borderStyle.Render("─") + " " + noteStyle.Render(nText) + " "
				leftW = ansi.StringWidth(leftPart)
			}
		}
	}

	lead := 1
	trail := 0
	if rightW > 0 {
		trail = 1
	}
	mid := innerW - lead - leftW - rightW - trail
	if mid < 0 {
		mid = 0
		trail = max(0, innerW-lead-leftW-rightW)
	}

	var b strings.Builder
	b.WriteString(borderStyle.Render("╭" + strings.Repeat("─", lead)))
	b.WriteString(leftPart)
	if mid > 0 {
		b.WriteString(borderStyle.Render(strings.Repeat("─", mid)))
	}
	b.WriteString(rightPart)
	if trail > 0 {
		b.WriteString(borderStyle.Render(strings.Repeat("─", trail)))
	}
	b.WriteString(borderStyle.Render("╮"))
	return b.String()
}
