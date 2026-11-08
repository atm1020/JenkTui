# Build actions: parameters, artifacts, replay, cancel

From a job page or a build page, the user can trigger a parameterized build
through a typed form, download artifacts, replay a pipeline script, and
cancel a running build. Every action has an observable Jenkins side effect.

## Sub-features

- `params-dialog`: `ctrl+b` on a parameterized job opens
  `Build <job>`, a form with one block per parameter. Each block shows the
  name, the type (`string`, `text`, `bool`, `choice`, `password`, ...), the
  description, and the value.
- `params-submit`: `ctrl+s` submits. The footer shows `Build triggered`,
  and the values reach Jenkins.
- `params-error`: if Jenkins rejects the request, a `Failed to trigger build` dialog
  appears and the log records `Failed to trigger build`. `esc` closes it.
- `artifacts-select`: `d` on a build page opens `Download artifacts`, a
  list of `[ ] name` entries with `N of M selected`. `space` toggles an
  entry, Enter downloads the selected ones, `a` downloads all, and `D` on
  the build page downloads everything without the selector. After saving,
  a confirmation asks `Downloaded N artifact(s). Open folder?`; use `n`
  to dismiss without launching the host file manager.
- `artifacts-saved`: the toast reads `Artifacts saved to <path>`. Files land
  under `<data>/artifacts/<server-id>/<job-path-with-underscores>/<build>/`
  with mode `0600`.
- `replay-view`: `r` on a job page or build page opens the script view
  with numbered pipeline source. The footer ends with `mainScript.groovy`.
- `replay-run`: in the script view, `r` replays. The toast reads
  `Build triggered, polling started: <job>`, Jenkins records the cause
  `Replayed #N`, and the run appears in My Runs.
- `replay-edit`: `e` opens `$EDITOR` on the script. This is not verified
  here, because it takes over the pane.
- `cancel`: `x` asks `Are you sure you want to cancel build #N?`, and `y`
  aborts it. The toast reads `Build #N cancelled successfully`, Jenkins
  reports `ABORTED`, and My Runs gets the `ABORTED` row.

## How to get to it (user POV)

- Params: `ctrl+b` on a job page, a build page, or a My Runs row of a
  parameterized job.
- Artifacts: `d` or `D` on a build page.
- Replay: `r` on a job page (uses the selected build), a build page, or a My
  Runs row.
- Cancel: `x` on a job page (uses the selected build), a build page, or a My
  Runs row.

## Driving it with jt

Preconditions:

- A fresh RUN. Use the canonical "open a job" sequence from `SKILL.md` for
  each job below, ending on `$J wait RUN '│ #[0-9]+ │'`.
- Jobs: `Ops/Deployments/deploy-service` (string param `ENVIRONMENT`, about
  11s), `DevTeam/Backend/artifact-generator` (5 artifacts), and
  `Ops/Monitoring/long-running-scan` (runs long enough to cancel).
- Discover the actual completed artifact build number **N** and newly
  triggered build IDs from Jenkins; historical #26 and B+1 assumptions
  are not reliable when another client can build.

Steps:

- **Parameterized trigger.**
  - On deploy-service, run `$J keys RUN C-b && $J wait RUN 'Build deploy-service'`.
    The `ENVIRONMENT` block shows `> staging`.
  - Clear the value with `BSpace` (send it 20 times with
    `$J keys RUN BSpace BSpace ...`), then run `$J type RUN verify-env`.
    The block shows `> verify-env`. Run `$J snap RUN params-filled`.
  - Run `$J keys RUN C-s && $J crumb RUN 'Build triggered' 10`.
  - Substitute the correlated build ID for `<NEW>`:
    `$J jenkins RUN '/job/Ops/job/Deployments/job/deploy-service/<NEW>/api/json?tree=result,actions%5Bparameters%5Bname,value%5D%5D'`
    lists `ENVIRONMENT = verify-env`.
- **Artifacts.**
  - On artifact-generator, run `$J keys RUN Enter`, wait for the actual
    `Build #N` and `coverage.csv` before acting. The header alone can appear
    while the page is still loading.
  - Run `$J keys RUN d && $J wait RUN 'Download artifacts'`.
  - Run `$J keys RUN j j Space && $J wait RUN '\[x\] coverage.csv'`.
  - Run `$J keys RUN Enter && $J wait RUN 'Downloaded 1 artifact' 10`.
    Snap that confirmation, then `$J keys RUN n` and confirm the footer
    `Artifacts saved to`.
  - Proof: GET `/job/DevTeam/job/Backend/job/artifact-generator/N/artifact/build/reports/coverage.csv`
    with `$J jenkins RUN`, then compare bytes with the downloaded file
    (remove the one newline `jt jenkins` appends). Verify mode `0600`.
    Then copy the file into `dist/verify/RUN/artifacts/` **before** `down`.
- **Replay.**
  - On the artifact-generator build page, run `$J keys RUN r && $J crumb RUN 'mainScript.groovy'`.
    Wait for `$J wait RUN 'pipeline'` before replaying; the footer alone
    can appear before source loads.
  - Run `$J keys RUN r && $J crumb RUN 'Build triggered, polling started' 10`.
  - Substitute the replay's correlated build ID for `<NEW>`:
    `$J jenkins RUN '/job/DevTeam/job/Backend/job/artifact-generator/<NEW>/api/json?tree=result,actions%5Bcauses%5BshortDescription%5D%5D'`
    lists `Replayed #N`, and
    `$J db RUN "select build_id,status from runs where job_name like '%artifact-generator%'"`
    reaches `SUCCESS`.
- **Cancel.**
  - On long-running-scan, trigger with `C-b`, then `y`, then wait for
    `Build triggered`.
  - Poll Jenkins until `…/lastBuild/api/json?tree=number,building` shows
    `building: true`.
  - Move the selection onto the new build with `$J keys RUN k k k k`. The
    stage grid's `┃` marker should sit next to `#<new>`.
  - Run `$J keys RUN x && $J wait RUN 'Are you sure you want to cancel build #<new>'`.
  - Run `$J keys RUN y && $J crumb RUN 'cancelled successfully' 10`.
  - Proof: Jenkins `…/<new>/api/json?tree=result` returns `ABORTED`, and
    `$J db RUN "select build_id,status from runs where job_name like '%long-running%'"`
    returns `<new> ABORTED`.

Verified 2026-10-03 (`dist/verify/proof-pages/`): deploy-service #13
`ENVIRONMENT=verify-env` SUCCESS, a byte-identical artifact download,
artifact-generator #27 replayed from #26, and long-running-scan #113
ABORTED.

## Gotchas

- **App bug: string and text parameters cannot contain `j`, `k`, or `?`.**
  The dialog's navigation and help keys (`BuildParams.Down/Up`,
  `HelpToggle`) are matched before the focused field sees the key
  (`src/ui/components/dialogs/buildparams/buildparams.go` `handleKeyPress`).
  Typing `-jtv` produces `-tv`. Avoid those letters in values unless the bug
  itself is under test.
- **Stale fixture:** older seeded `QA/all-param-types` jobs can have
  `REGION` with `choices: []`, causing Jenkins `500` on submit. This was
  still present in the local instance on 2026-11-08; the dialog showed
  `no choices` and then `Failed to trigger build`. The current seeder fixes
  choice XML and updates existing jobs. Inspect the parameter definitions
  before using this fixture; reseed when appropriate rather than assuming
  all current installations reproduce the rejection.
- After a trigger, the job page's selection stays on the **previous** build
  (stale Builds column; see [job-and-build.md](./job-and-build.md)). If
  you press `x` right away, it targets the old, finished build and warns
  that the build has already finished. Move the selection first.
- Every action here runs or aborts real builds on the Docker Jenkins. Use
  short jobs, and do not loop.
- `jt type` passes `--` to tmux, so text that starts with `-` is sent as
  text. Remember this only if you call tmux directly.
