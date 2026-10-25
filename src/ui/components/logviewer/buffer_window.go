package logviewer

import "charm.land/log/v2"

const (
	// MinimumHiddenLines is the trigger point for loading more content
	MinimumHiddenLines = 50

	// InitHiddenLines is the initial number of hidden lines to maintain
	InitHiddenLines = 250

	// PageSize is the default chunk size for loading
	PageSize = 100
)

// BufferWindow manages the sliding window over a file's lines.
// It tracks which range of lines is currently loaded and provides
// calculations for when to load more content.
type BufferWindow struct {
	From       int
	To         int
	TotalLines int
}

// GetRange returns current buffer range.
func (bw *BufferWindow) GetRange() (from, to int) {
	return bw.From, bw.To
}

// SetTotalLines initializes buffer for a new file.
func (bw *BufferWindow) SetTotalLines(totalLines, visibleLineCount int) {
	bw.TotalLines = totalLines
	bw.From = 0
	bw.To = min(InitHiddenLines+visibleLineCount, bw.TotalLines)
}

// IsLastChunk returns true if buffer contains the end of file.
func (bw *BufferWindow) IsLastChunk() bool {
	return bw.To >= bw.TotalLines
}

// IsFirstChunk returns true if buffer contains the start of file.
func (bw *BufferWindow) IsFirstChunk() bool {
	return bw.From == 0
}

// ShouldLoadDownFor returns true if loading is needed for the given line when scrolling down.
func (bw *BufferWindow) ShouldLoadDownFor(line int) bool {
	if bw.To >= bw.TotalLines {
		return false
	}
	hiddenLinesDown := bw.To - line
	log.Debug("ShouldLoadDownFor", "to", bw.To, "line", line, "hiddenLinesDown", hiddenLinesDown)
	return hiddenLinesDown <= MinimumHiddenLines
}

// ShouldLoadDown checks if more content should be loaded when scrolling down.
func (bw *BufferWindow) ShouldLoadDown(lastVisibleLine int) bool {
	return bw.ShouldLoadDownFor(lastVisibleLine)
}

// ShouldLoadUpFor returns true if loading is needed for the given line when scrolling up.
func (bw *BufferWindow) ShouldLoadUpFor(line int) bool {
	if bw.From == 0 {
		return false
	}
	return line-bw.From < MinimumHiddenLines
}

// ShouldLoadUp checks if more content should be loaded when scrolling up.
func (bw *BufferWindow) ShouldLoadUp() bool {
	return bw.ShouldLoadUpFor(MinimumHiddenLines)
}

// CalculateNewBufferRangeDown calculates new buffer range when the target line is below the current buffer.
func (bw *BufferWindow) CalculateNewBufferRangeDown(targetLine, visibleLineCount int) {
	oldFrom, oldTo := bw.From, bw.To
	from := max(0, targetLine-visibleLineCount-PageSize)
	to := min(targetLine+PageSize, bw.TotalLines)
	bw.From = from
	bw.To = to
	log.Debug("CalculateNewBufferRangeDown",
		"targetLine", targetLine,
		"oldFrom", oldFrom, "oldTo", oldTo,
		"newFrom", bw.From, "newTo", bw.To,
		"visibleLines", visibleLineCount,
		"totalLines", bw.TotalLines,
	)
}

// CalculateNewBufferRangeUp calculates new buffer range when the target line is above the current buffer.
func (bw *BufferWindow) CalculateNewBufferRangeUp(targetLine, visibleLineCount int) {
	oldFrom, oldTo := bw.From, bw.To
	to := min(bw.TotalLines, targetLine+visibleLineCount+PageSize)
	from := max(0, targetLine-PageSize)
	bw.From = from
	bw.To = to
	log.Debug("CalculateNewBufferRangeUp",
		"targetLine", targetLine,
		"oldFrom", oldFrom, "oldTo", oldTo,
		"newFrom", bw.From, "newTo", bw.To,
		"visibleLines", visibleLineCount,
	)
}
