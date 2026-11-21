package commandpalette

import (
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"
	"github.com/charmbracelet/x/ansi"
)

type Action int

const (
	SearchJenkins Action = iota + 1
	Favorites
	MyRuns
	SwitchServer
	ChangeTheme
	Help
	NotificationStyle
	NotificationsOff
	NotificationsSilent
	NotificationsBell
)

type Entry struct {
	Action                   Action
	Title, Aliases, Shortcut string
}

type SelectedMsg struct {
	Dialog *Dialog
	Action Action
}

type Dialog struct {
	title           string
	input           textinput.Model
	keys            keyMap
	entries         []Entry
	targets         []string
	matches         []int
	cursor          int
	width, rowLimit int
	showDividers    bool
}

func New(entries []Entry, width, height int, conf context.CommandPaletteDialogKeyMapConfig) *Dialog {
	input := textinput.New()
	input.Placeholder = "Type to filter commands"
	input.CharLimit = 128
	d := &Dialog{title: "Command Palette", input: input, keys: newKeyMap(conf), entries: entries}
	for _, entry := range entries {
		d.targets = append(d.targets, entry.Title+" "+entry.Aliases)
	}
	d.resize(width, height)
	d.filter()
	return d
}

func (d *Dialog) Init() tea.Cmd { return d.input.Focus() }

func (d *Dialog) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		d.resize(size.Width, size.Height)
		return d, nil
	}
	if press, ok := msg.(tea.KeyPressMsg); ok {
		return d, d.handleKey(press)
	}
	return d, d.updateInput(msg)
}

func (d *Dialog) View() string {
	t := styles.GetTheme()
	h := help.New()
	h.SetWidth(max(1, d.width-2))
	inset := t.S().Base.Padding(0, 1)
	textWidth := max(1, d.width-2)
	title := inset.Bold(true).Foreground(t.Primary).Render(ansi.Truncate(d.title, textWidth, "…"))
	input := inset.Render(ansi.Truncate(d.input.View(), textWidth, "…"))
	helpView := inset.Foreground(t.Grey).Render(h.ShortHelpView(d.keys.ShortHelp()))
	parts := []string{title}
	if d.showDividers {
		parts = append(parts, util.HorizontalLine(d.width, t.Border))
	}
	parts = append(parts, input)
	if d.showDividers {
		parts = append(parts, util.HorizontalLine(d.width, t.Border))
	}
	parts = append(parts, d.rows())
	if d.showDividers {
		parts = append(parts, util.HorizontalLine(d.width, t.Border))
	}
	parts = append(parts, helpView)
	content, _ := util.JoinDialogParts(parts...)
	return t.S().DialogBorder.Render(content)
}

func (d *Dialog) UpdateFooter() tea.Cmd { return nil }

func (d *Dialog) CleanUp() tea.Cmd {
	d.input.Blur()
	return nil
}

func (d *Dialog) Dimensions() (int, int) { return lipgloss.Size(d.View()) }

func (d *Dialog) KeyMap() help.KeyMap { return d.keys }

func (d *Dialog) resize(width, height int) {
	border := styles.GetTheme().S().DialogBorder
	d.width = max(1, styles.ClampDialogWidth(60, width)-border.GetHorizontalFrameSize())
	availableRows := height - border.GetVerticalFrameSize() - 6 // Overlay offset, title, input, help, footer.
	d.showDividers = availableRows >= 4
	if d.showDividers {
		availableRows -= 3
	}
	d.rowLimit = max(1, availableRows)
	d.input.SetWidth(max(1, d.width-2-lipgloss.Width(d.input.Prompt)-1))
}

func (d *Dialog) filter() {
	d.matches = d.matches[:0]
	d.cursor = 0
	if query := strings.TrimSpace(d.input.Value()); query != "" {
		for _, rank := range list.DefaultFilter(query, d.targets) {
			d.matches = append(d.matches, rank.Index)
		}
		return
	}
	for i := range d.entries {
		d.matches = append(d.matches, i)
	}
}

func (d *Dialog) rows() string {
	t := styles.GetTheme()
	if len(d.matches) == 0 {
		return t.S().Base.Foreground(t.Grey).Padding(0, 1).Render(ansi.Truncate("No matching commands", max(1, d.width-2), "…"))
	}
	start := max(0, d.cursor-d.rowLimit+1)
	end := min(len(d.matches), start+d.rowLimit)
	rows := make([]string, 0, end-start)
	for i := start; i < end; i++ {
		entry := d.entries[d.matches[i]]
		textWidth := max(1, d.width-2)
		style := t.S().Base.Width(d.width).Padding(0, 1).Foreground(t.FgBase)
		if i == d.cursor {
			style = style.Background(t.SelectedBackground)
		}
		text := entry.Title
		if entry.Shortcut != "" && lipgloss.Width(entry.Title)+lipgloss.Width(entry.Shortcut)+2 <= textWidth {
			labelWidth := textWidth - lipgloss.Width(entry.Shortcut) - 2
			text = lipgloss.NewStyle().Width(labelWidth).Render(ansi.Truncate(text, labelWidth, "…")) + "  " + entry.Shortcut
		}
		rows = append(rows, style.Render(ansi.Truncate(text, textWidth, "…")))
	}
	return strings.Join(rows, "\n")
}

func (d *Dialog) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, d.keys.close):
		return util.CmdHandler(util.CloseDialogMsg{Dialog: d})
	case key.Matches(msg, d.keys.up):
		d.cursor = max(0, d.cursor-1)
	case key.Matches(msg, d.keys.down):
		d.cursor = min(d.cursor+1, max(0, len(d.matches)-1))
	case key.Matches(msg, d.keys.selectItem):
		if len(d.matches) > 0 {
			return util.CmdHandler(SelectedMsg{Dialog: d, Action: d.entries[d.matches[d.cursor]].Action})
		}
	default:
		return d.updateInput(msg)
	}
	return nil
}

func (d *Dialog) updateInput(msg tea.Msg) tea.Cmd {
	previous := d.input.Value()
	var cmd tea.Cmd
	d.input, cmd = d.input.Update(msg)
	if previous != d.input.Value() {
		d.filter()
	}
	return cmd
}
