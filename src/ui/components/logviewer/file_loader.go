package logviewer

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/atm1020/jenktui/src/ui/styles"
)

// StyleFunc is a function that applies styling to a line
type StyleFunc func(line string) string

const fileBufferSize = 64 * 1024

// Loader handles file loading operations
type Loader struct {
	filePath      string
	maxLineLength int
}

// NewLoader creates a new file loader
func NewLoader(filePath string) *Loader {
	return &Loader{filePath: filePath}
}

// ScanLines counts the file's lines and records the maximum line length in bytes.
func (l *Loader) ScanLines() (int, error) {
	file, err := os.Open(l.filePath)
	if err != nil {
		return 0, err
	}
	defer file.Close()

	buffer := make([]byte, fileBufferSize)
	lineCount := 0
	currentLineLength := 0
	maxLineLength := 0
	hasData := false
	var lastByte byte

	for {
		n, err := file.Read(buffer)
		if n > 0 {
			chunk := buffer[:n]
			hasData = true
			lastByte = chunk[n-1]

			for len(chunk) > 0 {
				newline := bytes.IndexByte(chunk, '\n')
				if newline < 0 {
					currentLineLength += len(chunk)
					break
				}

				currentLineLength += newline
				maxLineLength = max(maxLineLength, currentLineLength)
				lineCount++
				currentLineLength = 0
				chunk = chunk[newline+1:]
			}
		}

		if err != nil {
			if err == io.EOF {
				break
			}
			return 0, err
		}
	}

	// A final record without '\n' is still one line.
	if hasData && lastByte != '\n' {
		lineCount++
		maxLineLength = max(maxLineLength, currentLineLength)
	}

	l.maxLineLength = maxLineLength
	return lineCount, nil
}

// LoadChunk loads a chunk of lines from the file with styling
func (l *Loader) LoadChunk(from, to int, styleFn StyleFunc) ([]string, error) {
	if from < 0 || to < from {
		return nil, fmt.Errorf("invalid line range [%d, %d)", from, to)
	}

	file, err := os.Open(l.filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	lines := make([]string, to-from)
	if len(lines) == 0 {
		return lines, nil
	}

	scanner := bufio.NewScanner(file)
	// One extra byte lets Scanner observe the delimiter, or EOF for an
	// unterminated line, after buffering the longest measured line.
	maxTokenSize := max(bufio.MaxScanTokenSize, l.maxLineLength+1)
	scanner.Buffer(make([]byte, fileBufferSize), maxTokenSize)

	idx := 0
	for idx < to && scanner.Scan() {
		if idx >= from {
			line := scanner.Text()
			if styleFn != nil {
				line = styleFn(line)
			}
			lines[idx-from] = line
		}
		idx++
	}

	return lines, scanner.Err()
}

// pipelinePrefix marks the step-scaffolding lines Jenkins interleaves with the
// build's own output ("[Pipeline] { (Build)", "[Pipeline] // stage").
const pipelinePrefix = "[Pipeline]"

// DefaultLogStyle splits a log line into one of two levels of emphasis: Jenkins'
// "[Pipeline]" scaffolding is dimmed, and everything else — the build's own
// output — gets the theme foreground, which reads brighter than the terminal
// default it would otherwise inherit.
//
// Only the outer foreground is set, so any ANSI the build emitted still shows
// through; blank lines are left alone rather than carrying a pointless escape
// sequence.
func DefaultLogStyle(line string) string {
	if line == "" {
		return line
	}

	s := styles.GetTheme().S()
	if strings.HasPrefix(strings.TrimLeft(line, " \t"), pipelinePrefix) {
		return s.LogPipeline.Render(line)
	}
	return s.LogDefault.Render(line)
}
