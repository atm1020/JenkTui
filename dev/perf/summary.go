package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/google/pprof/profile"
)

// Focus filters, equivalent to `go tool pprof -focus` with these patterns.
var (
	focusModelView = regexp.MustCompile(`components/((job|build)\.\(\*Model\)|dialogs/serversetup\.\(\*Dialog\))\.View$`)
	focusRenderer  = regexp.MustCompile(`cursedRenderer`)
)

// profileMetricKeys are read from each live-profile round's metrics.json.
// terminal_* exist only from #113 on; absent keys are skipped.
var profileMetricKeys = []string{
	"views_per_second", "terminal_writes_per_second", "terminal_bytes_per_second",
	"allocated_bytes", "allocations", "goroutines_active",
}

// Dist summarizes repeated runs of one metric.
type Dist struct {
	Median float64 `json:"median"`
	Min    float64 `json:"min"`
	Max    float64 `json:"max"`
	N      int     `json:"n"`
}

func newDist(values []float64) Dist {
	if len(values) == 0 {
		return Dist{}
	}
	v := append([]float64(nil), values...)
	sort.Float64s(v)
	mid := len(v) / 2
	median := v[mid]
	if len(v)%2 == 0 {
		median = (v[mid-1] + v[mid]) / 2
	}
	return Dist{Median: median, Min: v[0], Max: v[len(v)-1], N: len(v)}
}

type Metric struct {
	Name string `json:"name"`
	Dist
}

type FuncTime struct {
	Function string  `json:"function"`
	Ms       float64 `json:"ms"`
	Percent  float64 `json:"percent"`
}

// Summary is everything perf reports about one capture.
type Summary struct {
	Label       string            `json:"label"`
	Kind        string            `json:"kind"` // offline, live, or empty
	Benchmark   string            `json:"benchmark,omitempty"`
	Environment map[string]string `json:"environment"`
	Metrics     []Metric          `json:"metrics"`
	HotFlat     []FuncTime        `json:"hot_flat,omitempty"`
	// SanityChecked is false for live and benchmark-only captures.
	SanityChecked  bool     `json:"sanity_checked"`
	SanityFailures []string `json:"sanity_failures,omitempty"`
}

func (s *Summary) metric(name string) (Metric, bool) {
	for _, m := range s.Metrics {
		if m.Name == name {
			return m, true
		}
	}
	return Metric{}, false
}

func (s *Summary) add(name string, values ...float64) {
	if len(values) > 0 {
		s.Metrics = append(s.Metrics, Metric{Name: name, Dist: newDist(values)})
	}
}

func (a *app) loadSummary(label string) (*Summary, error) {
	dir := filepath.Join(a.root, label)
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, usageErr(fmt.Sprintf("no capture %q in %s", label, a.root),
			"go run ./dev/perf list", "go run ./dev/perf capture "+label)
	}
	return summarize(label, dir)
}

func summarize(label, dir string) (*Summary, error) {
	s := &Summary{Label: label, Kind: "empty", Environment: readEnvironment(dir)}
	if err := s.addBench(filepath.Join(dir, "bench.txt")); err != nil {
		return nil, err
	}
	rounds, err := profileRounds(dir)
	if err != nil {
		return nil, err
	}
	if len(rounds) > 0 {
		s.Kind = "offline"
		if err := s.addProfileRounds(rounds); err != nil {
			return nil, err
		}
	} else if s.Benchmark != "" {
		s.Kind = "offline"
	}
	if _, err := os.Stat(filepath.Join(dir, "live", "cpu.pprof")); err == nil {
		s.Kind = "live"
		if err := s.addLive(filepath.Join(dir, "live")); err != nil {
			return nil, err
		}
	}
	return s, nil
}

// addBench reads go test -bench output. Lines look like:
// BenchmarkX-2  2122  557998 ns/op  1.000 updates/op  172755 B/op  2137 allocs/op
func (s *Summary) addBench(path string) error {
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	defer f.Close()
	name, values, err := parseBench(f)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	s.Benchmark = name
	s.add("bench_ns_per_op", values["ns/op"]...)
	s.add("bench_bytes_per_op", values["B/op"]...)
	s.add("bench_allocs_per_op", values["allocs/op"]...)
	return nil
}

func parseBench(r io.Reader) (string, map[string][]float64, error) {
	values := map[string][]float64{}
	var name string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 || !strings.HasPrefix(fields[0], "Benchmark") {
			continue
		}
		if _, err := strconv.Atoi(fields[1]); err != nil {
			continue
		}
		base, _, _ := strings.Cut(fields[0], "-") // strip -GOMAXPROCS
		if name != "" && base != name {
			return "", nil, fmt.Errorf("mixed benchmarks %s and %s", name, base)
		}
		name = base
		for i := 2; i+1 < len(fields); i += 2 {
			v, err := strconv.ParseFloat(fields[i], 64)
			if err != nil {
				return "", nil, fmt.Errorf("bad value %q in %q", fields[i], sc.Text())
			}
			values[fields[i+1]] = append(values[fields[i+1]], v)
		}
	}
	return name, values, sc.Err()
}

// profileRounds lists live-profile round directories. Captures made by
// `task profile:job` keep a single round at the capture root.
func profileRounds(dir string) ([]string, error) {
	rounds, err := filepath.Glob(filepath.Join(dir, "profile-*"))
	if err != nil {
		return nil, err
	}
	sort.Slice(rounds, func(i, j int) bool { return roundNumber(rounds[i]) < roundNumber(rounds[j]) })
	var withMetrics []string
	for _, r := range rounds {
		if _, err := os.Stat(filepath.Join(r, "metrics.json")); err == nil {
			withMetrics = append(withMetrics, r)
		}
	}
	if len(withMetrics) == 0 {
		if _, err := os.Stat(filepath.Join(dir, "metrics.json")); err == nil {
			withMetrics = []string{dir}
		}
	}
	return withMetrics, nil
}

func roundNumber(dir string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(filepath.Base(dir), "profile-"))
	return n
}

func (s *Summary) addProfileRounds(rounds []string) error {
	metrics := make([]map[string]float64, 0, len(rounds))
	for _, r := range rounds {
		m, err := readMetrics(filepath.Join(r, "metrics.json"))
		if err != nil {
			return err
		}
		metrics = append(metrics, m)
	}
	for _, key := range profileMetricKeys {
		var vals []float64
		for _, m := range metrics {
			if v, ok := m[key]; ok {
				vals = append(vals, v)
			}
		}
		s.add(key, vals...)
	}

	var total, view, render []float64
	for _, r := range rounds {
		p, err := readProfile(filepath.Join(r, "cpu.pprof"))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		idx := valueIndex(p, "cpu")
		total = append(total, sampleMs(p, idx, nil))
		view = append(view, sampleMs(p, idx, focusModelView))
		render = append(render, sampleMs(p, idx, focusRenderer))
		if s.HotFlat == nil {
			s.HotFlat = flatTop(p, idx, 5)
		}
	}
	s.add("cpu_sampled_ms", total...)
	s.add("cpu_model_view_ms", view...)
	s.add("cpu_renderer_ms", render...)

	s.SanityChecked = true
	for i, m := range metrics {
		s.SanityFailures = append(s.SanityFailures, sanityFailures(rounds[i], m)...)
	}
	return nil
}

// sanityFailures reports a round whose work did not happen: no page views,
// no frames reaching the terminal sink (#113+), or a run that ended early.
func sanityFailures(round string, m map[string]float64) []string {
	var fails []string
	if m["views"] <= 1 {
		fails = append(fails, fmt.Sprintf("%s: views=%g", round, m["views"]))
	}
	if w, ok := m["terminal_writes"]; ok && w <= 1 {
		fails = append(fails, fmt.Sprintf("%s: terminal_writes=%g", round, w))
	}
	if m["elapsed_seconds"] < 0.5 {
		fails = append(fails, fmt.Sprintf("%s: elapsed_seconds=%g", round, m["elapsed_seconds"]))
	}
	return fails
}

func readMetrics(path string) (map[string]float64, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := map[string]float64{}
	for k, v := range raw {
		if f, ok := v.(float64); ok {
			out[k] = f
		}
	}
	return out, nil
}

func (s *Summary) addLive(dir string) error {
	cpu, err := readProfile(filepath.Join(dir, "cpu.pprof"))
	if err != nil {
		return err
	}
	idx := valueIndex(cpu, "cpu")
	s.add("live_cpu_sampled_ms", sampleMs(cpu, idx, nil))
	s.add("live_cpu_model_view_ms", sampleMs(cpu, idx, focusModelView))
	s.add("live_cpu_renderer_ms", sampleMs(cpu, idx, focusRenderer))
	s.HotFlat = flatTop(cpu, idx, 5)
	if heap, err := readProfile(filepath.Join(dir, "heap.pprof")); err == nil {
		s.add("live_heap_inuse_bytes", sampleSum(heap, valueIndex(heap, "inuse_space"), nil))
	}
	if g, err := readProfile(filepath.Join(dir, "goroutine.pprof")); err == nil {
		s.add("live_goroutines", sampleSum(g, valueIndex(g, "goroutine"), nil))
	}
	return nil
}

func readProfile(path string) (*profile.Profile, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	p, err := profile.Parse(f)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return p, nil
}

// valueIndex picks the sample value named typ, falling back to the last one
// (pprof's default sample index).
func valueIndex(p *profile.Profile, typ string) int {
	for i, st := range p.SampleType {
		if st.Type == typ {
			return i
		}
	}
	return len(p.SampleType) - 1
}

// sampleSum totals samples whose stack passes through a function matching
// focus (all samples when focus is nil), like `pprof -focus`.
func sampleSum(p *profile.Profile, idx int, focus *regexp.Regexp) float64 {
	var total int64
	for _, sample := range p.Sample {
		if focus == nil || stackMatches(sample, focus) {
			total += sample.Value[idx]
		}
	}
	return float64(total)
}

func sampleMs(p *profile.Profile, idx int, focus *regexp.Regexp) float64 {
	return toMs(p, idx, sampleSum(p, idx, focus))
}

func toMs(p *profile.Profile, idx int, v float64) float64 {
	switch p.SampleType[idx].Unit {
	case "nanoseconds":
		return v / 1e6
	case "microseconds":
		return v / 1e3
	case "seconds":
		return v * 1e3
	}
	return v
}

func stackMatches(sample *profile.Sample, focus *regexp.Regexp) bool {
	for _, loc := range sample.Location {
		for _, line := range loc.Line {
			if line.Function != nil && focus.MatchString(line.Function.Name) {
				return true
			}
		}
	}
	return false
}

// flatTop returns the n functions with the most self time. A location's
// first line is the innermost inlined frame, the one that was executing.
func flatTop(p *profile.Profile, idx, n int) []FuncTime {
	flat := map[string]int64{}
	var total int64
	for _, sample := range p.Sample {
		v := sample.Value[idx]
		total += v
		if len(sample.Location) == 0 || len(sample.Location[0].Line) == 0 || sample.Location[0].Line[0].Function == nil {
			continue
		}
		flat[sample.Location[0].Line[0].Function.Name] += v
	}
	if total == 0 {
		return nil
	}
	names := make([]string, 0, len(flat))
	for name := range flat {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if flat[names[i]] != flat[names[j]] {
			return flat[names[i]] > flat[names[j]]
		}
		return names[i] < names[j]
	})
	out := make([]FuncTime, 0, n)
	for _, name := range names[:min(n, len(names))] {
		out = append(out, FuncTime{
			Function: name,
			Ms:       toMs(p, idx, float64(flat[name])),
			Percent:  math.Round(float64(flat[name])/float64(total)*1e4) / 100,
		})
	}
	return out
}

var summaryEnvKeys = []string{"source", "commit", "dirty_files", "benchmark", "load1_at_start", "load1_at_end"}

func formatNumber(v float64) string {
	return strconv.FormatFloat(v, 'g', 6, 64)
}

func (s *Summary) writeText(w io.Writer) {
	writeKV(w, "label", s.Label)
	writeKV(w, "kind", s.Kind)
	for _, key := range summaryEnvKeys {
		if v, ok := s.Environment[key]; ok {
			writeKV(w, key, v)
		}
	}
	for _, m := range s.Metrics {
		writeKV(w, m.Name, fmt.Sprintf("%s (range %s..%s, n=%d)",
			formatNumber(m.Median), formatNumber(m.Min), formatNumber(m.Max), m.N))
	}
	if len(s.HotFlat) > 0 {
		fmt.Fprintln(w, "hot_flat_top5:")
		for _, f := range s.HotFlat {
			fmt.Fprintf(w, "  %gms %.2f%% %s\n", f.Ms, f.Percent, f.Function)
		}
	}
	switch {
	case !s.SanityChecked && s.Kind == "live":
		writeKV(w, "sanity", "not-checked (live-app capture)")
	case !s.SanityChecked:
		writeKV(w, "sanity", "not-checked (no live-profile rounds)")
	case len(s.SanityFailures) == 0:
		writeKV(w, "sanity", "ok")
	default:
		for _, f := range s.SanityFailures {
			writeKV(w, "sanity_fail", f)
		}
	}
}

func (s *Summary) sanityErr() error {
	if len(s.SanityFailures) > 0 {
		return fmt.Errorf("%w: %s", errSanity, s.Label)
	}
	return nil
}

func (a *app) printSummary(label string, asJSON bool) error {
	s, err := a.loadSummary(label)
	if err != nil {
		return err
	}
	if asJSON {
		enc := json.NewEncoder(a.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(s); err != nil {
			return err
		}
	} else {
		s.writeText(a.stdout)
	}
	return s.sanityErr()
}

func (a *app) summary(_ context.Context, args []string) error {
	fs := newFlagSet("summary")
	asJSON := fs.Bool("json", false, "")
	pos, err := parseArgs(fs, args, 1, "go run ./dev/perf summary my-change", "go run ./dev/perf list")
	if err != nil {
		return err
	}
	return a.printSummary(pos[0], *asJSON)
}
