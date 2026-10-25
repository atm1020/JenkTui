package paramsview

import (
	"strings"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
)

type keyMap struct {
	Close key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Close}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{{k.Close}}
}

type ParamsDialog struct {
	params    []gojenkins.BuildParameter
	width     int
	keymap    keyMap
	closeHint string
}

func NewParamsDialog(params []gojenkins.BuildParameter, windowWidth int, keymapConf context.ParamsViewDialogKeyMapConfig) *ParamsDialog {
	return &ParamsDialog{
		params:    params,
		width:     windowWidth,
		closeHint: keymapConf.Close.HelpText() + " to close",
		keymap: keyMap{
			Close: key.NewBinding(
				key.WithKeys(keymapConf.Close...),
				key.WithHelp(keymapConf.Close.HelpText(), "close"),
			),
		},
	}
}

func (d *ParamsDialog) Init() tea.Cmd {
	return nil
}

func (d *ParamsDialog) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if key.Matches(msg, d.keymap.Close) {
			return d, util.CmdHandler(util.CloseDialogMsg{})
		}
	}
	return d, nil
}

func (d *ParamsDialog) View() string {
	t := styles.GetTheme()

	hint := t.S().Base.Foreground(t.Grey).Render(d.closeHint)

	dialogWidth := styles.DialogWidth(d.width)
	border := t.S().DialogBorder
	innerWidth := dialogWidth - border.GetHorizontalFrameSize()

	var rows []string
	if len(d.params) == 0 {
		rows = []string{
			t.S().Base.Italic(true).Render("No parameters used for this build"),
			"",
			hint,
		}
	} else {
		nameStyle := t.S().Base.Bold(true).Width(20)
		valueStyle := t.S().Base

		rows = []string{
			t.S().Title.Render("Build Parameters"),
			"",
		}
		separator := t.S().Base.Foreground(t.Grey).Render(strings.Repeat("─", innerWidth))
		for i, p := range d.params {
			row := lipgloss.JoinHorizontal(lipgloss.Top,
				nameStyle.Render(p.Name),
				valueStyle.Render(p.Value),
			)
			rows = append(rows, row)
			if i < len(d.params)-1 {
				rows = append(rows, separator)
			}
		}
		rows = append(rows, "", hint)
	}

	// Paint every row to the full inner width so no unstyled cells leak the
	// terminal's default background, then let the fixed-width container frame it.
	for i, r := range rows {
		rows[i] = util.PadWidth(r, innerWidth)
	}
	content := strings.Join(rows, "\n")

	return border.Width(dialogWidth).Render(content)
}

func (d *ParamsDialog) UpdateFooter() tea.Cmd {
	return nil
}

func (d *ParamsDialog) CleanUp() tea.Cmd {
	return nil
}

func (d *ParamsDialog) Dimensions() (int, int) {
	v := d.View()
	return lipgloss.Width(v), lipgloss.Height(v)
}

func (d *ParamsDialog) KeyMap() help.KeyMap {
	return d.keymap
}
