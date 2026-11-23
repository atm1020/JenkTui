# Command palette

From a healthy isolated home view (`jt up`, `jt doctor`):

1. Snap home. Press `?` and wait for `P +commands` in help; press `?`
   again. Press uppercase `P`, wait for `Command Palette`, and snap the initial list.
   Compare with `C-s` → `Select Jenkins server`: both have an accent-colored
   inset heading, padded full-width selection, section dividers, and muted help.
   The palette has a divider between its heading and filter input, another
   between input and commands, and a third above help. At 30×11,
   dividers are omitted to keep the selected command and help visible; command
   titles take priority over shortcuts when space is tight.
2. Type `zzzzz`; wait for `No matching commands`. Press Enter; the empty state
   should remain. Snap it, press Escape, and check the palette is gone.
3. Press `C-p` and wait for `Global Search`; snap and close with `C-c`.
4. For each row, start from home. Open with `P`, wait for `Command Palette`,
   type the query, snap the filtered palette, press Enter, then wait and snap
   the destination. Use `crumb` for page destinations; Back (`b`) returns home.

| Query | Destination | Return |
|---|---|---|
| `jobs` | `Global Search` | `C-c` |
| `starred` | Favorites search scope and `No saved favorites` (same picker as `f`) | `C-c` |
| `history` | My Runs breadcrumb and `No runs yet` | `b` |
| `connection` | `Select Jenkins server` | `C-c` |
| `apprnce` | `Select Theme` (fuzzy alias) | `C-c` |
| `keybindings` | Help shows `P +commands` | `?` |
| `notification` | Separate `Notification Style` popup | Escape (cancel) |

In Notification Style, check that the current option is selected and marked
`current`. Filter `silent` and press Enter: the popup closes and the isolated
`config.json` has `desktop_enabled: true`, `sound_enabled: false`. Reopen via
`P` → `notification`, filter `sound`, and confirm; both fields become true.
Reopen, filter `Off`, and confirm; both become false. Reopen once more, move the
selection and press Escape: neither field changes. Snap the palette before
opening, the popup, and each confirmation. Read only the `notification` object
in the RUN's disposable config; never inspect the developer's config or
`server.json`. Choices reuse `key_map.dialogs.command_palette` navigation and
close bindings. This checks settings, not delivery of desktop notifications.

Check read-only invariants: use `jt db` to compare run and favorite counts, and
`jt jenkins` to compare the seed job's last build before/after. Dismissal and
navigation alone should not write to Jenkins or add rows. Run `jt down`.

For rebinding, repeat `up` with `JT_CONFIG_JSON` setting
`key_map.global.command_palette` to `alt+k` and `key_map.dialogs.command_palette.close`
to `ctrl+q`. Check `M-k` opens and `C-q` dismisses; snap both states and `down`.
