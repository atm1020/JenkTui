# Home, theme selector, and help

The home view is a splash that shows the connected server, the Jenkins
version, and the JenkTui version. `ctrl+t` opens a live theme picker that
is saved to config. `?` expands any page's footer into its full key list.

## Sub-features

- `home-splash`: the JENKTUI logo above a box with `Server <url>`,
  `Jenkins <version>`, and `JenkTui <version>`. The footer reads
  `👤 admin  home`.
- `home-version`: the `JenkTui` line is the build's VCS-stamped version,
  ending in `+dirty` for a dirty tree. The `Jenkins` line appears only
  after authentication succeeds.
- `theme-open`: `ctrl+t` opens `Select Theme`, a list of themes with `●`
  on the current one (default `GitHub Dark`).
- `theme-apply`: `j`/`k` then Enter applies the theme immediately: colors
  change and the dialog closes.
- `theme-persist`: the choice is written to `config.json` `theme`.
- `help-toggle`: `?` expands the footer to the page's full binding list
  (on a build page: `h prev stage`, `ctrl+l open step log`,
  `d choose artifacts`, `x cancel build`, and so on). `?` again collapses
  it.

## How to get to it (user POV)

- Home: app start (single server), or after picking a server. `b` returns
  there only within 3 steps of history.
- Theme: `ctrl+t` from any page.
- Help: `?` on any page.

## Driving it with jt

Preconditions:

- A fresh RUN (`$J up RUN` prints `state=home`).
- `CFG=${TMPDIR:-/tmp}/jenktui-verify/RUN/home/.config/JenkTui/config.json`

Steps:

- **Home splash.**
  - Run `$J screen RUN | grep -E 'Server|Jenkins |JenkTui '`. Expect three
    lines, including `http://127.0.0.1:8080` and `2.5xx.x`.
  - Run `$J crumb RUN 'admin.*home'`, then `$J snap RUN home`.
- **Theme.**
  - Record the colors with
    `before=$(tmux -L jtv-RUN capture-pane -p -e -t jt | md5sum)`.
  - Run `$J keys RUN C-t && $J wait RUN 'Select Theme'`, then
    `$J snap RUN theme-picker`. `● GitHub Dark` is marked.
  - Run `$J keys RUN j Enter`. `Select Theme` is gone.
  - Proof: `python3 -c "import json;print(json.load(open('$CFG'))['theme'])"`
    prints `Gruvbox`, and the ANSI capture's md5 differs from `before`.
- **Help.**
  - On any page, run `$J keys RUN '?' && $J wait RUN 'more|open step log|toggle'`,
    then `$J snap RUN help`.
  - Run `$J keys RUN '?'` to collapse it.

Verified 2026-10-03 (`dist/verify/proof-pages/`: `theme-picker`,
`theme-after`, `home-help`).

## Gotchas

- The theme list order is alphabetical. With the default `GitHub Dark`, one
  `j` lands on `Gruvbox`. If `JT_CONFIG_JSON` sets another theme, re-check
  where the `●` marker is.
- Theme proof needs the `.ansi` capture or the md5 of `capture-pane -e`.
  The plain text does not change.
- `ctrl+t` writes `config.json` in the isolated HOME only. Never copy
  that file over the developer's config.
