package logviewer

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createTestLogFile(t *testing.T, lineCount int) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "test.log")

	var sb strings.Builder
	for i := range lineCount {
		fmt.Fprintf(&sb, "line %d: log output\n", i+1)
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLoader_ScanLines(t *testing.T) {
	tests := []struct {
		name      string
		lineCount int
	}{
		{"empty file", 0},
		{"single line", 1},
		{"ten lines", 10},
		{"hundred lines", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var path string
			if tt.lineCount == 0 {
				dir := t.TempDir()
				path = filepath.Join(dir, "empty.log")
				if err := os.WriteFile(path, []byte(""), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				path = createTestLogFile(t, tt.lineCount)
			}

			loader := NewLoader(path)
			count, err := loader.ScanLines()
			if err != nil {
				t.Fatalf("ScanLines() error = %v", err)
			}
			if count != tt.lineCount {
				t.Errorf("ScanLines() = %d, want %d", count, tt.lineCount)
			}
		})
	}
}

func TestLoader_HandlesLongAndUnterminatedLines(t *testing.T) {
	longLine := strings.Repeat("x", 70*1024)
	path := filepath.Join(t.TempDir(), "long.log")
	if err := os.WriteFile(path, []byte(longLine+"\nlast line"), 0o600); err != nil {
		t.Fatal(err)
	}

	loader := NewLoader(path)
	count, err := loader.ScanLines()
	if err != nil {
		t.Fatalf("ScanLines() error = %v", err)
	}
	if count != 2 {
		t.Fatalf("ScanLines() = %d, want 2", count)
	}
	if loader.maxLineLength != len(longLine) {
		t.Fatalf("maxLineLength = %d, want %d", loader.maxLineLength, len(longLine))
	}
	lines, err := loader.LoadChunk(0, 2, nil)
	if err != nil {
		t.Fatalf("LoadChunk() error = %v", err)
	}
	if lines[0] != longLine || lines[1] != "last line" {
		t.Fatal("LoadChunk() did not preserve long or unterminated lines")
	}
}

func TestLoader_ScanLines_NonexistentFile(t *testing.T) {
	loader := NewLoader("/nonexistent/path/file.log")
	_, err := loader.ScanLines()
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}

func TestLoader_LoadChunk(t *testing.T) {
	path := createTestLogFile(t, 20)
	loader := NewLoader(path)

	tests := []struct {
		name      string
		from      int
		to        int
		wantCount int
		wantFirst string
		wantLast  string
	}{
		{
			name:      "load first 5 lines",
			from:      0,
			to:        5,
			wantCount: 5,
			wantFirst: "line 1: log output",
			wantLast:  "line 5: log output",
		},
		{
			name:      "load middle chunk",
			from:      5,
			to:        10,
			wantCount: 5,
			wantFirst: "line 6: log output",
			wantLast:  "line 10: log output",
		},
		{
			name:      "load last chunk",
			from:      15,
			to:        20,
			wantCount: 5,
			wantFirst: "line 16: log output",
			wantLast:  "line 20: log output",
		},
		{
			name:      "load all lines",
			from:      0,
			to:        20,
			wantCount: 20,
			wantFirst: "line 1: log output",
			wantLast:  "line 20: log output",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lines, err := loader.LoadChunk(tt.from, tt.to, nil)
			if err != nil {
				t.Fatalf("LoadChunk() error = %v", err)
			}
			if len(lines) != tt.wantCount {
				t.Fatalf("LoadChunk() returned %d lines, want %d", len(lines), tt.wantCount)
			}
			if lines[0] != tt.wantFirst {
				t.Errorf("first line = %q, want %q", lines[0], tt.wantFirst)
			}
			if lines[len(lines)-1] != tt.wantLast {
				t.Errorf("last line = %q, want %q", lines[len(lines)-1], tt.wantLast)
			}
		})
	}
}

func TestLoader_LoadChunk_WithStyleFunc(t *testing.T) {
	path := createTestLogFile(t, 5)
	loader := NewLoader(path)

	upper := func(line string) string {
		return strings.ToUpper(line)
	}

	lines, err := loader.LoadChunk(0, 5, upper)
	if err != nil {
		t.Fatal(err)
	}

	for i, line := range lines {
		if line != strings.ToUpper(line) {
			t.Errorf("line[%d] = %q, expected uppercase", i, line)
		}
	}
}

func TestLoader_LoadChunk_NonexistentFile(t *testing.T) {
	loader := NewLoader("/nonexistent/path/file.log")
	_, err := loader.LoadChunk(0, 10, nil)
	if err == nil {
		t.Error("expected error for nonexistent file, got nil")
	}
}
