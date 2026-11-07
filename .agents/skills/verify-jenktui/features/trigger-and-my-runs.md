# Trigger a build and My Runs

The user triggers a build from JenkTui and confirms it. JenkTui then tracks
that run live: queued, then in progress, then finished. My Runs lists every
build the user triggered on this server, with live status, and can open,
replay, or delete them.

## Sub-features

- `trigger-confirm`: `ctrl+b` on a job page asks
  `Are you sure you want to trigger…`. `y` confirms, and `n`/`esc` cancel
  without side effects.
- `trigger-toast`: a confirmed trigger shows `Build triggered` in the footer.
- `trigger-live`: the new build appears at the top of the job page's
  `Builds` column, with an animated spinner while it runs.
- `myruns-row`: `m` opens `My Runs`. The triggered run shows `In progress`,
  then its final status (`Success` for mobile-app) without a manual refresh.
- `myruns-persist`: the run is a row in `runs.db` table `runs`
  (`server_id='local'`), and its trigger intent is resolved in
  `trigger_intents`.
- `myruns-actions`: Enter opens the build, `L` the log, `r` replay,
  `ctrl+b` re-trigger, `d` delete the row, `/` filter, `s`/`S` sort,
  `t` cycle the status filter, and `R` reset.

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
  - A confirmation dialog appears.
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
- **Completion tracked.**
  - Run `$J wait RUN 'completed: Success' 90`. mobile-app takes about 26s,
    plus queue time and the 3s poll.
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
- **Cancel path (optional).**
  - On the job page, run `$J keys RUN C-b && $J wait RUN 'Are you sure' && $J keys RUN n`.
  - No toast appears, and Jenkins `lastBuild.number` is unchanged.

## Gotchas

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
  and check that `completion_outbox.delivered_at` is set. A popup will appear
  on the host.
