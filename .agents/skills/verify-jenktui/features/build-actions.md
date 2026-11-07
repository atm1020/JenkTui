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
- `params-error`: if Jenkins rejects the request, a `Server Error` dialog
  appears and the log records `Failed to trigger build`. `esc` closes it.
- `artifacts-select`: `d` on a build page opens `Download artifacts`, a
  list of `[ ] name` entries with `N of M selected`. `space` toggles an
  entry, Enter downloads the selected ones, `a` downloads all, and `D` on
  the build page downloads everything without the dialog.
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
  11s), `DevTeam/Backend/artifact-generator` (5 artifacts, last success
  #26), and `Ops/Monitoring/long-running-scan` (runs long enough to cancel).

Steps:

- **Parameterized trigger.**
  - On deploy-service, run `$J keys RUN C-b && $J wait RUN 'Build deploy-service'`.
    The `ENVIRONMENT` block shows `> staging`.
  - Clear the value with `BSpace` (send it 20 times with
    `$J keys RUN BSpace BSpace ...`), then run `$J type RUN verify-env`.
    The block shows `> verify-env`. Run `$J snap RUN params-filled`.
  - Run `$J keys RUN C-s && $J crumb RUN 'Build triggered' 10`.
  - Proof: `$J jenkins RUN '/job/Ops/job/Deployments/job/deploy-service/<B+1>/api/json?tree=result,actions%5Bparameters%5Bname,value%5D%5D'`
    lists `ENVIRONMENT = verify-env`.
- **Artifacts.**
  - On artifact-generator, run `$J keys RUN Enter && $J wait RUN 'Build #26'`.
    The `Artifacts` list shows 5 numbered files.
  - Run `$J keys RUN d && $J wait RUN 'Download artifacts'`.
  - Run `$J keys RUN j j Space && $J wait RUN '\[x\] coverage.csv'`.
  - Run `$J keys RUN Enter && $J crumb RUN 'Artifacts saved to' 10`.
  - Proof: compare the downloaded file with Jenkins's copy:
    `cmp <(curl -s -u admin:admin http://127.0.0.1:8080/job/DevTeam/job/Backend/job/artifact-generator/26/artifact/build/reports/coverage.csv) "$(find ${TMPDIR:-/tmp}/jenktui-verify/RUN/home/.local/share/JenkTui/artifacts -name coverage.csv)"`.
    Then copy the file into `dist/verify/RUN/artifacts/` **before** `down`.
- **Replay.**
  - On the artifact-generator build page, run `$J keys RUN r && $J crumb RUN 'mainScript.groovy'`.
    The pipeline source shows with line numbers.
  - Run `$J keys RUN r && $J crumb RUN 'Build triggered, polling started' 10`.
  - Proof: `$J jenkins RUN '/job/DevTeam/job/Backend/job/artifact-generator/<B+1>/api/json?tree=result,actions%5Bcauses%5BshortDescription%5D%5D'`
    lists `Replayed #26`, and
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
- **Seed bug: `QA/all-param-types` cannot be triggered.** Its `REGION`
  choice parameter has no choices in Jenkins (`choices: []`; the seed's
  `choiceParam` XML is not accepted). Jenkins returns `500` for the same
  request even without JenkTui. The dialog correctly shows
  `no choices`. Use it only to inspect how parameter types render.
- After a trigger, the job page's selection stays on the **previous** build
  (stale Builds column; see [job-and-build.md](./job-and-build.md)). If
  you press `x` right away, it targets the old, finished build and does
  nothing visible. Move the selection first.
- Every action here runs or aborts real builds on the Docker Jenkins. Use
  short jobs, and do not loop.
- `jt type` passes `--` to tmux, so text that starts with `-` is sent as
  text. Remember this only if you call tmux directly.
