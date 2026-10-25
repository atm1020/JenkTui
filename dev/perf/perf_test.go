package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/pprof/profile"
)

func TestNewDist(t *testing.T) {
	tests := []struct {
		in   []float64
		want Dist
	}{
		{nil, Dist{}},
		{[]float64{5}, Dist{Median: 5, Min: 5, Max: 5, N: 1}},
		{[]float64{3, 1, 2}, Dist{Median: 2, Min: 1, Max: 3, N: 3}},
		{[]float64{4, 1, 3, 2}, Dist{Median: 2.5, Min: 1, Max: 4, N: 4}},
	}
	for _, tt := range tests {
		if got := newDist(tt.in); got != tt.want {
			t.Errorf("newDist(%v) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}

const benchOutput = `goos: linux
pkg: github.com/atm1020/jenktui/src/ui/components/job
BenchmarkJobPageAnimationFrame-2   	    2122	    557998 ns/op	         1.000 updates/op	         1.000 views/op	  172755 B/op	    2137 allocs/op
BenchmarkJobPageAnimationFrame-2   	    2284	    594442 ns/op	         1.000 updates/op	         1.000 views/op	  174498 B/op	    2137 allocs/op
PASS
ok  	github.com/atm1020/jenktui/src/ui/components/job	12.464s
goos: linux
BenchmarkJobPageAnimationFrame-2   	    2193	    543683 ns/op	         1.000 updates/op	         1.000 views/op	  174785 B/op	    2137 allocs/op
`

func TestParseBench(t *testing.T) {
	name, values, err := parseBench(strings.NewReader(benchOutput))
	if err != nil {
		t.Fatal(err)
	}
	if name != "BenchmarkJobPageAnimationFrame" {
		t.Errorf("name = %q", name)
	}
	if got := newDist(values["ns/op"]); got.N != 3 || got.Median != 557998 {
		t.Errorf("ns/op = %+v", got)
	}
	if got := values["allocs/op"]; len(got) != 3 || got[0] != 2137 {
		t.Errorf("allocs/op = %v", got)
	}
}

func TestParseBenchRejectsMixedBenchmarks(t *testing.T) {
	in := "BenchmarkA-2 10 5 ns/op\nBenchmarkB-2 10 6 ns/op\n"
	if _, _, err := parseBench(strings.NewReader(in)); err == nil {
		t.Fatal("want error for mixed benchmarks")
	}
}

func pct(v float64) *float64 { return &v }

func TestVerdict(t *testing.T) {
	five := func(lo, hi float64) Dist { return Dist{Median: (lo + hi) / 2, Min: lo, Max: hi, N: 5} }
	tests := []struct {
		name          string
		before, after Dist
		delta         *float64
		want          string
	}{
		{"single run", Dist{Median: 1, Min: 1, Max: 1, N: 1}, five(0, 1), pct(-50), verdictTooFew},
		{"tiny delta", five(100, 110), five(101, 111), pct(1), verdictNegligible},
		{"faster, disjoint", five(100, 110), five(50, 60), pct(-45), verdictSeparate},
		{"slower, disjoint", five(50, 60), five(100, 110), pct(90), verdictSeparate},
		{"overlap", five(100, 110), five(90, 101), pct(-7), verdictOverlap},
		{"zero baseline", five(0, 0), five(5, 6), nil, verdictSeparate},
	}
	for _, tt := range tests {
		if got := verdict(tt.before, tt.after, tt.delta); got != tt.want {
			t.Errorf("%s: verdict = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestCompareSummariesSkipsOneSidedMetrics(t *testing.T) {
	before := &Summary{}
	before.add("cpu_renderer_ms", 0, 0, 0)
	before.add("old_only", 1)
	after := &Summary{}
	after.add("cpu_renderer_ms", 300, 310, 320)
	after.add("terminal_bytes_per_second", 3000)

	deltas := compareSummaries(before, after)
	if len(deltas) != 1 || deltas[0].Metric != "cpu_renderer_ms" {
		t.Fatalf("deltas = %+v", deltas)
	}
	if deltas[0].DeltaPct != nil {
		t.Errorf("delta from a zero median should be nil, got %v", *deltas[0].DeltaPct)
	}
}

// writeCPUProfile writes a profile where 30ms runs in Model.View (10ms of it
// inside an inlined helper), 50ms in the renderer, and 20ms elsewhere.
func writeCPUProfile(t *testing.T, path string) {
	t.Helper()
	names := []string{
		"github.com/atm1020/jenktui/src/ui/components/job.(*Model).View",
		"github.com/atm1020/jenktui/src/ui/components/job.(*Model).ViewHelper",
		"charm.land/bubbletea/v2.(*cursedRenderer).flush",
		"runtime.gcBgMarkWorker",
		"github.com/charmbracelet/x/ansi.StringWidth",
	}
	p := &profile.Profile{
		SampleType: []*profile.ValueType{{Type: "samples", Unit: "count"}, {Type: "cpu", Unit: "nanoseconds"}},
		PeriodType: &profile.ValueType{Type: "cpu", Unit: "nanoseconds"},
		Period:     10_000_000,
	}
	for i, name := range names {
		fn := &profile.Function{ID: uint64(i + 1), Name: name}
		p.Function = append(p.Function, fn)
		p.Location = append(p.Location, &profile.Location{ID: uint64(i + 1), Line: []profile.Line{{Function: fn}}})
	}
	loc := func(i int) *profile.Location { return p.Location[i] }
	// ViewHelper inlined into View: innermost line first.
	inlined := &profile.Location{ID: 99, Line: []profile.Line{{Function: p.Function[1]}, {Function: p.Function[0]}}}
	p.Location = append(p.Location, inlined)
	ms := func(n int64) []int64 { return []int64{n / 10, n * 1_000_000} }
	p.Sample = []*profile.Sample{
		{Location: []*profile.Location{loc(4), loc(0)}, Value: ms(20)}, // StringWidth <- View
		{Location: []*profile.Location{inlined}, Value: ms(10)},        // ViewHelper inlined in View
		{Location: []*profile.Location{loc(4), loc(2)}, Value: ms(50)}, // StringWidth <- renderer
		{Location: []*profile.Location{loc(3)}, Value: ms(20)},
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := p.Write(f); err != nil {
		t.Fatal(err)
	}
}

func TestProfileFocusAndFlat(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cpu.pprof")
	writeCPUProfile(t, path)
	p, err := readProfile(path)
	if err != nil {
		t.Fatal(err)
	}
	idx := valueIndex(p, "cpu")
	if idx != 1 {
		t.Fatalf("cpu value index = %d", idx)
	}
	for name, tt := range map[string]struct {
		got, want float64
	}{
		"total":    {sampleMs(p, idx, nil), 100},
		"view":     {sampleMs(p, idx, focusModelView), 30},
		"renderer": {sampleMs(p, idx, focusRenderer), 50},
	} {
		if tt.got != tt.want {
			t.Errorf("%s = %gms, want %gms", name, tt.got, tt.want)
		}
	}
	top := flatTop(p, idx, 2)
	if len(top) != 2 || top[0].Function != "github.com/charmbracelet/x/ansi.StringWidth" || top[0].Ms != 70 || top[0].Percent != 70 {
		t.Errorf("flatTop[0] = %+v", top)
	}
	if top[1].Function != "runtime.gcBgMarkWorker" {
		t.Errorf("flatTop[1] = %+v, want the 20ms gc worker (ties break by name)", top[1])
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func metricsJSON(views, writes int) string {
	return `{"views": ` + itoa(views) + `, "views_per_second": 10.5, "terminal_writes": ` + itoa(writes) +
		`, "terminal_writes_per_second": 10.4, "allocated_bytes": 100, "elapsed_seconds": 10}`
}

func itoa(n int) string { b, _ := json.Marshal(n); return string(b) }

func TestSummarizeRoundsInNumericOrder(t *testing.T) {
	dir := t.TempDir()
	for _, r := range []string{"profile-1", "profile-2", "profile-10"} {
		writeFile(t, filepath.Join(dir, r, "metrics.json"), metricsJSON(106, 104))
		writeCPUProfile(t, filepath.Join(dir, r, "cpu.pprof"))
	}
	writeFile(t, filepath.Join(dir, "bench.txt"), benchOutput)
	writeFile(t, filepath.Join(dir, environmentFile), "source: worktree\ncommit: abc\nload1_at_start: 0.5\n")

	rounds, err := profileRounds(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := filepath.Base(rounds[2]); got != "profile-10" {
		t.Errorf("last round = %s, want profile-10", got)
	}
	s, err := summarize("x", dir)
	if err != nil {
		t.Fatal(err)
	}
	view, ok := s.metric("cpu_model_view_ms")
	if !ok || view.N != 3 || view.Median != 30 {
		t.Errorf("cpu_model_view_ms = %+v", view)
	}
	if m, _ := s.metric("bench_ns_per_op"); m.N != 3 {
		t.Errorf("bench samples = %d", m.N)
	}
	if !s.SanityChecked || len(s.SanityFailures) != 0 || s.Kind != "offline" {
		t.Errorf("sanity = %v %v kind=%s", s.SanityChecked, s.SanityFailures, s.Kind)
	}
	var out bytes.Buffer
	s.writeText(&out)
	for _, want := range []string{"source: worktree\n", "cpu_model_view_ms: 30 (range 30..30, n=3)\n", "sanity: ok\n"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("summary text missing %q:\n%s", want, out.String())
		}
	}
}

func TestSummarizeLegacyLayoutAndSanity(t *testing.T) {
	dir := t.TempDir()
	// `task profile:job` layout: one round at the root, raw environment.txt.
	writeFile(t, filepath.Join(dir, "metrics.json"), metricsJSON(106, 1))
	writeFile(t, filepath.Join(dir, environmentFile), strings.Repeat("a", 40)+"\ngo version go1.26.2 linux/amd64\n")

	s, err := summarize("legacy", dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Environment["commit"] != strings.Repeat("a", 40) {
		t.Errorf("legacy commit = %q", s.Environment["commit"])
	}
	if len(s.SanityFailures) != 1 || !strings.Contains(s.SanityFailures[0], "terminal_writes=1") {
		t.Errorf("sanity failures = %v", s.SanityFailures)
	}
	if !errors.Is(s.sanityErr(), errSanity) {
		t.Error("sanityErr should wrap errSanity")
	}
}

func TestSanityToleratesPreRendererCaptures(t *testing.T) {
	// Captures before #113 have no terminal_writes key.
	if fails := sanityFailures("r", map[string]float64{"views": 106, "elapsed_seconds": 10}); len(fails) != 0 {
		t.Errorf("fails = %v", fails)
	}
	if fails := sanityFailures("r", map[string]float64{"views": 0, "elapsed_seconds": 0.1}); len(fails) != 2 {
		t.Errorf("fails = %v", fails)
	}
}

func TestParseArgsInterspersed(t *testing.T) {
	fs := newFlagSet("ab")
	var o measureOpts
	addMeasureFlags(fs, &o)
	base := fs.String("base", "", "")
	pos, err := parseArgs(fs, []string{"--rounds", "3", "name", "--base", "HEAD", "--quick"}, 1)
	if err != nil {
		t.Fatal(err)
	}
	if pos[0] != "name" || *base != "HEAD" || o.rounds != 3 || !o.quick {
		t.Errorf("pos=%v base=%q opts=%+v", pos, *base, o)
	}
	if err := o.validate(); err != nil || o.rounds != 1 || o.benchCount != 3 {
		t.Errorf("--quick should set 1 round, 3 samples: %+v %v", o, err)
	}
}

func TestParseArgsErrors(t *testing.T) {
	var ue *usageError
	if _, err := parseArgs(newFlagSet("summary"), nil, 1, "go run ./dev/perf summary x"); !errors.As(err, &ue) || len(ue.examples) != 1 {
		t.Errorf("missing positional: %v", err)
	}
	if _, err := parseArgs(newFlagSet("summary"), []string{"--nope"}, 1); !errors.As(err, &ue) {
		t.Errorf("unknown flag: %v", err)
	}
	if _, err := parseArgs(newFlagSet("summary"), []string{"x", "--help"}, 1); !errors.Is(err, errHelpShown) {
		t.Errorf("--help: %v", err)
	}
}

func TestMeasureOptsValidate(t *testing.T) {
	for _, o := range []measureOpts{
		{rounds: 0, benchCount: 1, duration: 1e9},
		{rounds: 1, benchCount: 1, duration: 1e8},
		{rounds: 1, benchCount: 1, duration: 1e9, benchOnly: true, profileOnly: true},
	} {
		var ue *usageError
		if err := o.validate(); !errors.As(err, &ue) {
			t.Errorf("%+v: want usage error, got %v", o, err)
		}
	}
}

func TestRunExitCodes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run(context.Background(), []string{"--help"}, &stdout, &stderr); code != 0 || !strings.Contains(stdout.String(), "Commands:") {
		t.Errorf("--help: code=%d out=%q", code, stdout.String())
	}
	stdout.Reset()
	if code := run(context.Background(), []string{"bogus"}, &stdout, &stderr); code != 2 || !strings.Contains(stderr.String(), "go run ./dev/perf --help") {
		t.Errorf("unknown command: code=%d err=%q", code, stderr.String())
	}
	if code := report(&stderr, errors.Join(errors.New("x"), errSanity)); code != 3 {
		t.Errorf("sanity code = %d", code)
	}
}

func TestEveryCommandHelpHasExamples(t *testing.T) {
	for name, cmd := range commands {
		if !strings.Contains(cmd.help, "\nExamples:\n  go run ./dev/perf "+name) {
			t.Errorf("%s --help lacks an Examples section starting with its own invocation", name)
		}
	}
}
