package styles

// layout.go holds width-responsive layout primitives shared across components.
// Terminal width is the single input; components derive their sizing from a named
// tier instead of scattering magic breakpoints, so "what counts as narrow" lives in
// exactly one place. Callers pass the live render width, so a resize re-evaluates
// the tier implicitly (there is no stored tier to go stale).

// WidthTier buckets a terminal width into named responsive breakpoints.
type WidthTier int

const (
	TierTiny   WidthTier = iota // < 58: minimal chrome, smallest cells
	TierNarrow                  // 58–79: compact
	TierMedium                  // 80–99: roomy
	TierFull                    // ≥ 100: full size
)

// WidthTierFor maps a width to its tier. Non-positive widths (an unmeasured
// terminal) fall back to the full tier so first-paint uses full-size layout.
func WidthTierFor(width int) WidthTier {
	switch {
	case width <= 0:
		return TierFull
	case width >= 100:
		return TierFull
	case width >= 80:
		return TierMedium
	case width >= 58:
		return TierNarrow
	default:
		return TierTiny
	}
}

// stageBoxFullWidth is the pipeline stage box content width on wide terminals. It
// matches the theme's StageBox styles (see buildStyles) and the per-package
// StageBoxWidth constants; the responsive tiers shrink from here.
const stageBoxFullWidth = 22

// StageBoxContentWidth returns the pipeline stage box content width for a terminal
// width, shrinking from stageBoxFullWidth on wide terminals toward a legible floor
// on narrow ones so more stages fit before the horizontal row clips/scrolls.
// Callers truncate the stage name to width-4, so the floor stays above that.
func StageBoxContentWidth(width int) int {
	switch WidthTierFor(width) {
	case TierFull:
		return stageBoxFullWidth
	case TierMedium:
		return 16
	default: // narrow + tiny
		return 12
	}
}

// Centered-dialog sizing bounds. A dialog is placed by centering it in the window
// (offset = (window-dialog)/2); if it is wider/taller than the window the offset
// goes negative and the overlay is clipped, so DialogWidth/DialogHeight always keep
// the dialog inside the window minus a margin, between a readable floor and a cap so
// it neither sprawls on wide terminals nor overflows on small ones.
const (
	dialogMinWidth  = 40
	dialogMaxWidth  = 100
	dialogMinHeight = 8
	dialogMaxHeight = 30
	dialogMargin    = 4
)

func clamp(value, low, high int) int {
	if value < low {
		return low
	}
	if value > high {
		return high
	}
	return value
}

// DialogWidth returns a centered dialog's width for a given window width: about two
// thirds of the window, clamped to [dialogMinWidth, dialogMaxWidth] and never wider
// than the window minus a margin. On a window too small for the floor it returns the
// available width so the dialog still fits instead of overflowing.
func DialogWidth(windowWidth int) int {
	if windowWidth <= 0 {
		return dialogMinWidth
	}
	avail := windowWidth - dialogMargin
	if avail < dialogMinWidth {
		return max(1, avail)
	}
	return clamp(windowWidth*2/3, dialogMinWidth, min(dialogMaxWidth, avail))
}

// DialogHeight returns a centered list-dialog's height for a given window height:
// about half the window, clamped to [dialogMinHeight, dialogMaxHeight] and never
// taller than the window minus a margin.
func DialogHeight(windowHeight int) int {
	if windowHeight <= 0 {
		return dialogMinHeight
	}
	avail := windowHeight - dialogMargin
	if avail < dialogMinHeight {
		return max(1, avail)
	}
	return clamp(windowHeight/2, dialogMinHeight, min(dialogMaxHeight, avail))
}

// ClampDialogWidth fits a content-driven dialog's natural width (its own
// preferred size, e.g. window/3 or the width of its longest row) into the same
// floor/ceiling as DialogWidth, so a short confirm prompt and a long form share
// one minimum box size and neither overflows the window. Prefer this over a
// dialog inventing its own min/max so every popup reads as one size system.
func ClampDialogWidth(naturalWidth, windowWidth int) int {
	if windowWidth <= 0 {
		return max(naturalWidth, dialogMinWidth)
	}
	avail := windowWidth - dialogMargin
	if avail < dialogMinWidth {
		return max(1, avail)
	}
	return clamp(naturalWidth, dialogMinWidth, min(dialogMaxWidth, avail))
}

// ClampDialogHeight is ClampDialogWidth's height counterpart.
func ClampDialogHeight(naturalHeight, windowHeight int) int {
	if windowHeight <= 0 {
		return max(naturalHeight, dialogMinHeight)
	}
	avail := windowHeight - dialogMargin
	if avail < dialogMinHeight {
		return max(1, avail)
	}
	return clamp(naturalHeight, dialogMinHeight, min(dialogMaxHeight, avail))
}
