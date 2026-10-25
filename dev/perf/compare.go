package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	verdictTooFew     = "too-few-runs"
	verdictNegligible = "negligible(<2%)"
	verdictSeparate   = "ranges-separate"
	verdictOverlap    = "ranges-overlap"
)

// Delta compares one metric across two captures.
type Delta struct {
	Metric   string   `json:"metric"`
	Before   Dist     `json:"before"`
	After    Dist     `json:"after"`
	DeltaPct *float64 `json:"delta_pct"` // nil when the before median is 0
	Verdict  string   `json:"verdict"`
}

// verdict is deliberately conservative: a difference counts only when every
// run of one side beat every run of the other.
func verdict(before, after Dist, deltaPct *float64) string {
	switch {
	case before.N < 3 || after.N < 3:
		return verdictTooFew
	case deltaPct != nil && *deltaPct > -2 && *deltaPct < 2:
		return verdictNegligible
	case after.Max < before.Min || after.Min > before.Max:
		return verdictSeparate
	default:
		return verdictOverlap
	}
}

func compareSummaries(before, after *Summary) []Delta {
	var deltas []Delta
	for _, b := range before.Metrics {
		a, ok := after.metric(b.Name)
		if !ok {
			continue
		}
		d := Delta{Metric: b.Name, Before: b.Dist, After: a.Dist}
		if b.Median != 0 {
			pct := (a.Median - b.Median) / b.Median * 100
			d.DeltaPct = &pct
		}
		d.Verdict = verdict(b.Dist, a.Dist, d.DeltaPct)
		deltas = append(deltas, d)
	}
	return deltas
}

type comparison struct {
	Before    *Summary `json:"before"`
	After     *Summary `json:"after"`
	Benchstat string   `json:"benchstat,omitempty"`
	Note      string   `json:"benchstat_note,omitempty"`
	Deltas    []Delta  `json:"deltas"`
}

// benchstat runs the external tool when both captures timed the same
// benchmark; otherwise it says why it was skipped.
func (a *app) benchstat(ctx context.Context, before, after *Summary) (out, note string) {
	switch {
	case before.Benchmark == "" || after.Benchmark == "":
		return "", "skipped: a side has no bench.txt"
	case before.Benchmark != after.Benchmark:
		return "", fmt.Sprintf("skipped: %s vs %s measure different units", before.Benchmark, after.Benchmark)
	}
	if _, err := exec.LookPath("benchstat"); err != nil {
		return "", "skipped: benchstat not installed (task benchstat:install)"
	}
	cmd := exec.CommandContext(ctx, "benchstat",
		before.Label+"="+filepath.Join(before.Label, "bench.txt"),
		after.Label+"="+filepath.Join(after.Label, "bench.txt"))
	cmd.Dir = a.root
	raw, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Sprintf("benchstat failed: %v: %s", err, raw)
	}
	var kept []string
	for _, line := range strings.Split(strings.TrimRight(string(raw), "\n"), "\n") {
		if !strings.HasPrefix(line, "goos:") && !strings.HasPrefix(line, "goarch:") &&
			!strings.HasPrefix(line, "pkg:") && !strings.HasPrefix(line, "cpu:") {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n"), ""
}

func (a *app) printComparison(ctx context.Context, beforeLabel, afterLabel string, asJSON bool) error {
	before, err := a.loadSummary(beforeLabel)
	if err != nil {
		return err
	}
	after, err := a.loadSummary(afterLabel)
	if err != nil {
		return err
	}
	c := comparison{Before: before, After: after, Deltas: compareSummaries(before, after)}
	c.Benchstat, c.Note = a.benchstat(ctx, before, after)
	if asJSON {
		enc := json.NewEncoder(a.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(c); err != nil {
			return err
		}
	} else {
		c.writeText(a)
	}
	return errors.Join(before.sanityErr(), after.sanityErr())
}

func (c *comparison) writeText(a *app) {
	w := a.stdout
	fmt.Fprintf(w, "== compare %s -> %s\n", c.Before.Label, c.After.Label)
	fmt.Fprintln(w, "-- benchstat")
	if c.Note != "" {
		fmt.Fprintln(w, c.Note)
	} else {
		fmt.Fprintln(w, c.Benchstat)
	}
	fmt.Fprintln(w, "-- medians (ranges-separate = every run of one side beat every run of the other)")
	for _, d := range c.Deltas {
		pct := "n/a"
		if d.DeltaPct != nil {
			pct = fmt.Sprintf("%+.1f%%", *d.DeltaPct)
		}
		v := d.Verdict
		if v == verdictTooFew {
			v = fmt.Sprintf("%s(n=%d/%d)", v, d.Before.N, d.After.N)
		}
		fmt.Fprintf(w, "%-28s %12s -> %-12s %8s  %s\n", d.Metric, formatNumber(d.Before.Median), formatNumber(d.After.Median), pct, v)
	}
	for _, s := range []*Summary{c.Before, c.After} {
		switch {
		case !s.SanityChecked:
			fmt.Fprintf(w, "%s sanity: not-checked\n", s.Label)
		case len(s.SanityFailures) == 0:
			fmt.Fprintf(w, "%s sanity: ok\n", s.Label)
		default:
			for _, f := range s.SanityFailures {
				fmt.Fprintf(w, "%s sanity_fail: %s\n", s.Label, f)
			}
		}
	}
}

func (a *app) compare(ctx context.Context, args []string) error {
	fs := newFlagSet("compare")
	asJSON := fs.Bool("json", false, "")
	pos, err := parseArgs(fs, args, 2, "go run ./dev/perf compare base head", "go run ./dev/perf list")
	if err != nil {
		return err
	}
	return a.printComparison(ctx, pos[0], pos[1], *asJSON)
}
