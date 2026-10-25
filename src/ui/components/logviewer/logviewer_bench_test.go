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

func generateLogFile(b *testing.B, dir string, lineCount int, matchEvery int) string {
	b.Helper()
	path := filepath.Join(dir, fmt.Sprintf("test_%d.log", lineCount))

	var sb strings.Builder
	for i := range lineCount {
		if matchEvery > 0 && i%matchEvery == 0 {
			fmt.Fprintf(&sb, "[ERROR] line %d: something went wrong with the process\n", i)
		} else {
			fmt.Fprintf(&sb, "[INFO] line %d: normal log output from the application\n", i)
		}
	}
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		b.Fatal(err)
	}
	return path
}

const benchmarkLoaderBufferSize = 250

var (
	benchmarkLineCount int
	benchmarkLines     []string
)

func BenchmarkLoaderScanLines(b *testing.B) {
	tmpDir := b.TempDir()
	for _, lineCount := range []int{1_000, 100_000} {
		path := generateLogFile(b, tmpDir, lineCount, 0)
		info, err := os.Stat(path)
		if err != nil {
			b.Fatal(err)
		}
		loader := NewLoader(path)

		b.Run(fmt.Sprintf("short_lines/%d", lineCount), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(info.Size())
			for b.Loop() {
				count, err := loader.ScanLines()
				if err != nil {
					b.Fatal(err)
				}
				benchmarkLineCount = count
			}
		})
	}

	for _, lineSize := range []int{70 * 1024, 1024 * 1024} {
		path := filepath.Join(tmpDir, fmt.Sprintf("long_%d.log", lineSize))
		if err := os.WriteFile(path, []byte(strings.Repeat("x", lineSize)), 0o600); err != nil {
			b.Fatal(err)
		}
		loader := NewLoader(path)

		b.Run(fmt.Sprintf("single_line/%dKiB", lineSize/1024), func(b *testing.B) {
			b.ReportAllocs()
			b.SetBytes(int64(lineSize))
			for b.Loop() {
				count, err := loader.ScanLines()
				if err != nil {
					b.Fatal(err)
				}
				benchmarkLineCount = count
			}
		})
	}
}

func BenchmarkLoaderLoadChunk(b *testing.B) {
	const totalLines = 100_000
	path := generateLogFile(b, b.TempDir(), totalLines, 0)
	loader := NewLoader(path)

	for _, tc := range []struct {
		name     string
		from, to int
	}{
		{name: "start", from: 0, to: benchmarkLoaderBufferSize},
		{name: "middle", from: 50_000, to: 50_000 + benchmarkLoaderBufferSize},
		{name: "end", from: totalLines - benchmarkLoaderBufferSize, to: totalLines},
	} {
		b.Run(tc.name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				lines, err := loader.LoadChunk(tc.from, tc.to, nil)
				if err != nil {
					b.Fatal(err)
				}
				benchmarkLines = lines
			}
		})
	}
}

func BenchmarkLoaderLoadChunkWithStyle(b *testing.B) {
	path := generateLogFile(b, b.TempDir(), 1_000, 0)
	loader := NewLoader(path)
	style := func(line string) string { return strings.ToUpper(line) }

	b.ReportAllocs()
	for b.Loop() {
		lines, err := loader.LoadChunk(0, benchmarkLoaderBufferSize, style)
		if err != nil {
			b.Fatal(err)
		}
		benchmarkLines = lines
	}
}

func BenchmarkSearchFile(b *testing.B) {
	tmpDir := b.TempDir()

	for _, lineCount := range []int{100, 1_000, 10_000, 100_000, 2_000_000} {
		path := generateLogFile(b, tmpDir, lineCount, 100)

		b.Run(fmt.Sprintf("lines_%d", lineCount), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, err := SearchFile(path, "error")
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkHighlightMatches(b *testing.B) {
	style := lipgloss.NewStyle().Bold(true)

	lines := map[string]string{
		"short":  "[INFO] ok",
		"medium": "[ERROR] line 42: something went wrong with the process handling",
		"long":   strings.Repeat("[ERROR] critical failure in component xyz ", 10),
	}

	for name, line := range lines {
		b.Run(name+"/match", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = HighlightMatches(line, "error", style)
			}
		})

		b.Run(name+"/no_match", func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = HighlightMatches(line, "zzzzz", style)
			}
		})
	}
}

// DefaultLogStyle now renders every line, not just the "[Pipeline]" ones, so
// its per-line cost is what a chunk load pays 250 times over.
func BenchmarkDefaultLogStyle(b *testing.B) {
	styles.SwitchTheme(styles.Github)

	lines := map[string]string{
		"pipeline": "[Pipeline] { (Build)",
		"output":   "[ERROR] line 42: something went wrong with the process handling",
		"empty":    "",
	}

	for name, line := range lines {
		b.Run(name, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_ = DefaultLogStyle(line)
			}
		})
	}
}

func BenchmarkBufferWindow_ShouldLoadDown(b *testing.B) {
	bw := &BufferWindow{From: 0, To: 500, TotalLines: 10_000}

	b.ReportAllocs()
	for b.Loop() {
		_ = bw.ShouldLoadDownFor(450)
	}
}

func BenchmarkBufferWindow_CalculateNewBufferRangeDown(b *testing.B) {
	for _, total := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("total_%d", total), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				bw := &BufferWindow{From: 0, To: 500, TotalLines: total}
				bw.CalculateNewBufferRangeDown(600, 50)
			}
		})
	}
}

func BenchmarkBufferWindow_CalculateNewBufferRangeUp(b *testing.B) {
	for _, total := range []int{1_000, 10_000, 100_000} {
		b.Run(fmt.Sprintf("total_%d", total), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				bw := &BufferWindow{From: 500, To: 1000, TotalLines: total}
				bw.CalculateNewBufferRangeUp(400, 50)
			}
		})
	}
}
