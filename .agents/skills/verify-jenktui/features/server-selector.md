# Server selector

With several Jenkins servers configured, JenkTui asks which one to use at
startup unless it remembers a last server. `ctrl+s` switches servers at any
time. Each server has its own favorites and My Runs.

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

## How to get to it (user POV)

- Start the app with several servers in `server.json`.
- Press `ctrl+s` from any page.

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

Verified 2026-10-03 (`dist/verify/proof-server/`).

## Gotchas

- A failed `up` with two servers used to look like "app did not reach the
  home view". The picker is now accepted as ready, so check `state=` in the
  `up` output.
- Switching servers always returns to home and clears history.
- Never point the second entry at a real server. `jt` reuses the local
  Jenkins URL and credentials for both entries.
