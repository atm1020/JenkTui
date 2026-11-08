# Server selector and connection setup

With several Jenkins servers configured, JenkTui asks which one to use at
startup unless it remembers a last server. `ctrl+s` switches servers at any
time outside dialogs/text input. Each server has its own favorites and My
Runs. The picker also opens connection setup, including an optional private
CA certificate and a read-only test before saving.

## Sub-features

- `server-startup-picker`: with 2 or more servers and no `last_server_id`,
  the app starts on the `Select Jenkins server` dialog, which lists
  `<name>  <url>` rows.
- `server-pick`: Enter connects. The home splash then shows that server's
  URL and Jenkins version.
- `server-remember`: the choice is written to `config.json` as
  `last_server_id`, so the next start skips the picker.
- `server-switch`: `ctrl+s` reopens the picker from any page. Picking
  another server returns to home and updates `last_server_id`.
- `server-scope`: favorites and My Runs rows are keyed by server id. Data
  created on one server does not show on another.
- `server-setup`: `a` adds and `e` edits the selected connection. Tab and
  Shift+Tab change fields; `ctrl+a` reveals the optional CA certificate.
  Enter tests and saves; cancel returns to the picker when servers exist.
- `server-token`: editing never loads the stored token into the form;
  leaving its placeholder unchanged keeps the credential behind the setup
  boundary.
- `server-ca`: a private-CA HTTPS endpoint requires a readable PEM CA path.
  A failed connection test keeps the form open without saving; a successful
  test saves and connects to the edited server.

## How to get to it (user POV)

- Start the app with several servers in `server.json`.
- Press `ctrl+s` from a normal, non-modal page.
- In the picker, `a` opens new setup and `e` edits the selected row.
- With no configured servers, startup opens setup directly. Ordinary
  `jt up` always provisions a server; that first-run route needs a separate
  isolated no-server launch, not deletion of a developer's configuration.

## Driving it with jt

Preconditions:

- Start with `JT_SECOND_SERVER=1 $J up RUN`, which prints
  `state=server-picker`. Both servers point at the same local Jenkins, so
  the screen looks the same on either one. The proof is
  `last_server_id` plus server-scoped data.
- `CFG=${TMPDIR:-/tmp}/jenktui-verify/RUN/home/.config/JenkTui/config.json`

Steps:

- **Startup picker.**
  - Run `$J snap RUN server-startup-picker`. It shows `Local` and `Local B`.
- **Pick the first server.**
  - Run `$J keys RUN Enter && $J wait RUN 'Jenkins +[0-9]+\.[0-9]+'`.
  - Proof: `python3 -c "import json;print(json.load(open('$CFG'))['last_server_id'])"`
    prints `local`.
- **Switch.**
  - Run `$J keys RUN C-s && $J wait RUN 'Select Jenkins server'`.
  - Run `$J keys RUN j Enter && $J crumb RUN 'home'`.
  - `last_server_id` is now `local-b`.
- **Scoped data.**
  - On Local B, open `mobile-app` (the canonical sequence), press `F`, then
    `f`, and wait for `$J wait RUN 'Jobs \(1\)'`.
  - Run `$J keys RUN C-s`, wait for `Select Jenkins server`, then
    `$J keys RUN k Enter`, then `f`, and wait for
    `$J wait RUN 'Jobs \(0\)'`.
  - Run `$J db RUN 'select server_id,name from favorites'`. The only row
    is `local-b`.

- **Setup and private-CA trust.**
  - For the local TLS fixture, run `task certs` and `task jenkins:tls`.
    Export `JT_JENKINS_URL=https://localhost:8443` and
    `JT_CA_CERT="$PWD/dev/docker/tls/ca.pem"` for **all** harness commands,
    then start a fresh RUN with `JT_SECOND_SERVER=1` and run `doctor`.
  - From the picker, `a`, wait for `Set up Jenkins server`, then `C-a`,
    wait for `CA certificate`, and snap. `C-c` returns to the picker.
  - Press `e`, wait for `Edit Jenkins server`, then `C-a` and wait for
    `CA certificate`. Snap only the form (stored credentials are not loaded).
    Enter tests the unchanged fields, saves, and returns to authenticated
    home. Do not read or copy `server.json` as evidence.
  - Reopen the picker and edit its first row. Reveal CA with `C-a`; from
    the initial Name field, `Tab Tab Tab Tab C-u` clears the CA field.
    Enter must show `unknown authority`, not save. Snap the error.
  - Type the absolute CA path into that still-focused field, press Enter,
    and wait for the home breadcrumb and Jenkins version. Snap recovery.
- **Remembered startup.**
  - Select Local B and read only `last_server_id` from isolated config.
    Restart the binary in the same HOME using the restart command in
    [favorites.md](./favorites.md), then run `doctor` before driving.
    Authenticated home without a picker proves remembered startup.

Historical verification: 2026-10-03 (`dist/verify/proof-server/`). Setup,
private-CA failure/recovery, Favorites scope, and remembered restart were
re-driven 2026-11-08 (`dist/verify/maintain-server/`).

## Gotchas

- A failed `up` with two servers used to look like "app did not reach the
  home view". The picker is now accepted as ready, so check `state=` in the
  `up` output.
- Switching servers always returns to home and clears history.
- Each fresh picker initially selects the first row, not the active server.
- `doctor` and `jenkins` take connection options from the current environment;
  keep the TLS exports set even though the app has its own saved CA path.
- Never point the second entry at a real server. `jt` reuses the local
  Jenkins URL and credentials for both entries.
