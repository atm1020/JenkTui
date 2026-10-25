# Confirmation Dialog Component

A general-purpose, reusable confirmation dialog component for JenkTui that displays "Are you sure?" style prompts with Yes/No options.

## Features

- **Customizable message**: Display any question or prompt
- **Callback support**: Execute custom commands on Yes or No
- **Keyboard-driven**: Multiple ways to interact (arrows, Y/N keys, Enter/ESC)
- **Safe defaults**: Defaults to "No" to prevent accidental confirmations
- **Theme-aware**: Follows JenkTui's theme system

## Usage

### Basic Example

```go
// In your component's Update method
case tea.KeyPressMsg:
    switch {
    case key.Matches(msg, m.keymap.Delete):
        // Create confirmation callback
        onConfirm := func() tea.Msg {
            // Perform the action
            return deleteJobMsg{jobID: m.selectedJob}
        }

        // Create and show dialog
        dialog := confirm.NewConfirmDialog(
            "Are you sure you want to delete this job?",
            onConfirm,
            nil, // onCancel is optional
        )

        return m, util.CmdHandler(util.OpenDialogMsg{Dialog: dialog})
    }
```

### With Cancel Callback

```go
onConfirm := func() tea.Msg {
    return triggerBuildMsg{jobID: m.selectedJob}
}

onCancel := func() tea.Msg {
    return util.FooterNotiMsg{Message: "Build cancelled"}
}

dialog := confirm.NewConfirmDialog(
    "Are you sure you want to trigger a new build?",
    onConfirm,
    onCancel,
)

return m, util.CmdHandler(util.OpenDialogMsg{Dialog: dialog})
```

## Keyboard Controls

| Key(s) | Action |
|--------|--------|
| `←` / `→` | Switch between Yes and No |
| `Tab` | Switch between Yes and No |
| `Y` | Select Yes immediately |
| `N` | Select No immediately |
| `ESC` | Cancel (same as No) |
| `Enter` / `Space` | Confirm selected option |

## API

### `NewConfirmDialog(question string, onConfirm tea.Cmd, onCancel tea.Cmd)`

Creates a new confirmation dialog.

**Parameters:**
- `question`: The question/prompt to display to the user
- `onConfirm`: Command to execute when user selects "Yes" (required)
- `onCancel`: Command to execute when user selects "No" (optional, can be `nil`)

**Returns:** `*ConfirmDialog`

## Design Decisions

1. **Default to "No"**: For safety, the "No" button is selected by default, requiring explicit user action to confirm potentially destructive operations.

2. **Multiple input methods**: Users can navigate with arrows/tab OR press Y/N directly, providing both exploration and quick actions.

3. **Explicit callbacks**: Rather than returning boolean values, the dialog executes commands, fitting better with Bubble Tea's message-driven architecture.

## Integration

The dialog integrates with JenkTui's dialog system via the `util.OpenDialogMsg` and `util.CloseDialogMsg` messages. The app automatically handles dialog lifecycle, window resizing, and rendering.

## Example Use Cases

- Confirming job deletion
- Confirming build trigger
- Confirming configuration changes
- Confirming navigation away from unsaved changes
- Any other yes/no decision requiring user confirmation
