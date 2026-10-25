package logviewer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/lipgloss/v2"
)

// --- SearchFile tests ---

func writeTestFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestSearchFile(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		pattern   string
		wantLines []int
		wantErr   bool
	}{
		{
			name:      "finds matching lines",
			content:   "hello world\nfoo bar\nhello again\n",
			pattern:   "hello",
			wantLines: []int{1, 3},
		},
		{
			name:      "case insensitive",
			content:   "ERROR occurred\ninfo line\nerror again\n",
			pattern:   "error",
			wantLines: []int{1, 3},
		},
		{
			name:      "no matches returns empty slice",
			content:   "line one\nline two\n",
			pattern:   "zzz",
			wantLines: []int{},
		},
		{
			name:      "empty pattern matches all lines",
			content:   "a\nb\nc",
			pattern:   "",
			wantLines: []int{1, 2, 3},
		},
		{
			name:      "single line without trailing newline",
			content:   "only line",
			pattern:   "only",
			wantLines: []int{1},
		},
		{
			name:      "empty file",
			content:   "",
			pattern:   "test",
			wantLines: []int{},
		},
		{
			// A line with the pattern multiple times must contribute one
			// entry per occurrence, not collapse into a single match — this
			// is what makes "Matches: X/Y" and n/N navigation count actual
			// occurrences instead of distinct lines.
			name:      "multiple occurrences on one line",
			content:   "no match here\nerror then error then error\nlast line\n",
			pattern:   "error",
			wantLines: []int{2, 2, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTestFile(t, tt.content)

			got, err := SearchFile(path, tt.pattern)
			if (err != nil) != tt.wantErr {
				t.Fatalf("SearchFile() error = %v, wantErr %v", err, tt.wantErr)
			}

			if len(got) != len(tt.wantLines) {
				t.Fatalf("SearchFile() returned %d matches, want %d: got %v", len(got), len(tt.wantLines), got)
			}
			for i, m := range got {
				if m.Line != tt.wantLines[i] {
					t.Errorf("match[%d].Line = %d, want %d", i, m.Line, tt.wantLines[i])
				}
			}
		})
	}
}

// Col must be the exact byte/display-column offset where each occurrence
// starts on its line — this is what lets a search jump scroll the viewport
// horizontally to the match instead of only jumping to the right line.
func TestSearchFile_MatchColumns(t *testing.T) {
	path := writeTestFile(t, "prefix error middle error suffix\n")

	got, err := SearchFile(path, "error")
	if err != nil {
		t.Fatal(err)
	}

	want := []MatchPosition{
		{Line: 1, Col: 7},
		{Line: 1, Col: 20},
	}
	if len(got) != len(want) {
		t.Fatalf("SearchFile() returned %d matches, want %d: got %v", len(got), len(want), got)
	}
	for i, m := range got {
		if m != want[i] {
			t.Errorf("match[%d] = %+v, want %+v", i, m, want[i])
		}
	}
}

func TestSearchFile_NonexistentFile(t *testing.T) {
	_, err := SearchFile("/nonexistent/path/file.log", "test")
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

// --- HighlightMatches tests ---

func TestHighlightMatches(t *testing.T) {
	// Use a simple style that wraps matches in markers we can detect
	style := lipgloss.NewStyle().Bold(true)

	tests := []struct {
		name    string
		line    string
		pattern string
	}{
		{
			name:    "empty pattern returns line unchanged",
			line:    "hello world",
			pattern: "",
		},
		{
			name:    "no match returns line unchanged",
			line:    "hello world",
			pattern: "zzz",
		},
		{
			name:    "single match",
			line:    "an ERROR occurred",
			pattern: "ERROR",
		},
		{
			name:    "multiple matches",
			line:    "error then error again",
			pattern: "error",
		},
		{
			name:    "case insensitive match",
			line:    "ERROR and error",
			pattern: "error",
		},
		{
			name:    "regex special characters in pattern",
			line:    "value is [Pipeline] test",
			pattern: "[Pipeline]",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HighlightMatches(tt.line, tt.pattern, style)

			if tt.pattern == "" {
				if result != tt.line {
					t.Errorf("empty pattern: got %q, want %q", result, tt.line)
				}
				return
			}

			if !strings.Contains(strings.ToLower(tt.line), strings.ToLower(tt.pattern)) {
				// No match expected — result should equal input
				if result != tt.line {
					t.Errorf("no match: got %q, want %q", result, tt.line)
				}
				return
			}

			// Match expected — result should differ from input (styled)
			if result == tt.line {
				t.Errorf("expected styled output to differ from input for pattern %q", tt.pattern)
			}
		})
	}
}

// --- DefaultLogStyle tests ---

func TestDefaultLogStyle(t *testing.T) {
	styles.SwitchTheme(styles.Github)

	tests := []struct {
		name        string
		line        string
		wantChanged bool
	}{
		{
			name:        "pipeline line gets styled",
			line:        "[Pipeline] stage 'Build'",
			wantChanged: true,
		},
		{
			name:        "indented pipeline line gets styled",
			line:        "   [Pipeline] // stage",
			wantChanged: true,
		},
		{
			// Build output that merely mentions the marker is the build's own
			// text, not Jenkins scaffolding — only the prefix counts. It still
			// gets the ordinary foreground, just not the dimmed one.
			name:        "pipeline mentioned mid-line is not dimmed",
			line:        "[INFO] parsing [Pipeline] section",
			wantChanged: true,
		},
		{
			name:        "normal line gets the theme foreground",
			line:        "[INFO] building project",
			wantChanged: true,
		},
		{
			name:        "empty line unchanged",
			line:        "",
			wantChanged: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DefaultLogStyle(tt.line)
			changed := result != tt.line
			if changed != tt.wantChanged {
				t.Errorf("DefaultLogStyle(%q) changed=%v, want changed=%v", tt.line, changed, tt.wantChanged)
			}
		})
	}
}

// The whole point of the two levels is that they don't look the same: the
// scaffolding must render dimmer than the build's own output.
func TestDefaultLogStyle_PipelineDiffersFromOutput(t *testing.T) {
	styles.SwitchTheme(styles.Github)

	const text = "same text either way"
	pipeline := DefaultLogStyle("[Pipeline] " + text)
	output := DefaultLogStyle("[INFO] " + text)

	pipelineSGR, _, _ := strings.Cut(pipeline, "[Pipeline]")
	outputSGR, _, _ := strings.Cut(output, "[INFO]")
	if pipelineSGR == outputSGR {
		t.Errorf("pipeline and output lines share the same styling %q", pipelineSGR)
	}
	if outputSGR == "" {
		t.Error("expected ordinary log lines to carry the theme foreground")
	}
}

// --- SearchFile with large content ---

func TestSearchFile_LargeFile(t *testing.T) {
	var sb strings.Builder
	matchCount := 0
	for i := range 1000 {
		if i%100 == 0 {
			fmt.Fprintf(&sb, "[ERROR] line %d\n", i)
			matchCount++
		} else {
			fmt.Fprintf(&sb, "[INFO] line %d\n", i)
		}
	}

	path := writeTestFile(t, sb.String())
	got, err := SearchFile(path, "error")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != matchCount {
		t.Errorf("got %d matches, want %d", len(got), matchCount)
	}
}
