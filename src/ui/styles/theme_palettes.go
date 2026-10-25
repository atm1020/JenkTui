package styles

import (
	"fmt"
	"image/color"
	"math"
	"strconv"
	"strings"

	colortheme "github.com/atm1020/jenktui/src/theme"
)

// palette is the semantic token table consumed by buildTheme. Theme files stay
// deliberately compact: paletteFromDefinition derives borders, selection
// surfaces, gutters, and stage colors from seven contrast-checked source roles.
type palette struct {
	// Surfaces and text.
	bg        color.Color
	fg        color.Color
	primary   color.Color
	secondary color.Color
	accent    color.Color
	running   color.Color
	onAccent  color.Color

	// Borders and selection.
	primaryBorder   color.Color
	secondaryBorder color.Color
	faintBorder     color.Color
	border          color.Color
	selectedBg      color.Color

	// Signals.
	success color.Color
	error   color.Color
	warning color.Color
	brown   color.Color
	yellow  color.Color
	grey    color.Color

	// Standalone surfaces.
	highlightBg     color.Color
	searchCurrentBg color.Color
	footerBg        color.Color
	footerDivider   color.Color

	// Stage boxes. Their fills retain alpha for semantic consumers even though
	// the standard stage styles keep the terminal canvas transparent.
	stageActiveBg      color.Color
	stageNotExecutedBg color.Color
	stageNotExecBorder color.Color

	// Gutter.
	gutterCurrentBg color.Color
	gutterSeparator color.Color
}

// buildTheme resolves a palette into the complete Theme read by renderers.
func buildTheme(p palette) *Theme {
	return &Theme{
		// Base.
		BgBase:    p.bg,
		FgBase:    p.fg,
		Primary:   p.primary,
		Secondary: p.secondary,

		// Borders and surfaces.
		PrimaryBorder:      p.primaryBorder,
		SecondaryBorder:    p.secondaryBorder,
		FaintBorder:        p.faintBorder,
		SelectedBackground: p.selectedBg,

		// Highlights.
		HighlightForeground: p.onAccent,
		HighlightBackground: p.highlightBg,

		// Status colors.
		Success:  p.success,
		Unstable: p.warning,
		Aborted:  p.grey,
		Running:  p.running,

		SearchHighlightBackground:        p.yellow,
		SearchHighlightForeground:        p.bg,
		SearchCurrentHighlightBackground: p.searchCurrentBg,
		SearchCurrentHighlightForeground: p.fg,

		Error:   p.error,
		Warning: p.warning,
		Info:    p.accent,
		Link:    p.primary,
		Brown:   p.brown,
		Yellow:  p.yellow,
		Grey:    p.grey,

		HeaderBackground: p.primary,
		HeaderForeground: p.onAccent,

		FooterBackground: p.footerBg,
		FooterForeground: p.fg,

		FooterPageBg: p.primary,
		FooterPageFg: p.primary,

		FooterServerBg: p.primaryBorder,
		FooterServerFg: p.fg,

		FooterDivider:         p.footerDivider,
		FooterServerConnected: p.success,

		JenkinsRed:  p.error,
		JenkinsBlue: p.accent,

		// Stage backgrounds.
		StagePending:     p.faintBorder,
		StageRunning:     p.stageActiveBg,
		StageSuccess:     p.stageActiveBg,
		StageFailed:      p.stageActiveBg,
		StageAborted:     p.faintBorder,
		StageNotExecuted: p.stageNotExecutedBg,

		// Stage borders.
		StagePendingBorder:     p.grey,
		StageRunningBorder:     p.running,
		StageSuccessBorder:     p.success,
		StageFailedBorder:      p.error,
		StageAbortedBorder:     p.grey,
		StageNotExecutedBorder: p.stageNotExecBorder,
		StageFocusedBorder:     p.warning,

		// Gutter.
		GutterLineNumber:  p.grey,
		GutterCurrentLine: p.onAccent,
		GutterCurrentBg:   p.gutterCurrentBg,
		GutterVisualLine:  p.onAccent,
		GutterVisualBg:    p.gutterCurrentBg,
		GutterSeparator:   p.gutterSeparator,

		// Spinner.
		SpinnerColor: p.success,

		// Build info.
		BuildNumberBg: p.primary,
		BuildNumberFg: p.onAccent,
		BuildDateBg:   p.primaryBorder,
		BuildDateFg:   p.fg,

		// Legacy aliases retained for existing call sites.
		Background: p.bg,
		Foreground: p.fg,
		AccentBlue: p.accent,
		Highlight:  p.primary,
		Failure:    p.error,
		Border:     p.border,
		SubtleText: p.grey,
	}
}

func paletteFromDefinition(definition colortheme.Definition) (palette, error) {
	if err := definition.Validate(); err != nil {
		return palette{}, err
	}

	bg, err := parseHexColor(definition.BG)
	if err != nil {
		return palette{}, err
	}
	accent, err := parseHexColor(definition.Accent)
	if err != nil {
		return palette{}, err
	}
	brightFG, err := parseHexColor(definition.BrightFG)
	if err != nil {
		return palette{}, err
	}
	mutedFG, err := parseHexColor(definition.FG)
	if err != nil {
		return palette{}, err
	}
	success, err := parseHexColor(definition.Green)
	if err != nil {
		return palette{}, err
	}
	warning, err := parseHexColor(definition.Yellow)
	if err != nil {
		return palette{}, err
	}
	failure, err := parseHexColor(definition.Red)
	if err != nil {
		return palette{}, err
	}

	border := mixColor(bg, mutedFG, 0.35)
	secondaryBorder := mixColor(bg, mutedFG, 0.50)
	faintBorder := mixColor(bg, mutedFG, 0.18)

	return palette{
		bg:        bg,
		fg:        brightFG,
		primary:   accent,
		secondary: mutedFG,
		accent:    accent,
		running:   accent,
		onAccent:  bg,

		primaryBorder:   border,
		secondaryBorder: secondaryBorder,
		faintBorder:     faintBorder,
		border:          border,
		selectedBg:      mixColor(bg, accent, 0.18),

		success: success,
		error:   failure,
		warning: warning,
		brown:   warning,
		yellow:  warning,
		grey:    mutedFG,

		highlightBg:     warning,
		searchCurrentBg: mixColor(bg, warning, 0.45),
		footerBg:        mixColor(bg, brightFG, 0.04),
		footerDivider:   border,

		stageActiveBg:      withAlpha(bg, 200),
		stageNotExecutedBg: withAlpha(bg, 100),
		stageNotExecBorder: border,

		gutterCurrentBg: accent,
		gutterSeparator: border,
	}, nil
}

func parseHexColor(value string) (color.RGBA, error) {
	if len(value) != 7 || value[0] != '#' {
		return color.RGBA{}, fmt.Errorf("invalid color %q", value)
	}
	parsed, err := strconv.ParseUint(strings.TrimPrefix(value, "#"), 16, 24)
	if err != nil {
		return color.RGBA{}, fmt.Errorf("invalid color %q: %w", value, err)
	}
	return color.RGBA{
		R: uint8(parsed >> 16),
		G: uint8((parsed >> 8) & 0xff),
		B: uint8(parsed & 0xff),
		A: 255,
	}, nil
}

func mixColor(from, to color.RGBA, amount float64) color.RGBA {
	mix := func(a, b uint8) uint8 {
		return uint8(math.Round(float64(a) + (float64(b)-float64(a))*amount))
	}
	return color.RGBA{
		R: mix(from.R, to.R),
		G: mix(from.G, to.G),
		B: mix(from.B, to.B),
		A: 255,
	}
}

func withAlpha(value color.RGBA, alpha uint8) color.RGBA {
	value.A = alpha
	return value
}
