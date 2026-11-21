# Command palette research

Design notes for the built-in command palette. See the [README](../README.md) for the current shortcuts.

## What the references do

- **Charm Crush:** `ctrl+p` opens a modal with a focused filter input; arrows move the selection, Enter runs it, and Esc closes it. The dialog returns the selected item's action rather than replaying a keystroke. ([binding](../.crush/internal/ui/model/keys.go#L101), [dialog](../.crush/internal/ui/dialog/commands.go#L85), [selection and filtering](../.crush/internal/ui/dialog/commands.go#L162))
- Crush fuzzy-matches titles, aliases, and descriptions. An item can show its shortcut, and commands that require an active session are only listed when there is one. ([searchable text](../.crush/internal/ui/dialog/commands_item.go#L62), [fuzzy matching](../.crush/internal/ui/list/filterable.go#L70), [command list](../.crush/internal/ui/dialog/commands.go#L454))
- **gh-dash:** its search shortcut sets search mode on the current section. Its custom commands are dispatched via configured keybindings. Neither flow is the model for a global picker of built-in actions. ([section search](../.ghdash/internal/tui/ui.go#L348), [custom bindings](../.ghdash/internal/tui/modelUtils.go#L70))

## Recommendation for JenkTui

Keep `ctrl+p` for **Global Search** (Jenkins jobs and folders). Add a configurable uppercase `P` for **Command Palette** (JenkTui actions), with a visible help hint. The palette can offer “Search Jenkins” as an entry, but typing in it should only filter local commands; no Jenkins requests until the user chooses a feature that needs them. ([current default](../src/config/loader.go#L52), [global search fetch](../src/ui/components/dialogs/globalsearch/globalsearch.go#L179), [global help](../src/ui/util/keys.go#L69))

Start with Search Jenkins, Favorites, My Runs, Switch Server, Change Theme, and Help. Show each action's configured shortcut and support aliases such as `history` for My Runs and `appearance` for Change Theme. For example: **P → `appearance` → Enter**. Add page-specific commands later, only when applicable, and route build-changing actions through their existing confirmation flows.

Make it a dialog alongside Global Search, with a focused input and a fuzzy list. On Enter, emit a typed selection and let the app run the same handler as the existing shortcut. That preserves the app's dialog cleanup and My Runs' current-view scoping without synthesizing keys or duplicating action logic. No custom commands or category tabs are needed for the first version. ([dialog lifecycle](../src/ui/ui.go#L518), [existing shortcut dispatch](../src/ui/ui.go#L655), [keybinding conventions](../CODING_STANDARDS.md#keybindings))
