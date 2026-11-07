# Job and build pages

The job page shows a job's health and recent builds, with each build's
stages laid out as a grid. Opening a build shows its metadata, its artifacts,
and its stages, each with a status and duration.

## Sub-features

- `job-header`: the job name, `Health Score: N%` (or
  `No health report available`), and the build stability text.
- `job-builds`: a `Builds` column of `#N` boxes, newest first. A selected
  build shows its date and duration.
- `job-stages`: for each build, the stage boxes show the stage name and
  duration (for mobile-app: `Checkout`, `Dependencies`, `Lint`,
  `Unit Tests`, `Build iOS`, and more).
- `job-nav`: `j`/`k` move between builds. `B`/`S` focus the builds pane or
  the stages pane.
- `build-open`: Enter opens `Build #N` with `Started`, `Duration`,
  `Status`, `Started by`, `Details`, `Node`, and `Artifacts`.
- `build-stages`: on the build page, `h`/`l` move the highlighted stage
  (drawn with a double border). `ctrl+l` opens that stage's step log.
- `build-step-log`: `ctrl+l` opens the selected stage's step log. It
  shows only that stage's output, and the footer **stays** at
  `…mobile-app  N` (no `console`).
- `build-help`: `?` expands the footer into the full key list for the page.
- `build-params`: `P` shows the build parameters (parameterized jobs only).

## How to get to it (user POV)

- Global search result → Enter.
- Folder listing row → Enter.
- Favorites row → Enter.
- My Runs row: Enter opens the build, and `ctrl+j` opens the job.
- From a build page, `ctrl+j` returns to its job.

## Driving it with jt

Preconditions:

- The instance is ready. `DevTeam/Frontend/mobile-app` has at least one
  completed build. Check with
  `$J jenkins RUN '/job/DevTeam/job/Frontend/job/mobile-app/api/json?tree=lastCompletedBuild[number]'`.

Steps:

- **Open the job.**
  - Follow `search-open-job` in
    [search-and-browse.md](./search-and-browse.md).
  - Wait for `$J wait RUN '│ #[0-9]+ │'` before pressing any key. The
    `Builds` column lists `#<lastBuild>` first (a stale column is possible;
    see Gotchas).
- **Inspect the stages.**
  - Run `$J screen RUN | grep -E 'Checkout|Unit Tests'`.
  - Stage names appear in the grid.
- **Move the selection.**
  - Run `$J keys RUN j && sleep 0.5`. There is no stable text to wait on
    here; this is the one place a short sleep is acceptable.
  - The selected build changes to the second-newest build. Its date and
    duration lines appear next to it.
- **Open the build.**
  - Run `$J keys RUN Enter && $J crumb RUN 'mobile-app.*<N>' && $J wait RUN 'Build #<N>'`.
  - The page shows `Status: Success` (or the build's real result), and the
    footer ends with that build number.
- **Move between stages.**
  - Run `$J keys RUN l`.
  - The double-bordered box moves from `Checkout` to `Dependencies`.
- **Switch stage and open its step log at once.**
  - From the build page with `Checkout` selected, run
    `$J keys RUN l C-l && $J wait RUN 'Loaded:'`. Both keys go in one
    send, before the new stage's details arrive from Jenkins.
  - The log shows the **Dependencies** output (`Installing pods/gradle deps...`),
    never Checkout's (`Checking out source...`). If you press `ctrl+l` before
    the details have loaded, the footer may first say
    `Loading Dependencies stage…`; press it again. Then run `b` and `h` to
    return to Checkout. This guards #119.
- **Step log.**
  - Run `$J keys RUN C-l && $J wait RUN 'Loaded:'`.
  - The screen shows the Dependencies stage output
    (`Installing pods/gradle deps...`). Cross-check with
    `$J jenkins RUN '/job/DevTeam/job/Frontend/job/mobile-app/<N>/consoleText' | grep -c 'Installing pods'`.
  - Then run `$J keys RUN b && $J wait RUN 'Build #<N>'`.
- **Full log from the build page.**
  - Run `$J keys RUN L && $J crumb RUN '<N>.*console' && $J wait RUN 'Loaded:'`.
  - For #557, the status line reads `Loaded: 1-73/73`, and `jt jenkins …/consoleText | wc -l`
    gives 74 (`jt jenkins` appends a newline). Then run `b`.
- **Back to the job.**
  - Run `$J keys RUN b && $J wait RUN 'Health Score|No health report available'`.
- **Proof.**
  - Run `$J snap RUN job-page` and `$J snap RUN build-page`.
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
- The job page animates (spinners) while a build runs. `snap` captures a
  single frame, so wait for text, not for a spinner glyph.
- The `.ansi` snap is the only place where status colors and the selection
  highlight are visible. The plain `.txt` loses them.
