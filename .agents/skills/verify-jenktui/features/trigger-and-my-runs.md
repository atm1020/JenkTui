# Trigger a build and My Runs

The user triggers a build from JenkTui and confirms it. JenkTui then tracks
that run live: queued, then in progress, then finished. My Runs lists every
build the user triggered on this server, with live status, and can open,
replay, or delete them.

## Sub-features

- `trigger-feedback`: `ctrl+b` on a job or build page opens the confirmation
  or form at once, because those pages already hold the job. My Runs knows the
  job by URL only, so it has to ask: the row the user acted on shows a spinner
  and `Triggering` in its status cell while the job resolves, and no dialog
  appears until the answer is in. The trigger dialog is therefore only ever
  seen at its finished size, and the footer does not show the wait. The
  animation lasts roughly 100-150 ms against local Jenkins — `snap`/`screen`
  are too slow to land inside it, so capture it with a tight loop started
  before the keypress:
  `for i in $(seq 400); do tmux -L jtv-RUN capture-pane -p -t jt > f$i.txt; done`
  started ~80 ms ahead of `$J keys RUN C-b`. Proved 2026-11-08 in
  `dist/verify/trig-a/frames-myruns/` (~20 frames, spinner glyph advancing).
- `trigger-size`: the confirmation is sized by its question, so it stays close
  to the text and does not stretch to the width used by lists and the parameter
  form. The same job confirms at the same width from a job, a build, and My
  Runs. A longer job name makes a wider box; the terminal width does not.
- `confirm-buttons`: `Yes` and `No` sit together on one line against the
  dialog's right edge, the same distance from it at every dialog width and
  question length. This applies to every confirmation, not just triggers.
- `trigger-confirm`: `ctrl+b` on a job page asks
  `Are you sure you want to trigger…`. `y` confirms, and `n`/`esc` cancel
  without side effects.
- `trigger-toast`: a confirmed trigger shows `Build triggered` in the footer.
- `trigger-live`: the new build appears in the job page's stage grid
  with an animated spinner. The left Builds column can stay stale (see
  [job-and-build.md](./job-and-build.md)); report that as a product issue.
- `myruns-row`: `m` opens `My Runs`. The triggered run shows `In progress`,
  then its final status (`Success` for mobile-app) without a manual refresh.
- `myruns-persist`: the run is a row in `runs.db` table `runs`
  (`server_id='local'`), and its trigger intent is resolved in
  `trigger_intents`.
- `myruns-actions`: Enter opens the build, `ctrl+j` opens the job immediately
  in its loading state, `L` the log, `r` replay,
  `ctrl+b` re-trigger, `d` delete the row, `/` filter, `s`/`S` sort,
  `t` open the filter editor focused on Status (Up/Down selects), and
  `R` reset.

## How to get to it (user POV)

- Trigger: `ctrl+b` on a job page, on a build page, or on a My Runs row.
- My Runs: `m` from any page.

## Driving it with jt

Preconditions:

- The instance is freshly started, so My Runs is empty.
- Nobody else is building `DevTeam/Frontend/mobile-app`.
- Record the baseline with
  `$J jenkins RUN '/job/DevTeam/job/Frontend/job/mobile-app/api/json?tree=lastBuild[number],inQueue'`.
  Call `lastBuild.number` **B**. The new build should be **B+1**.

Steps:

- **Open the job.**
  - Run `$J keys RUN C-p && $J wait RUN 'Global Search' && $J type RUN mobile-app && $J keys RUN Enter && $J wait RUN '1 item'`.
  - Then run `$J keys RUN Enter && $J crumb RUN 'Frontend.*mobile-app' && $J wait RUN '│ #[0-9]+ │'`.
  - Then run `$J snap RUN trigger-before`.
- **Ask to trigger.**
  - Run `$J keys RUN C-b && $J wait RUN 'Are you sure you want to trigger'`.
  - The dialog appears on the keypress, with no intermediate frame: this page
    already holds the job. Do not expect any loading text in the footer.
  - Compare this snap's dialog border against the build-page and My Runs
    trigger snaps below: the same job confirms at the same width from all
    three, and the box hugs its question rather than spanning the terminal.
    `TestResolvedPagesTriggerWithoutAnyWait` pins the request-free open;
    the snaps show the sizing against a real terminal and job name.
- **Confirm.**
  - Run `$J keys RUN y && $J wait RUN 'Build triggered' 10`.
  - Then run `$J snap RUN trigger-toast`.
- **Live build on the job page.**
  - Run `$J wait RUN '#B+1' 30`, putting the literal number in the regex.
  - The new build appears as a stage-grid row with a spinner. The left
    `Builds` column may stay stale; see Gotchas in
    [job-and-build.md](./job-and-build.md). Do not wait on `│ #B+1 │`.
- **My Runs while running.**
  - Run `$J keys RUN m && $J wait RUN 'My Runs' && $J wait RUN 'In progress' 30`.
  - Then run `$J snap RUN myruns-running`.
- **My Runs → job.**
  - Wait for the actual row (`$J wait RUN '#<B+1>.*In progress'`, substituting
    the build number), not just the `My Runs` header or job-name filter.
  - Run `$J snap RUN myruns-job-before && $J keys RUN C-j`.
  - Run `$J crumb RUN 'Frontend.*mobile-app' && $J wait RUN '│ #[0-9]+ │'`
    and `$J snap RUN myruns-job-after`.
  - The first frame can be `Loading job...`; wait for build boxes before
    sending job actions. Press `m`, wait for the breadcrumb and actual row,
    then continue observing completion.
- **Completion tracked.**
  - Run `$J wait RUN 'completed: Success' 90` after observing `In progress`.
    mobile-app takes about 26s, plus queue time and the 3s poll. If the run
    was first observed already terminal, verify its terminal row and DB
    instead: that initial observation does not generate a completion toast.
  - The same row now reads `#B+1   Success   <date>  ~29s`, and the footer
    toast reads `Build #B+1 completed: Success`.
  - Then run `$J snap RUN myruns-done`.
- **Store side effects.**
  - Run `$J db RUN "select job_name,build_id,status,server_id from runs"`.
    It should return one row for mobile-app with build `B+1`, status
    `SUCCESS`, and server `local`.
  - Run `$J db RUN "select state,queue_id,build_id from trigger_intents"`.
    The intent should have a queue ID and build `B+1`.
- **Jenkins side effect.**
  - Run `$J jenkins RUN '/job/DevTeam/job/Frontend/job/mobile-app/lastBuild/api/json?tree=number,result,building'`.
  - The build should be `number = B+1`, `result = "SUCCESS"`, and
    `building = false`.
- **Build-page trigger (required).**
  - From the ready job page, run `$J keys RUN Enter && $J wait RUN 'Build #[0-9]+'`.
  - Run `$J snap RUN build-trigger-before && $J keys RUN C-b && $J wait RUN 'Are you sure you want to trigger'`.
  - Run `$J snap RUN build-trigger-dialog && $J keys RUN y && $J crumb RUN 'Build triggered'`.
  - Run `$J snap RUN build-trigger-after`. This entry point returns to the job.
- **My Runs trigger (required).**
  - Run `$J keys RUN m && $J crumb RUN 'My Runs' && $J wait RUN '#[0-9]+'`.
  - Run `$J snap RUN myruns-trigger-before && $J keys RUN C-b`.
  - This row has to resolve its job first. If the server is slow enough, the
    selected row's status reads `Triggering` with a spinner before the dialog
    opens; snap it if you catch it. Then run
    `$J wait RUN 'Are you sure you want to trigger'`. No loading box appears
    over the list. `TestMyRunsShowsTheWaitOnItsRowThenOpensTheDialog` pins the
    row animation, the single resolve request, and the dialog that follows.
  - Run `$J snap RUN myruns-trigger-dialog && $J keys RUN y && $J crumb RUN 'Build triggered'`.
  - Run `$J snap RUN myruns-trigger-after`. Repeat the store and Jenkins queries
    above: each confirmation creates a separate intent/queue receipt. Wait for
    the newest row to resolve, not just an older row's `Success` text.
- **Parameterized and dynamic triggers (required).**
  - Repeat all three trigger entry points with `deploy-service`: wait for
    `Build deploy-service`, snapshot the form, submit with `C-s`, then verify
    `ENVIRONMENT` in Jenkins and each intent in `runs.db`. See
    [build-actions.md](./build-actions.md) for editing the value.
  - Open `cascade-infra-4-level` through search. Snapshot before `C-b`, wait
    for `Build cascade-infra-4-level`, then `aws-us-east-1-k8s-01` and `default`.
  - Snapshot resolved choices, then run `$J keys RUN Enter && $J wait RUN 'gcp'`
    and `$J keys RUN Down Enter && $J wait RUN 'gcp-us-central1-k8s-01'`.
    Wait for `default`, snapshot again, submit with `C-s`, and wait for
    `Build triggered`. Open My Runs and wait for that run's success.
  - Verify Jenkins parameters `CLOUD=gcp`, `REGION=us-central1`,
    `CLUSTER=gcp-us-central1-k8s-01`, `NAMESPACES=default`, plus its resolved
    intent and `SUCCESS` row. This proves form loading and cascading choices
    still work after reusing the already-polled parameter definitions.
- **Cancel path (optional).**
  - On the job page, run `$J keys RUN C-b && $J wait RUN 'Are you sure' && $J keys RUN n`.
  - Expect `$J crumb RUN 'Build trigger aborted'`; Jenkins
    `lastBuild.number` and the trigger-intent count remain unchanged.

## Gotchas

- Resolved job/build pages reuse their loaded parameter definitions without
  another job GET. My Runs resolves its URL once (one job GET, not two).
  Reopen a page to pick up externally changed parameter definitions; dynamic
  choice values still resolve through the live build form.
- A job-name match can come from My Runs' filter line before any row is
  loaded. Wait for `#<build number>` in a row before `ctrl+j` or Enter; an
  early key is ignored. Likewise, an old breadcrumb can briefly remain during
  navigation, so also wait for destination content.
- Every confirmed trigger runs a real build on the Docker Jenkins. That is
  expected, but do not loop this recipe without need.
- Status words on screen are title case (`In progress`, `Success`). In
  `runs.db` and the Jenkins API they are upper case (`SUCCESS`).
- Pressing `m` **from a job page** opens My Runs already filtered to that
  job (header `Name: DevTeam … mobile-app | Order: Date ↓`). Pressing `m`
  from home shows all runs. `R` resets the filter. Drive both entry points
  if the filter is under test.
- `wait 'Success'` can match an older build elsewhere on screen. Wait on it
  only while the My Runs view is open on a fresh RUN, where the triggered
  run is the only row.
- If `In progress` never appears but the run reaches `Success`, the build
  finished between polls. That still counts as tracked. Note it with
  `$J note` instead of failing the run.
- Desktop completion notifications are off in the harness config. To verify
  them, start with
  `JT_CONFIG_JSON='{"poll_interval":3,"notification":{"desktop_enabled":true}}'`
  and observe the host popup as well as `completion_outbox.delivered_at`.
  A delivered DB acknowledgement alone does not prove desktop delivery.
