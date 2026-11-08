---
name: verify-jenktui
description: Launch and drive the real JenkTui terminal UI against the repo's local Docker Jenkins, in an isolated $HOME and a private tmux server, to prove a feature or fix works the way a user would see it (screens, keypresses, runs.db rows, Jenkins state). Use after changing UI, polling, store, trigger, or log-viewer code, or whenever a claim about JenkTui behavior needs live evidence rather than unit tests.
---

# Verify JenkTui

JenkTui is a full-screen Bubble Tea TUI. The only user surface is the terminal,
so you drive it by sending keys to a tmux pane and reading the pane back as
text. Everything goes through `jt`, a Go command in `dev/jt/`. Build it once
per session from the repo root, then call the binary:

```bash
go build -o dist/bin/jt ./dev/jt && J=dist/bin/jt   # dist/ is gitignored
```

`$J --help` lists every command and environment override, and
`$J COMMAND --help` gives details and examples. Usage errors exit 2 with a
correct example invocation; timeouts, a dead app, and failed checks exit 1.
Rebuild after pulling changes to `dev/jt/`.

Each command takes a **RUN id** that you choose (for example `fav-fix`). A RUN
gets its own binary build, its own `$HOME` (so its config, `runs.db`, logs, and
cache are separate), its own tmux socket (`tmux -L jtv-<RUN>`), and its own
evidence folder. Several RUNs can run side by side. Your real
`~/.config/JenkTui` and `~/.local/share/JenkTui` are never touched. This works
because on Linux, `src/config/dirs.go` builds every directory from
`os.UserHomeDir()`, which reads `$HOME`.

## Launch

Prerequisites: `go`, `tmux`, and Docker running the local
Jenkins LTS on `127.0.0.1:8080` (admin/admin) with the seed data loaded:

```bash
task jenkins:lts        # starts container jenkins-lts on 127.0.0.1:8080 (first boot ~1-2 min)
task seed               # creates/updates seed folders and jobs; preserves build history
```

Then start one instance:

```bash
$J up demo
```

`up` does the following:

- refuses any Jenkins URL that is not localhost
- checks that Jenkins answers HTTP 200 with the configured credentials
- runs `go build` into `${TMPDIR:-/tmp}/jenktui-verify/<RUN>/jenktui`
- writes `server.json` (server id `local`, name `Local`) and `config.json`
  (`poll_interval: 3`, desktop and sound notifications off)
- starts the app in a 160x45 tmux pane

It treats the app as **ready** when the home splash shows `Jenkins  2.x.y`.
That line only appears after an authenticated call to Jenkins succeeds. With
`JT_SECOND_SERVER=1` the app instead opens the startup server picker
(`Select Jenkins server`), which also counts as ready. `up` prints
`ready RUN=demo state=home|server-picker evidence=...`, or exits non-zero and
dumps the screen. After a failed `up`, run `down`.

Options:

- `JT_COLS`/`JT_ROWS` change the terminal size.
- `JT_CONFIG_JSON='{"theme":"Nord"}'` replaces config.json. Fields you leave
  out fall back to defaults. If you set `JT_CONFIG_JSON`, also set
  `"notification":{"desktop_enabled":false}` unless you really want a desktop
  popup.
- `JT_SECOND_SERVER=1` adds a second server (`local-b`, "Local B", same
  Jenkins) so the server selector has something to switch to.
- `JT_JENKINS_URL=http://127.0.0.1:8081` with `task jenkins:2479` and
  `task seed:2479` targets Jenkins 2.479.
- Private-CA HTTPS: run `task certs` and `task jenkins:tls`, then export
  `JT_JENKINS_URL=https://localhost:8443` and
  `JT_CA_CERT="$PWD/dev/docker/tls/ca.pem"` before `up`. Keep these exports
  for `doctor` and `jenkins` too: those commands use the current environment,
  not the RUN's saved connection. `JT_CA_CERT` becomes `ca_cert_path` in
  the isolated server entry. See [server setup](features/server-selector.md).

Run `$J doctor demo` before the first drive. Teardown is `$J down demo`; see Cleanup.

## Doctor

Run before the first drive, after a restart, and after any failed drive.
It is read-only:

```bash
$J doctor demo
```

It checks that:

- the private tmux session exists
- the app process is alive (`pane_dead=0`)
- the pane is running **this RUN's binary** (`/proc/<pid>/exe`)
- the isolated HOME and its log exist
- `jenktui.log` has no `panic` (it also prints the `ERRO` count as info: the
  router logs every history eviction at `ERRO`, so `ERRO` lines alone are
  normal)
- Jenkins auth works
- the seed job `DevTeam/Frontend/mobile-app` exists

It also prints the build stamp (`git describe --dirty`) and the app's own
`Starting JenkTui version=...` line. It exits non-zero on any `FAIL`.

If doctor fails on "pane runs our binary" or "process still running", the
instance is not worth driving. Read `$J log demo 80` and `$J screen demo`, then
run `down` and `up` again. If the process is healthy but the UI state is
unexpected, reset to a known view or relaunch before continuing.

## Drive

| Command | Use |
|---|---|
| `$J keys RUN K...` | tmux key names: `Enter` `Escape` `Tab` `Up` `Down` `C-p` `C-b` `C-s` `C-t` `C-r` `C-c`, or single characters like `b` `F` `L` |
| `$J type RUN 'text'` | literal text, for search boxes (no key-name parsing) |
| `$J wait RUN 'ERE' [secs]` | poll until the regex matches the screen (default 20s); on timeout, dumps the screen and exits 1 |
| `$J crumb RUN 'ERE' [secs]` | like `wait`, but matches only the footer row (breadcrumb + toast). Use it to prove **which page is active** |
| `$J screen RUN` | print the current screen |

**Always wait for the next view before sending the next key. Never use
`sleep` instead.** `wait` uses Go regular expressions over the entire screen;
use `(?m)` if `^` or `$` must match individual lines. A header or breadcrumb
under a dialog does not prove that dialog closed. Data loads asynchronously;
keys sent too early are dropped or land on the previous view. Two traps the
proof runs hit:

- Dialogs draw over the old page, so the old page's text stays on screen. A
  plain `wait 'Health Score'` can match the page you just left. **Use
  `crumb` for the page identity**, then `wait` for page content.
- The job page drops every key while it is loading, and its `Health Score`
  header can render before loading finishes. For a job with builds, wait
  for build boxes: `$J wait RUN '│ #[0-9]+ │'`. A never-built job instead
  becomes ready at `No pipeline runs found`. Folder pages are ready at
  `N items`. The build page also drops keys during loading: after its
  breadcrumb/header, wait for loaded content such as `Stage ID:` on a
  completed pipeline before sending actions.

The canonical "open a job" sequence:

```bash
$J keys RUN C-p && $J wait RUN 'Global Search' && $J keys RUN C-r && $J wait RUN 'Global Search' \
  && $J type RUN mobile-app && $J keys RUN Enter && $J wait RUN '‣ .*mobile-app' \
  && $J keys RUN Enter && $J crumb RUN 'Frontend.*mobile-app' && $J wait RUN '│ #[0-9]+ │'
```

Stable handles to wait on:

- **Footer breadcrumb** (bottom row): it shows `👤 admin` followed by the path,
  e.g. `DevTeam  Frontend  mobile-app  557  console`. Nerd-font icons sit
  between the parts, so match the words with `.*` between them, not exact
  spacing.
- **Footer toast** (right side of the bottom row): e.g. `Added to favorites`,
  `Build triggered`.
- **View headers**: `Global Search` (the search box placeholder; it
  disappears once a query is typed, so wait for it only right after `C-p`
  or `C-r`), `Health Score` or
  `No health report available` (job page), `Build #<n>` (build page),
  `Loaded: a-b/N | Line: x | Scroll: y%` (log viewer), `Jobs (n)   Folders (n)`
  (Favorites), `My Runs`.

Default keys (from `getDefaultKeyMapConfig` in `src/config/loader.go`;
active dialogs and text-input modes can intercept global keys):

- **Global:** `ctrl+p` search, `b` back, `]` forward, `m` My Runs,
  `f` Favorites, `ctrl+s` server, `ctrl+t` theme, `?` help, `ctrl+c` quit
- **Job:** `enter` build, `L` log, `ctrl+b` trigger, `r` replay, `x` cancel,
  `F` favorite, `B`/`S` builds/stages pane
- **Build:** `h`/`l` stage, `ctrl+l` step log, `L` full log, `P` params,
  `d`/`D` artifacts, `ctrl+j` job

The home view has **no job list**. The way into jobs and folders is global
search (`C-p`) or Favorites/My Runs.

Back history holds only **3** views (`maxHistorySize` in
`src/ui/router/router.go`), so `b` cannot always reach home. If you need
home again, run `down` and `up`.

Closing dialogs: search, theme, and server dialogs close with `ctrl+c`
(inside a dialog, it closes only the dialog). Build params and artifacts
close with `esc`, as do the confirm and error dialogs. Outside a dialog,
`ctrl+c` **quits the app**. Run `doctor` after any `ctrl+c` you are unsure
about.

`type` sends text exactly, but it does not bypass the app's key routing:
in the build-params dialog, `j`, `k`, and `?` are navigation keys and never
reach the text field (see [features/build-actions.md](features/build-actions.md)).

The feature map in [`features/`](features/README.md) has a recipe for each
user-facing feature. Before claiming a feature works, read its file and drive
every entry point it lists.

## Watching a run

A human can watch any RUN live from another terminal, without disturbing it:

```bash
tmux -L jtv-<RUN> attach -r -t jt     # -r: read-only; detach with Ctrl-b d
```

`up` pins the window at `JT_COLS`x`JT_ROWS`, so a smaller viewer terminal
crops the view instead of resizing the app. Use a viewer terminal of at least
160x45 to see the whole screen.

## Evidence

| Command | Writes |
|---|---|
| `$J snap RUN NAME` | `NAME.txt` (plain screen) and `NAME.ansi` (with colors: `cat` it in a terminal, or `less -R`) |
| `$J note RUN 'text'` | a line in `transcript.log` |
| `$J db RUN 'SQL'` | runs a read-only query on the instance's `runs.db` and copies the output into the transcript |
| `$J jenkins RUN /path/api/json` | GET from the local Jenkins (use `-g`-style brackets such as `?tree=lastBuild[number,result]`) |
| `$J log RUN [N]` | tail of `jenktui.log` (has ANSI colors) |

Evidence lives in **`dist/verify/<RUN>/`** (gitignored; set `JT_EVIDENCE_ROOT`
to change it). It contains `transcript.log`, which timestamps every
key/type/wait/snap/db, plus `build.txt`. `down` adds `final-screen.txt`,
`logs/`, and a consistent `runs.db` backup. `server.json` is never copied,
because it contains credentials.

Proof standards:

- **Drive the real user path**: keys into the real binary against real
  Jenkins. Never claim a behavior from a unit test, a test-only setter, or by
  writing to `runs.db` yourself.
- **Snap before and after** the action, not only the final screen. The
  transcript shows the keys in between.
- **Verify the side effects** together with the screen:
  - favorites, My Runs, and trigger intents: rows in `runs.db` (tables `runs`,
    `favorites`, `trigger_intents`, `completion_outbox`, all keyed by
    `server_id = 'local'`)
  - triggers, cancels, and replays: Jenkins state via `$J jenkins`
  - downloads: files under the RUN's `home/.local/share/JenkTui/artifacts/`
    (copy them into the evidence folder before `down`)
- **Mocks:** none. The Docker Jenkins *is* the disposable boundary. Triggers,
  replays, and cancels really run builds there, which is expected. Never point
  `JT_JENKINS_URL` at a shared or production Jenkins (`jt` refuses
  non-localhost URLs).
- **Side effects that escape the sandbox:** `o`/`O` (open in browser) launch
  the host's browser, `y` (yank in the log viewer) writes the host clipboard,
  and desktop notifications are off unless you enable them. Avoid these keys
  unless they are the feature under test. If they are, say in the proof which
  effect you observed.

## Cleanup

```bash
$J down demo
```

`down` saves the last screen, sends `ctrl+c`, kills **only** the private tmux
server `jtv-demo` (never by process name), copies the logs and `runs.db` into
`dist/verify/demo/`, and deletes `${TMPDIR:-/tmp}/jenktui-verify/demo/`. It is
safe to run again and safe to run after a failed `up`. Run it after **every**
attempt, including failed ones.

To find leftover instances: `ls ${TMPDIR:-/tmp}/jenktui-verify/` (one
directory per live RUN), or `ls /tmp/tmux-$(id -u)/ | grep jtv-`. Then
`$J down <RUN>` for each one.

`down` does not stop Jenkins, and does not undo builds you triggered there. To
reset Jenkins: `task jenkins:lts:reset && task seed` (this wipes all Jenkins
build history, so ask first if anyone else uses that container).

## Helpers

`jt` (source in `dev/jt/`, unit tests beside it) is the only helper. It talks
to Jenkins over HTTP and reads `runs.db` with the repo's pure-Go SQLite
driver, so it needs no `curl` or `sqlite3`. Run `$J list` to see RUNs that
still have state.
