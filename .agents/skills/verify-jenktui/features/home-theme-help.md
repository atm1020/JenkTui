# Home, theme selector, and help

The home view is a splash that shows the connected server, the Jenkins
version, and the JenkTui version. `ctrl+t` opens a live theme picker that
is saved to config. `?` expands any page's footer into its full key list.

## Sub-features

- `home-splash`: the JENKTUI logo above a box with `Server <url>`,
  `Jenkins <version>`, and `JenkTui <version>`. The footer reads
  `👤 admin  home`.
- `home-version`: the `JenkTui` line displays `version.Version`; do not
  require a `+dirty` suffix. The harness records its separate Git stamp
  in `build.txt`. The `Jenkins` line appears after authentication succeeds.
- `theme-open`: `ctrl+t` opens `Select Theme`, a list of themes with `●`
  on the current one (default `GitHub Dark`).
- `theme-apply`: `j`/`k` previews colors immediately. Enter commits and
  closes; `ctrl+c` cancels the preview and restores the original theme.
- `theme-persist`: the choice is written to `config.json` `theme`.
- `help-toggle`: `?` expands the footer to the page's full binding list
  (on a build page: `h prev stage`, `ctrl+l open step log`,
  `d choose artifacts`, `x cancel build`, and so on). `?` again collapses
  it. While expanded, `ctrl+g` switches between local and global help.

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
    lines, including the configured local URL and actual Jenkins version.
  - Run `$J crumb RUN 'admin.*home'`, then `$J snap RUN home`.
- **Theme.**
  - Record the colors with
    `before=$(tmux -L jtv-RUN capture-pane -p -e -t jt | md5sum)`.
  - Run `$J keys RUN C-t && $J wait RUN 'Select Theme'`, then
    `$J snap RUN theme-picker`. `● GitHub Dark` is marked.
  - Run `$J keys RUN j`, then `$J snap RUN theme-preview`. Cancel with
    `C-c` and confirm the original theme/config remain. Reopen with `C-t`,
    wait for `Select Theme`, then run `$J keys RUN j Enter`.
  - Proof: `python3 -c "import json;print(json.load(open('$CFG'))['theme'])"`
    prints `Gruvbox`. Capture `$J snap RUN theme-after`; compare actual
    palette escapes on the same home surface, not only the whole-screen
    md5 (dialog/selection changes also alter that hash).
- **Help.**
  - On any page, run `$J keys RUN '?' && $J wait RUN 'more|open step log|toggle'`,
    then `$J snap RUN help`.
  - Run `$J keys RUN C-g` and snap the global help tier, then run
    `$J keys RUN '?'` to collapse it.

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
