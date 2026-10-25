package confirm

import (
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// ConfirmDialog represents a confirmation dialog for yes/no questions.
type ConfirmDialog struct {
	width  int
	height int

	question   string  // The question to ask the user
	selectedNo bool    // true if "No" button is selected
	onConfirm  tea.Cmd // Command to execute on "Yes"
	onCancel   tea.Cmd // Command to execute on "No"
	keymap     KeyMap
}

// NewConfirmDialog creates a new confirmation dialog.
// question: The question to display
// onConfirm: Command to execute when user selects "Yes"
// onCancel: Optional command to execute when user selects "No" (can be nil)
// keymapConf: Keymap configuration from user config
func NewConfirmDialog(question string, onConfirm tea.Cmd, onCancel tea.Cmd, keymapConf context.ConfirmDialogKeyMapConfig) *ConfirmDialog {
	return &ConfirmDialog{
		question:   question,
		selectedNo: true, // Default to "No" for safety
		onConfirm:  onConfirm,
		onCancel:   onCancel,
		keymap:     UserKeyMap(keymapConf),
	}
}

func (c *ConfirmDialog) Init() tea.Cmd {
	return nil
}

// Update handles keyboard input for the confirmation dialog.
func (c *ConfirmDialog) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		c.width = msg.Width
		c.height = msg.Height
	case tea.KeyPressMsg:
		return c, c.handleKeyPress(msg)
	}
	return c, nil
}

// View renders the confirmation dialog with Yes/No buttons.
func (c *ConfirmDialog) View() string {
	t := styles.GetTheme()
	baseStyle := t.S().Base
	yesStyle := t.S().Text
	noStyle := yesStyle

	if c.selectedNo {
		noStyle = noStyle.Foreground(t.HighlightForeground).Background(t.Primary)
		yesStyle = yesStyle.Background(t.SelectedBackground)
	} else {
		yesStyle = yesStyle.Foreground(t.HighlightForeground).Background(t.Primary)
		noStyle = noStyle.Background(t.SelectedBackground)
	}

	const horizontalPadding = 3
	yesButton := yesStyle.PaddingLeft(horizontalPadding).Underline(true).Render("Y") +
		yesStyle.PaddingRight(horizontalPadding).Render("es")
	noButton := noStyle.PaddingLeft(horizontalPadding).Underline(true).Render("N") +
		noStyle.PaddingRight(horizontalPadding).Render("o")

	questionWidth := lipgloss.Width(c.question)
	buttons := baseStyle.Width(questionWidth).Align(lipgloss.Right).Render(
		lipgloss.JoinHorizontal(lipgloss.Center, yesButton, baseStyle.Render("  "), noButton),
	)

	// Paint every part to the widest part's width so no unstyled cells leak the
	// terminal's default background, then size the container to fit.
	content, w := util.JoinDialogParts(c.question, "", buttons)

	// Clamp into the shared dialog floor/ceiling so a short question still reads
	// as the same popup size as the rest of the app, and a long one never
	// overflows the window.
	clampedWidth := styles.ClampDialogWidth(w, c.width)
	if clampedWidth != w {
		content = lipgloss.NewStyle().Width(clampedWidth).Render(content)
		w = clampedWidth
	}

	border := t.S().DialogBorder
	return border.Width(w + border.GetHorizontalFrameSize()).Render(content)
}

// UpdateFooter updates the footer with keybindings.
func (c *ConfirmDialog) UpdateFooter() tea.Cmd {
	return nil
}

func (c *ConfirmDialog) CleanUp() tea.Cmd {
	return nil
}

// Dimensions returns the width and height of the dialog.
// The dialog will be centered horizontally and positioned at the top of the window.
func (c *ConfirmDialog) Dimensions() (int, int) {
	dialogView := c.View()
	dialogWidth := lipgloss.Width(dialogView)
	dialogHeight := lipgloss.Height(dialogView)

	return dialogWidth, dialogHeight
}

func (c *ConfirmDialog) KeyMap() help.KeyMap {
	return c.keymap
}

func (c *ConfirmDialog) handleKeyPress(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, c.keymap.LeftRight, c.keymap.Tab):
		c.selectedNo = !c.selectedNo
	case key.Matches(msg, c.keymap.EnterSpace):
		return c.submit(!c.selectedNo)
	case key.Matches(msg, c.keymap.Yes):
		return c.submit(true)
	case key.Matches(msg, c.keymap.No, c.keymap.Close):
		return c.submit(false)
	}
	return nil
}

func (c *ConfirmDialog) submit(confirm bool) tea.Cmd {
	cmds := []tea.Cmd{util.CmdHandler(util.CloseDialogMsg{})}
	// onConfirm / onCancel are already tea.Cmds (func() tea.Msg), built when the
	// dialog was opened. Returning them here is what fires them; Bubble Tea runs
	// them after Update. Do not call onConfirm() / onCancel().
	if confirm && c.onConfirm != nil {
		cmds = append(cmds, c.onConfirm)
	}
	if !confirm && c.onCancel != nil {
		cmds = append(cmds, c.onCancel)
	}
	return tea.Batch(cmds...)
}
