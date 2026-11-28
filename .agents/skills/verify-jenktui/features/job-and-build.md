# Job and build pages

The job page shows a job's health and recent builds, with each build's
stages laid out as a grid (collapsing parallel blocks into fixed-height
Parallel Group Cards). Opening a build shows an Omaci-inspired unified layout:
a compact framed `build` summary bar, a framed `stages` Stage Timeline View
plotted on a shared horizontal time axis with tree connectors and durations,
and a split bottom area pairing a compact `steps` list on the left with an
inline `log` viewer on the right (with instant stage/step jumping and 4 scopes:
`[1] full`, `[2] stage`, `[3] step`, `[4] outside`, plus `[z]` tall-log zoom).

## Sub-features

- `job-unsupported`: an unresolved target with no supported page stays on
  the destination with `Navigation to ... is not supported` and a configured
  Back-key hint. Back returns to the caller; Forward reopens the message.
- `job-header`: the job name, `Health Score: N%` (or
  `No health report available`), and the build stability text.
- `job-builds`: a `Builds` column of up to 10 `#N` boxes derived from the
  classic stage view (`wfapi/runs`), newest first, staying live-synced as new
  builds start. A selected build shows its date and duration.
- `job-startup`: the first ready content includes stage summaries from classic
  `/wfapi/runs` (without `fullStages=true`), without a second runs fetch or a
  later stage pop-in. Cold URL opens wait for both the projected job record
  (`?tree=` without `builds`) and runs state; replay supplies runs, not
  cold job-request completion. Revisiting an already-resolved pipeline job
  (`Back`/`Forward` or `ctrl+j` from a build page) with a warm replay renders
  immediately while refreshing metadata in the background. A startup poll
  failure replaces loading with `Could not load pipeline runs:` plus the
  underlying error and Back hint. A successful retry restores stages; refresh
  failures leave already-loaded runs usable.
- `job-stages`: for each build, sequential stages render as stage boxes
  showing name and duration (for mobile-app: `Checkout`, `Dependencies`,
  `Lint`, `Unit Tests`, `Build iOS`, and more), while overlapping parallel
  stages collapse into a single fixed-height Parallel Group Card with
  `├─`/`└─` branch rows, status icons, durations, worst-status border color,
  and `+N more` overflow when more than five branches exist (for
  `webapp-build`: `Install`, `Parallel Tests { Unit, Integration, Lint }`,
  `Bundle`).
- `job-nav`: `j`/`k` move between builds. `B`/`S` focus the builds pane or
  the stages pane.
- `build-open`: Enter opens the framed `build` bar (`Build #N` with `Started`,
  `Duration`, `Status`, `Started by`, `Details`, `Node`, and `Artifacts`), the
  framed `stages` timeline, and the split `steps` + inline `log` panes. Opening
  a failed or running build auto-focuses the first failed navigable stage (or
  active stage if none failed), loads its steps, and loads + indexes the inline
  console log (jumping to the focused stage/step line).
- `build-stages`: on the build page, stages render inside the `stages` frame as
  a Stage Timeline View (`STAGE` / time axis / `DUR`) with half-block bars (`▄`,
  shaded/pulsing bars and a live `now` marker while running) and `├─`/`└─` tree
  connectors for parallel branches. `h`/`l` move the focused stage marker (`▸`),
  skipping empty Declarative parallel wrapper headings and jumping the inline
  log to that stage; `j`/`k` select a compact single-line step in the `steps`
  pane and jump/update the inline log.
- `build-inline-log`: the right-hand `log` pane supports 4 scopes via `1`
  (`full` with stage/step jumping), `2` (`stage`, cleanly de-interleaved across
  parallel branches via `/logText/progressiveHtml`), `3` (`step`), and `4`
  (`outside`, showing Pre-stage setup and Post-stage errors/stacktraces outside
  stages). `Tab`/`Shift+Tab` and `ctrl+h`/`ctrl+l` switch focus between `steps`
  and `log`; `z` toggles zoomed tall-log mode (`Esc` unzooms); `L` toggles full
  console log ↔ scoped step/stage log.
- `build-step-log`: `ctrl+l` focuses the inline log pane on the selected flow
  step (preserving the `Loading <stage> stage…` stale-details guard during
  stage switches). The footer **stays** at `…mobile-app  N` (no `console`) in
  scoped mode, and shows `…console` when focused on Full Console log.
- `build-help`: `?` expands the footer into the full key list for the page.
- `build-params`: `p` shows build parameters, or `No parameters used for
  this build` when empty.

## How to get to it (user POV)

- Global search result → Enter.
- Folder listing row → Enter.
- Favorites row → Enter.
- My Runs row: Enter opens the build, and `ctrl+j` opens the job.
- From a build page, `ctrl+j` returns to its job.

## Driving it with jt

Preconditions:

- The instance is ready. `DevTeam/Frontend/mobile-app` has at least one
  completed build (two to verify selection movement). Check with
  `$J jenkins RUN '/job/DevTeam/job/Frontend/job/mobile-app/api/json?tree=lastCompletedBuild[number]'`.

Steps:

- **Open the job.**
  - Follow `search-open-job` in
    [search-and-browse.md](./search-and-browse.md).
  - Wait for `$J wait RUN '│ #[0-9]+ │'` before pressing any key. The
    `Builds` column lists `#<lastBuild>` first (a stale column is possible;
    see Gotchas).
- **Shared startup and replay.**
  - Repeat job opening from search, a folder row, Favorites, My Runs `ctrl+j`,
    and a build page `ctrl+j`. Snap the caller before opening and the ready
    job afterwards; wait for build boxes and a stage name (`Checkout` for
    mobile-app), not just health/breadcrumb text.
  - From a folder, use `$J wait RUN '[0-9]+ items'` (local seed counts vary).
    Snap before Enter, then after readiness. Back to the folder, snap,
    Forward to the job, wait for the same content, and snap again.
  - Trigger mobile-app through the UI, wait for its My Runs row to read
    `In progress`, snap before `ctrl+j`, reopen the job, and snap its running
    stage/pulse. Cross-check `building=true` in Jenkins, then the resolved
    intent and final `SUCCESS` row in runs.db. See the trigger recipe.
  - A local server can complete startup between terminal captures. The real
    poll-manager request-count regression test pins two parallel initial
    requests (one projected job query without `builds` and one classic
    `wfapi/runs` query without `fullStages=true`), including snapshot/job
    ordering and replay; terminal snapshots alone cannot prove request counts.
  - If initial polling fails, wait for `Could not load pipeline runs:` and
    `Back returns`, then snap the error and use Back to leave. Do not require
    build boxes on a failed load. Do not change shared Jenkins permissions to
    manufacture a failure: the persistent-403 real-manager regression test
    covers this case, both result orderings, stale failures, and recovery.
- **Unsupported kind.**
  - Search for `quick-health-check`, snap the selected result, then press Enter.
  - Run `$J wait RUN 'Navigation to .*quick-health-check.* is not supported'`
    and `$J wait RUN 'Back returns'`, then snap the unsupported page.
  - Press `b`, wait for the caller's breadcrumb and content, and snap it.
    Press `]`, wait for the same unsupported message, and snap again.
  - This is a real destination, not an error dialog or an automatic back-out.
- **Inspect the stages.**
  - Run `$J screen RUN | grep -E 'Checkout|Unit Tests'`.
  - Stage names appear in the grid.
- **Move the selection.**
  - Run `$J keys RUN j`, then capture `$J snap RUN job-selection` and
    inspect the ANSI selection against the preceding snapshot.
  - The selected build changes to the second-newest build. Its date and
    duration lines appear next to it.
- **Open the build.**
  - Run `$J keys RUN Enter && $J crumb RUN 'mobile-app.*<N>' && $J wait RUN 'Build #<N>'`.
  - Wait for loaded details (`$J wait RUN 'Stage ID:'` on this completed
    pipeline) before further keys. The header alone can precede readiness.
    Capture `$J snap RUN build-page` while on the build page.
- **Move between stages.**
  - Run `$J keys RUN l`.
  - The timeline focus marker (`▸`) moves from `Checkout` to `Dependencies`.
- **Parallel stages (`DevTeam/Frontend/webapp-build`).**
  - Open `DevTeam/Frontend/webapp-build` via Global Search (`C-p`), wait for
    `$J crumb RUN 'Frontend.*webapp-build'`, `$J wait RUN '│ #[0-9]+ │'`, and
    `$J wait RUN 'Parallel Tests'`, then capture `$J snap RUN parallel-job-page`.
  - Confirm `Parallel Tests` collapses `├─` `Unit`, `├─` `Integration`, and
    `└─` `Lint` into one fixed-height Parallel Group Card between `Install`
    and `Bundle` (and that any failed run's card uses the failure status icon
    and border color).
  - Open a completed build with `Enter`, wait for `$J wait RUN 'Stage ID:'`,
    and capture `$J snap RUN parallel-build-page`. Confirm the Stage Timeline
    View renders the `STAGE` / time-axis / `DUR` header, horizontal block
    bars, and indented `├─`/`└─` parallel branches under `Parallel Tests`.
  - Press `l` from `Install` and verify navigation skips the empty
    `Parallel Tests` wrapper heading and focuses `Unit` directly (`steps ─ Unit`),
    jumping the inline log to `Running unit tests...`. Press `2` (`Stage: Unit`)
    to verify parallel branch de-interleaving (only `Unit` lines), `3` for step
    scope, and `4` (`Outside Stages`) for Pre-stage & Post-stage lines.
  - On a failed build (such as `#16` where `Integration` failed with an uncaught
    post-stage `NoSuchMethodError`), verify smart auto-focus lands on `Integration`
    and pressing `4` (`Outside Stages`) surfaces the Post-stage stacktrace.
  - Trigger a live run (`C-b` → confirm `y`), wait for the new `#N` row and
    final `SUCCESS` transition, then cross-check `$J db` (`runs` table) and
    `$J jenkins` (`/job/DevTeam/job/Frontend/job/webapp-build/<N>/api/json`).
- **Switch stage and focus step log at once.**
  - First restore Checkout with `h` and wait for its details (`steps ─ Checkout`).
    Then send `$J keys RUN l C-l` together. If the footer reports
    `Loading Dependencies stage…`, wait for `steps ─ Dependencies` and press
    `C-l` again; confirm the inline log shows Dependencies output
    (`Installing pods/gradle deps...`), never Checkout's. Return focus to steps
    with `C-h`. This guards #119.
- **Full log toggle & zoom from the build page.**
  - Run `$J keys RUN L && $J crumb RUN '<N>.*console' && $J wait RUN 'Loaded:'`
    to switch to/focus Full Console log inline, then `z` to zoom full-screen
    (`zoomed`) and `Escape` to return to the split build view.
- **Back to the job.**
  - From steps focus (`C-h`), run `$J keys RUN b && $J crumb RUN 'Frontend.*mobile-app' && $J wait RUN '│ #[0-9]+ │'`.
- **Proof.**
  - Run `$J snap RUN job-page` on the job page; retain the earlier
    `build-page` snap from the actual build page.
  - Cross-check the build's real result with
    `$J jenkins RUN '/job/DevTeam/job/Frontend/job/mobile-app/<N>/api/json?tree=result,duration'`.

## Gotchas

- The left `Builds` column is capped to the 10 runs returned by classic Stage
  View (`wfapi/runs`), so older builds beyond the latest 10 do not appear in
  the `Builds` filter. Newly triggered builds appear live in both `Builds` and
  the stage grid as soon as polling observes them.
- At 160 columns, only the first 5 to 7 stages fit. Later stages are cut
  off at the right edge. Do not assert on `Deploy`-type stage names unless
  you raised `JT_COLS`.
- A pipeline or unknown-kind target opens immediately with a centered `Loading job...`
  spinner before its job data arrives. Wait for build boxes, not a header
  or breadcrumb alone, before sending job keys; the loading frame may be
  too brief to catch against local Jenkins.
- The job page animates (spinners) while a build runs. `snap` captures a
  single frame, so wait for text, not for a spinner glyph.
- The `.ansi` snap is the only place where status colors and the selection
  highlight are visible. The plain `.txt` loses them.
