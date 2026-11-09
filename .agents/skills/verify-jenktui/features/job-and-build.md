# Job and build pages

The job page shows a job's health and recent builds, with each build's
stages laid out as a grid. Opening a build shows its metadata, its artifacts,
and its stages, each with a status and duration.

## Sub-features

- `job-unsupported`: an unresolved target with no supported page stays on
  the destination with `Navigation to ... is not supported` and a configured
  Back-key hint. Back returns to the caller; Forward reopens the message.
- `job-header`: the job name, `Health Score: N%` (or
  `No health report available`), and the build stability text.
- `job-builds`: a `Builds` column of `#N` boxes, newest first. A selected
  build shows its date and duration.
- `job-startup`: the first ready content includes full stage detail, without
  a second runs fetch or a later stage pop-in. Loading requires both the full
  job record and runs state; replay supplies runs, not job-request completion.
  A startup poll failure replaces loading with `Could not load pipeline runs:`
  plus the underlying error and Back hint. A successful retry restores stages;
  refresh failures leave already-loaded runs usable.
- `job-stages`: for each build, the stage boxes show the stage name and
  duration (for mobile-app: `Checkout`, `Dependencies`, `Lint`,
  `Unit Tests`, `Build iOS`, and more).
- `job-nav`: `j`/`k` move between builds. `B`/`S` focus the builds pane or
  the stages pane.
- `build-open`: Enter opens `Build #N` with `Started`, `Duration`,
  `Status`, `Started by`, `Details`, `Node`, and `Artifacts`.
- `build-stages`: on the build page, `h`/`l` move the highlighted stage
  (drawn with a double border); `j`/`k` select a step in its details.
- `build-step-log`: `ctrl+l` opens the selected flow step's log, not
  necessarily all output from that stage. The footer **stays** at
  `…mobile-app  N` (no `console`).
- `build-help`: `?` expands the footer into the full key list for the page.
- `build-params`: `P` shows build parameters, or `No parameters used for
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
    poll-manager request-count regression test pins one initial full-stage
    runs query (three total requests), including snapshot/job ordering and
    replay; terminal snapshots alone cannot prove request counts.
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
  - The double-bordered box moves from `Checkout` to `Dependencies`.
- **Switch stage and open its step log at once.**
  - First restore Checkout with `h` and wait for its details (for this
    fixture, `Stage ID: 6`). Then send `$J keys RUN l C-l` together.
    If the footer reports `Loading Dependencies stage…`, wait for the
    new details and press `C-l` again, then `$J wait RUN 'Loaded:'`.
  - The log shows the **Dependencies** output (`Installing pods/gradle deps...`),
    never Checkout's (`Checking out source...`). If you press `ctrl+l` before
    the details have loaded, the footer may first say
    `Loading Dependencies stage…`; press it again. Then run `b` and `h` to
    return to Checkout. Explicitly select the intended flow step before
    asserting its output. This guards #119.
- **Step log.**
  - Select Dependencies with `l`, wait for its details (`Stage ID: 12`
    for this fixture), and select its `Print Message` step with `j`/`k`.
    Run `$J keys RUN C-l && $J wait RUN 'Loaded:'`.
  - The screen shows that step's Dependencies output
    (`Installing pods/gradle deps...`). Cross-check with
    `$J jenkins RUN '/job/DevTeam/job/Frontend/job/mobile-app/<N>/consoleText' | grep -c 'Installing pods'`.
  - Then run `$J keys RUN b && $J wait RUN 'Build #<N>'`.
- **Full log from the build page.**
  - Run `$J keys RUN L && $J crumb RUN '<N>.*console' && $J wait RUN 'Loaded:'`.
  - For #557, the status line reads `Loaded: 1-73/73`, and `jt jenkins …/consoleText | wc -l`
    gives 74 (`jt jenkins` appends a newline). Then run `b`.
- **Back to the job.**
  - Run `$J keys RUN b && $J crumb RUN 'Frontend.*mobile-app' && $J wait RUN '│ #[0-9]+ │'`.
- **Proof.**
  - Run `$J snap RUN job-page` on the job page; retain the earlier
    `build-page` snap from the actual build page.
  - Cross-check the build's real result with
    `$J jenkins RUN '/job/DevTeam/job/Frontend/job/mobile-app/<N>/api/json?tree=result,duration'`.

## Gotchas

- **Stale Builds column (suspected app bug, seen 2026-10-03):** after a
  trigger from the job page, the new build appears in the stage grid
  (`#13` label with a spinner), but the left `Builds` column keeps showing
  the previous top build for at least 25s. The selection also stays on the
  old build, so `x` and `L` act on it. Leaving the page and reopening it
  fixes the column. Wait for `#N` anywhere on screen, not for `│ #N │`.
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
