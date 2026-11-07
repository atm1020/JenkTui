# Coding standards

Authoritative rules, reached through [AGENTS.md](AGENTS.md)'s task routes.
Read the applicable sections; verification skills supply execution procedures,
not exceptions to these gates.

## Completion gates

Use `Taskfile.yml` tasks; iterate with focused Go package tests. Before finishing
a code change, pass `task check` (including race tests) and `task build`. Fix
formatting with `task fmt`, lint with `task lint:fix`, then rerun the checks.
Documentation-only changes require no Go build. Unit tests do not replace the
live/performance gates below; complete every triggered gate and same-change
maintenance before reporting completion.

## Live verification

Run live verification after changes under `src/ui/`, `src/poll/`, `src/store/`,
`src/notification/`, `src/config/`, `src/context/` (including keybindings),
`src/buildstatus/`, `src/jobref/`, or `main.go`.

Read [verify-jenktui](.agents/skills/verify-jenktui/SKILL.md) and the matching
recipe in its [feature index](.agents/skills/verify-jenktui/features/README.md).
Use its real-binary/local-Docker-Jenkins harness with isolated `$HOME` and private
tmux server: build `dist/bin/jt` from `./dev/jt`, then run `up` and `doctor`.
Drive **every** recipe entry point, snap before and after, and verify side effects
in `runs.db` and Jenkins state. Run `down` after **every attempt**, including failures.
Report what was proved and its evidence path `dist/verify/<RUN>/`. If Docker or
Jenkins is unavailable, report the skip and reason; never claim live behavior
from unit tests alone.

## Performance verification

Run [jenktui-perf-test](.agents/skills/jenktui-perf-test/SKILL.md) after changing
rendering or anything on the frame path: `View`, job/build pages,
spinners/animation/frame clock, section/frame caches (`src/ui/block`,
`src/ui/components/build/sections.go`), lipgloss-heavy styling, `src/ui/styles/`,
or poll-to-render delivery. Also run it for **any performance claim**.

Follow the skill's A/B procedure: run `go run ./dev/perf doctor`, write the expected
claim, then run `go run ./dev/perf ab <name> --base HEAD` for uncommitted changes
(use the before-ref for committed changes; add `--page build` for the build page).
Only `ranges-separate` supports a measured-difference claim; `~`, `ranges-overlap`,
and `negligible` mean no measurable difference. Never report `sanity_fail`/exit 3
results. Report verdict, primary number/unit, run count, range, and limiter.
Apply [same-change maintenance](#verification-maintenance) when its triggers match.

## Verification maintenance

Keep verification current **in the same change**; read the affected
[live skill](.agents/skills/verify-jenktui/SKILL.md),
[feature index](.agents/skills/verify-jenktui/features/README.md), or
[performance skill](.agents/skills/jenktui-perf-test/SKILL.md).

- New or changed feature, key, view, dialog, or footer text: update or add
  the matching recipe in `.agents/skills/verify-jenktui/features/` and its index.
- Changed default keys, wait handles (breadcrumb, headers, `Loaded:` line),
  router history size, config fields, or directory layout: update the live skill.
- Changed `dev/jt/` behavior: update the live skill and the `jt` unit tests.
- New seed jobs or Jenkins fixtures (`dev/seed/`, `dev/docker/`): update the
  recipes that rely on them.
- A screen neither offline workload covers: add a workload on
  `src/ui/perfprofile` and register it in `pages` in `dev/perf/capture.go`.
- A changed workload, benchmark name, CLI flag, or output field: update the
  performance skill.
- A deliberate performance change on the job or build page: add the new
  capture label and numbers to the performance skill's reference tables,
  marked with the date and commit.

## UI architecture

Pages implement `util.JenkTuiModel` from `src/ui/util/model.go`. Declare page
methods in this order: `Init`, `Update`, `View`, `ID`, `UpdateFooter`, `CleanUp`,
`KeyMap`, even if the Go interface lists them differently. Keep helpers above
or below this method block, not between its methods.

### Update methods

Keep `Update` focused on dispatch. Extract a dedicated handler when a branch
exceeds four lines or contains non-trivial logic; use
`src/ui/components/build/build.go` as the pattern.

Dialogs use the separate `JenkTuiDialogModel` interface. The app shell owns the
active dialog and router. Navigate by emitting a model, not by mutating router
state from a component:

```go
return util.CmdHandler(router.NavigateMsg{Model: nextModel})
```

The router calls `CleanUp` when leaving a view and `Init` when entering one.
Release owned resources and poll leases in `CleanUp`; make cleanup idempotent.
View IDs must be stable and unique for the represented resource.

Bubble Tea commands run asynchronously and `tea.Batch` does not preserve result
order. Do not perform network, filesystem, database, or editor work directly in
`Update` or `View`; return a command and handle its typed result message. Keep
`View` deterministic and side-effect free. Destination views should navigate
immediately, begin in a loading state, and fetch their own data from `Init`.

## Polling and alerting

- Reuse `src/buildstatus/` and `src/jobref/` for status and Jenkins job identity;
  do not reimplement status/URL parsing. Scope persisted runs by Jenkins server ID.
- A view acquires a `poll.Watch` for a `TargetJobWindow` or `TargetExactRun` and
  releases it in `CleanUp`.
- Apply replay/delivery snapshots only when `watch.Matches(snapshot)`; generation
  checks reject stale asynchronous results.
- `poll.Manager.Results()` has one intended consumer: the app shell. Components
  receive routed typed snapshots through `ApplyPollSnapshot`.
- Snapshot replay is state, not a completion event.
- Jenkins observations are persisted centrally and transactionally. My Runs is a
  query consumer; do not add a second observation writer in a UI component.
- Completion records and notification acknowledgement are durable via the SQLite
  outbox. Never acknowledge a completion before its side effect succeeds.
- Trigger queue receipts/intents are durable. Prefer queue-ID correlation over
  guessing a build from timestamps or build-number baselines.
- Scheduler work must remain bounded, cancellable, non-overlapping per job, and
  joined by `Stop`. Delivery is coalesced; do not create per-run result floods.
- Completion eligibility is global; desktop and sound side effects remain gated
  by user configuration.

## Configuration

Merge missing application fields with defaults in `src/config/loader.go`, including
new keybindings for existing configs. Write config files with mode `0600`.
Claim environment-override support only when `src/config/dirs.go` implements it.

### Keybindings

New keybindings must remain config-driven:

1. Add the `KeyBinding` field to the appropriate config type in
   `src/context/keys.go`.
2. Add its default in `getDefaultKeyMapConfig` in `src/config/loader.go`.
3. Build the component binding from that config in its `keys.go`.
4. Update config/README tests or examples when the public schema changes.

## Styling

Use `styles.GetTheme()` and shared styles rather than literal colors. Preserve
`tea.View.BackgroundColor = t.BgBase` (OSC 11) and transparent base styles;
full-screen/per-cell `BgBase` painting regresses rendering performance. Add
backgrounds only to surfaces that differ from the base (selection, badge, footer,
dialog, status); retain the theme tests guarding this contract.

## Testing

- Put tests beside the package as `*_test.go`; use temporary directories and
  fakes/test servers, not a developer's real config or Jenkins installation.
- Prefer behavior/state/message assertions over brittle full rendered-string
  snapshots; layout truncates content according to terminal dimensions.
- Test asynchronous command results explicitly and keep background loops finite.
- Poll/store tests should cover cancellation, idempotency, stale generations,
  bounded concurrency, durable completion dedupe, and restart behavior where
  relevant.
