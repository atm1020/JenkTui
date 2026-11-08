# JenkTui verification map

This directory is the maintained source for verifying JenkTui's user-facing
behavior. Read this index before you drive the app. Then use the matching
feature file as your recipe. Every command assumes you are in the repo root
with `J=dist/bin/jt` (build it once per session with
`go build -o dist/bin/jt ./dev/jt`).

## Baseline preconditions

- The local Jenkins LTS answers at `http://127.0.0.1:8080` as `admin`/`admin`
  and has the seed data loaded. Use `task jenkins:lts` and then `task seed`.
- `$J up <RUN>` printed `ready`, and `$J doctor <RUN>` exits 0.
- Each RUN starts with an empty `runs.db`: no favorites, no My Runs history.
  Recipes that need prior state create it themselves through the UI.
- Never drive a tmux session or JenkTui process that this run did not start.
  In particular, never drive the developer's own `jenktui` window.

## Driving conventions

- Start each recipe from home unless its preconditions say otherwise.
  Back history retains only three views; if home is no longer reachable,
  run `down` and `up` rather than repeatedly pressing `b`.
- After **every** keypress that changes the view, wait for the next view's
  stable text with `$J wait` before you send the next key.
- Use `$J type` for text that goes into a search box. Use `$J keys` for named
  keys and single-character commands.
- Match the footer breadcrumb by its words joined with `.*`. Nerd-font glyphs
  sit between the parts.
- Seed job paths used throughout:
  - `DevTeam/Frontend/mobile-app`: pipeline, about 26s per build, 9 stages
  - `DevTeam/Backend/log-heavy-job`: 1,500+ log lines, including 500 lines of
    `Processing batch`
  - `DevTeam/Backend`: currently five seeded children; check the actual
    Jenkins count and row order rather than assuming a fixed fixture.

## Proof and skip reporting

- Capture the screen before and after the action with `$J snap`. The
  transcript records the keys in between.
- When the action mutates state, also capture a second, read-only view of
  that state: `$J db` for `runs.db`, or `$J jenkins` for Jenkins.
- Tag every snap name with the feature ID, e.g. `fav-toggle-after`.
- If an entry point cannot be reached, record the command you tried and the
  precondition that was not met with `$J note`. Do not report an entry point
  as verified because you reached the feature through a different one.

## Feature entry contract

Each feature file starts with an H1 title and one paragraph that describes
the user-visible behavior. It then has exactly four H2 sections, in this
order:

1. `Sub-features`: short IDs, one line per behavior.
2. `How to get to it (user POV)`: every entry point a user has.
3. `Driving it with jt`: starts with `Preconditions:`, then labeled
   bullets. Each bullet pairs a user action with the exact command and the
   result you should see.
4. `Gotchas`: traps that can waste a verification run or make it invalid.

## Features

One row per feature file; related surfaces share a recipe. Historical
end-to-end evidence is in `dist/verify/proof-*` (2026-10-03, Jenkins 2.568.3).
All eight files received source review and feature-level live coverage on
2026-11-08 against Jenkins 2.580.1: `dist/verify/maintain-map/` and
`dist/verify/maintain-server/`. That maintenance pass does not establish every
sub-feature or optional external effect below.

| Surfaces | Recipe |
|---|---|
| Home, theme preview/save/cancel, local/global help | [home-theme-help.md](./home-theme-help.md) |
| Global search, folder listing, back/forward | [search-and-browse.md](./search-and-browse.md) |
| Job, build, selected step log, parameter display | [job-and-build.md](./job-and-build.md) |
| Console log viewer | [log-viewer.md](./log-viewer.md) |
| Trigger confirmation, My Runs | [trigger-and-my-runs.md](./trigger-and-my-runs.md) |
| Favorites | [favorites.md](./favorites.md) |
| Build form, artifacts/open-folder confirmation, replay, cancellation | [build-actions.md](./build-actions.md) |
| Server picker, add/edit setup, private-CA test/save | [server-selector.md](./server-selector.md) |

Not verified yet:

- Replay edit (`e` opens `$EDITOR`)
- Open in browser (`o`/`O`)
- Log yank (`y`, which writes the host clipboard)
- Node selector
- Multibranch (`merge-queue-test`)
- Active Choices / dynamic parameters (`Dynamic/`)
- Desktop notifications

## Known app issues found while verifying (2026-10-03)

These are recorded in the recipes so that a run does not mistake them for
harness failures:

1. Build params: `j`, `k`, and `?` cannot be typed into string or text
   fields ([build-actions.md](./build-actions.md)).
2. Job page: the Builds column and selection stay stale after a trigger
   ([job-and-build.md](./job-and-build.md)). This is suspected, not
   root-caused.
3. Favorites: `d` gives no feedback, and the old toast lingers
   ([favorites.md](./favorites.md)).
4. Older seed data: `all-param-types` can retain empty `REGION` choices
   and return 500. Current seed XML is fixed; check/reseed the fixture
   ([build-actions.md](./build-actions.md)).
5. Logging: router evictions are logged at `ERRO`, so `ERRO` lines are
   noise. `doctor` counts them instead of failing on them.
