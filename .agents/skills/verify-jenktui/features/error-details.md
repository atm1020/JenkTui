# Error dialog HTML diagnostics

JSON-response errors can offer the original HTML through an explicit clipboard action. The dialog renders only the error summary and a sensitive-data warning. Captured HTML stays in memory, is limited to 256 KiB, and is released when the dialog closes.

## Sub-features

- `error-html-copy`: `y` sends the captured HTML bytes, not the summary, through Bubble Tea's clipboard command.
- `error-html-warning`: warn about sensitive data and identify incomplete captures.
- `error-no-html`: ordinary errors have no copy action.
- `error-close`: `esc` closes without copying; copy does not close the dialog.

## How to get to it (user POV)

The shared error overlay appears when an operation reports an error. A reproducible HTML case is opening a build deleted from Jenkins after its job page loaded. Trigger/replay errors use the same overlay when they retain the HTML-response error.

## Driving it with jt

Preconditions: local Jenkins and seed data; `up` and `doctor` passed. Use a disposable pipeline named `html-copy-diagnostic` with one completed build. Set `JT_CONFIG_JSON='{"poll_interval":3600,"notification":{"desktop_enabled":false}}'` before `up` so the selected build remains in the loaded job window. Use the gojenkins version pinned in `go.mod`, without a local workspace override.

- **Open disposable job:** `$J keys RUN C-p && $J wait RUN 'Global Search' && $J type RUN html-copy-diagnostic && $J keys RUN Enter && $J wait RUN '‣ .*html-copy-diagnostic' && $J keys RUN Enter && $J crumb RUN html-copy-diagnostic && $J wait RUN '│ #1 │'`. Snapshot with `$J snap RUN error-before`.
- **Cause a real HTML failure:** delete only that disposable build using local Jenkins administration, then `$J keys RUN Enter && $J wait RUN 'copy HTML' && $J snap RUN error-html`. Expect HTTP 404, an HTML explanation, and `HTML may contain sensitive data`, without rendering the page source.
- **Copy:** `$J keys RUN y && $J wait RUN 'HTML sent to clipboard' && $J snap RUN error-copied`. This writes the clipboard. Inspect the terminal's OSC 52 transfer without logging its body; record decoded byte count and an HTML-signature assertion only. A footer confirmation proves dispatch, not acceptance by the host clipboard.
- **Close:** `$J keys RUN Escape && $J snap RUN error-closed`. Confirm the overlay disappeared. Run `doctor`, then `down`, and delete the disposable job.
- **Unavailable and truncated captures:** the deterministic dialog tests cover no-body errors, truncation, custom bindings, and exact clipboard-message bytes: `go test ./src/ui/components/dialogs/errorpage`. These are unit evidence, not live clipboard evidence.

## Gotchas

- HTML can contain private data or credentials. Review it locally before sharing; keep actual HTML out of screenshots, logs, and verification artifacts.
- The runtime capability uses `errors.As`, so `%w` wrappers preserve copying; errors recreated with `%v` do not.
- Capture applies to HTML JSON-decoding failures; it does not change successful HTML replay responses or imply that every HTTP failure retains a body.
- Clipboard support depends on the terminal's OSC 52 policy. `y` sends a request; it cannot verify the host clipboard.
