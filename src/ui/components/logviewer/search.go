package logviewer

import (
	"bufio"
	"io"
	"os"
	"regexp"

	"charm.land/lipgloss/v2"
)

// MatchPosition identifies one search-match occurrence: the 1-based line it
// was found on, and the column (byte offset, which equals display column for
// the plain-ASCII file content this is computed from) where it starts on
// that line — used to scroll horizontally into view when jumping to it.
type MatchPosition struct {
	Line int
	Col  int
}

// SearchFile searches for pattern in file and returns every match
// occurrence. A line containing the pattern N times contributes N entries
// (not one), so the match count and n/N navigation reflect actual
// occurrences rather than collapsing a multi-match line into a single stop.
// An empty pattern matches every line once, at column 0.
func SearchFile(filename, pattern string) ([]MatchPosition, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var re *regexp.Regexp
	if pattern != "" {
		re = regexp.MustCompile(`(?i)` + regexp.QuoteMeta(pattern))
	}

	reader := bufio.NewReaderSize(file, 256*1024) // 256KB buffer
	lineNum := 0
	matches := []MatchPosition{}

	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return nil, err
		}

		lineNum++

		if pattern == "" {
			matches = append(matches, MatchPosition{Line: lineNum, Col: 0})
		} else {
			for _, loc := range re.FindAllStringIndex(line, -1) {
				matches = append(matches, MatchPosition{Line: lineNum, Col: loc[0]})
			}
		}

		if err == io.EOF {
			break
		}
	}

	return matches, nil
}

// HighlightMatches highlights all occurrences of pattern in line
func HighlightMatches(line, pattern string, style lipgloss.Style) string {
	if pattern == "" {
		return line
	}

	re := regexp.MustCompile(`(?i)` + regexp.QuoteMeta(pattern))
	return re.ReplaceAllStringFunc(line, func(match string) string {
		return style.Render(match)
	})
}
