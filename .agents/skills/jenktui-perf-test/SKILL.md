---
name: jenktui-perf-test
description: Measure JenkTui rendering performance on your own, with no human-run `task dev`. Interleaved before/after A/B with benchstat, CPU/alloc/terminal-output profiles, noise verdicts, and a work-happened sanity check, all through the non-interactive `go run ./dev/perf` CLI. Use it for any perf claim (PR before/after, regression check, "is this faster?"), after changing rendering, View, stage/job page, animation, or lipgloss-heavy code, or to profile a live screen against local Jenkins.
argument-hint: "[ab NAME --base REF | capture LABEL | live LABEL]"
---

# JenkTui performance test

Everything runs through `go run ./dev/perf` (also `task perf -- ...`), a Go
command in this repo. It is non-interactive and prints `key: value` lines
(`--json` on `summary`, `compare`, `capture`, and `ab`). It exits 0 on success,
2 on a usage error (the message includes a correct example invocation), and 3
when a sanity check shows the measured work did not happen. Every subcommand
takes `--help` with examples. Its logic is unit-tested in `dev/perf/`.

Write each call in full (`go run ./dev/perf ...`). A `P="go run ./dev/perf"`
variable does not word-split in zsh.

```bash
go run ./dev/perf doctor                                   # toolchain, load, dirty tree, :6060 status
go run ./dev/perf ab my-change --base HEAD                 # before/after of your uncommitted change
go run ./dev/perf ab pr-110 --base a4766d7 --head e38bbc5  # between two commits
go run ./dev/perf capture my-change                        # one side only
go run ./dev/perf summary my-change                        # re-read a capture
go run ./dev/perf compare job-113 my-change --json         # any two captures
go run ./dev/perf ab my-change --base HEAD --page build    # build-page workload instead of the job page
go run ./dev/perf list
```

## What it measures

Most measurements use the offline `job-animation-v1` workload. The page is 160×50,
with 20 builds and one running stage, rendered by Bubble Tea's real renderer into
a counting sink. It needs no Jenkins, no TTY, and no personal config.
`newAnimationPage` in `src/ui/components/job/animation_bench_test.go` defines
it. `--page build` measures `build-animation-v1` instead: a 160×50 build page
with a running stage and step, defined by `newAnimationPage` in
`src/ui/components/build/animation_workload_test.go`. Each round runs:

- **Replay benchmark** `BenchmarkJobPageAnimationFrame`: ns/B/allocs per
  Update/View frame. This is the micro number.
- **Live profile**: 10 s of real command delivery. It records sampled CPU, focused
  CPU for the page (`Model.View`) and the renderer (`cursedRenderer`), views/s,
  terminal writes/s and bytes/s, allocations, and goroutines. This is the
  end-to-end number.

`ab` builds both sources: a detached temporary git worktree for a ref, or the
current files for `worktree`. It then alternates rounds base, head, base, head,
so warmup and machine drift affect both sides equally. Defaults are 5 rounds,
10 benchmark samples per side, and about 2.5 min total. Output goes to
`dist/profiles/<LABEL>/` (override with `PERF_ROOT`), laid out for the pprof
command in step 5 below.

## Procedure

1. Run `go run ./dev/perf doctor`. If it warns about load, report it, and rely on the
   interleaving rather than on a quiet machine.
2. Write the claim you expect to make before you measure.
3. Run `go run ./dev/perf ab NAME --base <before-ref>`. It takes over 2 minutes, so run it in
   the background if your tool would time out. Use `--quick` only for a stated
   ballpark (1 round). `--bench-only` or `--profile-only` narrows the run.
4. Read the comparison:
   - benchstat `~` or `ranges-overlap` means **no measurable difference**.
     `negligible(<2%)` is the same.
   - `ranges-separate` means every run on one side beat every run on the other.
     It is the only median verdict that supports a claim.
   - `too-few-runs` means n<3. Rerun with more `--rounds`.
   - `sanity_fail` or exit 3 means views or terminal writes were missing, so the
     number is not about the work. Do not report it.
5. Name the limiter. Print the per-round hot list with
   `go run ./dev/perf summary LABEL --json | jq .hot_flat`, or dig in with
   `go tool pprof -top -focus '<regex>' dist/profiles/LABEL/job.test dist/profiles/LABEL/profile-1/cpu.pprof`.
   Separate page CPU (`cpu_model_view_ms`) from renderer CPU (`cpu_renderer_ms`).
   A page-only optimization cannot cut more than the page's share.
6. Report the verdict first, then one primary number with its unit, the run
   count, the range, and the limiter. Example: "page View CPU 170 → 90 ms per
   10 s, median of 5 interleaved rounds, ranges separate; renderer `Draw` now
   dominates."

If the change touches a screen neither offline workload covers, add one on the
shared `src/ui/perfprofile` harness (see
`src/ui/components/build/animation_workload_test.go`), register it in `pages`
in `dev/perf/capture.go`, or profile the real app (next section).

## Live app profiling (local Jenkins)

pprof is served on `localhost:6060` only when `DEVELOPMENT` is set. Start an
isolated instance with the verify-jenktui harness. No human terminal is needed.

```bash
go build -o dist/bin/jt ./dev/jt && J=dist/bin/jt
JT_DEVELOPMENT=1 $J up perf          # needs `task jenkins:lts` + `task seed`
$J keys perf ...                     # drive to the screen to profile
go run ./dev/perf live perf-job --duration 15s      # cpu/heap/allocs/goroutine pprof + summary
$J down perf
```

Only one DEVELOPMENT instance can own :6060 at a time. A live profile is a
single run with no A/B. Use it to locate hot spots, not to claim a delta.

## Reference points (same i5-10310U)

Absolute numbers depend on the machine. Compare A/B on one machine, and do not
reuse these numbers across hosts. Each label below is a capture under
`dist/profiles/` if it is present locally, so
`go run ./dev/perf compare <label> my-change` works against it. All live
numbers are medians per 10 s run.

### Job page (`--page job`)

| | Baseline `job-113` (2026-09-27) | Current `job-current` (2026-10-03, 314c31e) |
| --- | ---: | ---: |
| Replay | 555 µs/op, 170 KiB/op, 2.137k allocs/op | 702 µs/op, 173 KiB/op, 2.137k allocs/op |
| Views/s, terminal writes/s | 10.6, 10.4 | 10.6, 10.3 |
| Terminal bytes/s | about 3.4 KB | 3.36 KB |
| Sampled CPU | 560 ms | 390 ms |
| `Model.View` / renderer | 170 ms / 310 ms | 120 ms / 220 ms |

The job page's rendering code did not change between the two captures, and
allocations match exactly. The replay time difference is machine load
(load1 1.5 to 2, with a range of 591 µs to 1.0 ms), not a regression. The
renderer (`StyledString.Draw`, then the ultraviolet cell diff) is the larger
cost.

### Build page (`--page build`)

| | Baseline `build-115` (two spinner chains) | `build-116` (one frame clock) | `build-117` (placeholder frame cache) | Current `build-sections2` (section cache) |
| --- | ---: | ---: | ---: | ---: |
| Replay | 4.28 ms/op, 844 KiB/op, 18.4k allocs/op | 4.47 ms/op, same | 17.4 µs/op, 19 KiB/op, 28 allocs/op | 12.3 µs/op, 23 KiB/op, 46 allocs/op |
| Views/s, terminal writes/s | 20.5, 18.6 | 10.6, 10.4 | 10.5, 10.4 | 10.6, 10.3 |
| Terminal bytes/s | 1.84 KB | 1.85 KB | 1.85 KB | 1.84 KB |
| Sampled CPU | 1910 ms | 1020 ms | 380 ms | 280 ms |
| `Model.View` / renderer | 1430 ms / 320 ms | 710 ms / 200 ms | 100 ms / 220 ms | 10 ms / 240 ms |

`build-sections2` was measured on 2026-10-03 against `build-117`'s code in
the same interleaved run, where `build-117` replayed at 8.9 µs/op. The section
cache's replay is about 38% slower than splicing placeholders, but the live
run allocates half as much and page `View` CPU fell from 50 to 10 ms (ranges
separate), because startup messages no longer force full re-renders. The
renderer is now nearly all of the CPU.

The build page caches by section (`src/ui/components/build/sections.go` on
`src/ui/block`). A frame re-renders only the sections that show a spinner,
and each of those keeps one entry per spinner frame, so after one spinner
cycle every frame is a cache hit. A build-page replay above about 100 µs/op
means sections are missing on every frame; `TestFrameRerendersOnlyAnimatedSections`
pins which sections may re-render.

At 20 FPS instead of 10 (`FrameInterval`), views and terminal writes double
on both pages. Sampled CPU rises 68% on the build page and 83% on the job
page, mostly in the renderer. The spinners are 10 FPS designs, so they would
also spin twice as fast.
