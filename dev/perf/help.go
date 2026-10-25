package main

const helpDoctor = `go run ./dev/perf doctor

Check the toolchain, machine load, uncommitted changes, and whether a pprof
server is listening on localhost:6060. Exit 1 if a required tool is missing.

Examples:
  go run ./dev/perf doctor
`

const measureFlags = `  --page P           workload: job (default), build, or setup
  --rounds N         rounds; each = 1 benchmark batch + 1 live profile (default 5)
  --bench-count N    benchmark samples per round (default 2 -> 10 samples)
  --benchtime T      go -benchtime (default 1s)
  --duration D       live profile duration per round, >= 1s (default 10s)
  --bench-only       skip the live profile
  --profile-only     skip the replay benchmark
  --quick            1 round, 3 bench samples, 3s profile; ballpark only
  --force            replace existing captures with the same label
`

const helpCapture = `go run ./dev/perf capture LABEL [flags]

Measure one source: the page's replay benchmark (BenchmarkJobPageAnimationFrame
or BenchmarkBuildPageAnimationFrame) and the live Bubble Tea profile
(CPU/heap/allocs/goroutines, terminal writes and bytes). Writes
dist/profiles/LABEL/{environment.txt,bench.txt,PAGE.test,profile-N/}
and prints the summary. Fails if LABEL exists unless --force.

Flags:
  --ref REF          git ref to measure (default: worktree = current files incl. edits)
` + measureFlags + `
Examples:
  go run ./dev/perf capture baseline --ref main
  go run ./dev/perf capture my-change
  go run ./dev/perf capture smoke --quick --force
  go run ./dev/perf capture build-baseline --page build
`

const helpAB = `go run ./dev/perf ab NAME --base REF [--head REF] [flags]

Build both sources, then interleave rounds (base, head, base, head, ...) so
warmup and machine drift hit both sides. Writes NAME-base and NAME-head, then
prints the comparison. Use this for before/after claims.

Flags:
  --base REF         required; the "before" git ref
  --head REF         "after" source (default: worktree, i.e. your uncommitted change)
  --json             print the comparison as JSON
` + measureFlags + `
Examples:
  go run ./dev/perf ab stage-cache --base HEAD
  go run ./dev/perf ab pr-110 --base a4766d7 --head e38bbc5 --rounds 7
  go run ./dev/perf ab try --base main --quick --force
  go run ./dev/perf ab build-clock --base HEAD --page build
`

const helpSummary = `go run ./dev/perf summary LABEL [--json]

Print key: value metrics for a capture: benchmark and live-profile medians
with range and run count, focused CPU (page Model.View, renderer), the top
flat functions, and a sanity line. Exit 3 if the work did not happen (no
views or terminal writes).

Examples:
  go run ./dev/perf summary my-change
  go run ./dev/perf summary my-change | grep '^cpu_'
  go run ./dev/perf summary my-change --json | jq '.metrics[] | select(.name=="cpu_model_view_ms")'
`

const helpCompare = `go run ./dev/perf compare BEFORE AFTER [--json]

benchstat for the replay benchmark (skipped when the benchmarks differ or
benchstat is missing), then per-metric median deltas with a verdict:
  ranges-separate    every run of one side beat every run of the other
  ranges-overlap     no measurable difference
  negligible(<2%)    medians within 2%
  too-few-runs       fewer than 3 runs on a side
Exit 3 if either side failed its sanity check.

Examples:
  go run ./dev/perf compare stage-cache-base stage-cache-head
  go run ./dev/perf compare job-112-after my-change --json
`

const helpLive = `go run ./dev/perf live LABEL [--duration D] [--force]

Profile a real running app through its pprof server (localhost:6060, served
only when DEVELOPMENT is set). Start one yourself without a human:

  go build -o dist/bin/jt ./dev/jt                # once; needs local Jenkins
  JT_DEVELOPMENT=1 dist/bin/jt up perf
  dist/bin/jt keys perf ...                       # drive to the screen
  go run ./dev/perf live perf-job --duration 15s
  dist/bin/jt down perf

Writes dist/profiles/LABEL/live/{cpu,heap,allocs,goroutine}.pprof. Only one
DEVELOPMENT instance can own :6060 at a time. A live capture is a single run:
use it to find hot spots, not to claim a delta.

Flags:
  --duration D       CPU sampling time, whole seconds (default 10s)
  --force            replace an existing LABEL

Examples:
  go run ./dev/perf live home-idle
  go run ./dev/perf live job-running --duration 20s --force
`

const helpList = `go run ./dev/perf list

List captures with their commit and source.

Examples:
  go run ./dev/perf list
`
