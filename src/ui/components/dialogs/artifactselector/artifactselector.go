package artifactselector

import (
	"fmt"
	"strings"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

// Dialog lets the user choose a subset of a build's artifacts to download.
type Dialog struct {
	artifacts []gojenkins.Artifact
	selected  []bool
	cursor    int
	offset    int

	width, height     int
	keymap            KeyMap
	help              help.Model
	onDownload        func([]gojenkins.Artifact) tea.Cmd
	selectionRequired bool
}

var _ util.JenkTuiDialogModel = (*Dialog)(nil)

// New creates an artifact selector. Artifacts start unchecked so opening the
// dialog never opts the user into downloading files they do not need.
func New(
	artifacts []gojenkins.Artifact,
	width, height int,
	onDownload func([]gojenkins.Artifact) tea.Cmd,
	keymapConf context.ArtifactSelectorDialogKeyMapConfig,
) *Dialog {
	d := &Dialog{
		artifacts:  append([]gojenkins.Artifact(nil), artifacts...),
		selected:   make([]bool, len(artifacts)),
		keymap:     UserKeyMap(keymapConf),
		help:       help.New(),
		onDownload: onDownload,
	}
	d.setSize(width, height)
	return d
}

func (d *Dialog) Init() tea.Cmd {
	return nil
}

func (d *Dialog) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return d, d.handleWindowSizeMsg(msg)
	case tea.KeyPressMsg:
		return d, d.handleKeyPressMsg(msg)
	}
	return d, nil
}

func (d *Dialog) View() string {
	t := styles.GetTheme()
	border := t.S().DialogBorder
	dialogWidth := styles.DialogWidth(d.width)
	innerWidth := max(1, dialogWidth-border.GetHorizontalFrameSize())
	contentHeight := max(1, styles.DialogHeight(d.height)-border.GetVerticalFrameSize())

	title := t.S().Title.Render(ansi.Truncate("Download artifacts", innerWidth, "…"))
	summaryText := fmt.Sprintf("%d of %d selected", d.selectedCount(), len(d.artifacts))
	if d.selectionRequired {
		summaryText = "Select at least one artifact"
	}
	summaryStyle := t.S().Base.Foreground(t.Grey)
	if d.selectionRequired {
		summaryStyle = t.S().Base.Foreground(t.Warning)
	}
	summary := summaryStyle.Render(ansi.Truncate(summaryText, innerWidth, "…"))

	visibleRows := d.visibleRowCount()
	start, end := d.visibleRange(visibleRows)
	rows := make([]string, 0, 2+(end-start)+2)
	rows = append(rows, title, summary)

	if len(d.artifacts) == 0 {
		rows = append(rows, t.S().Base.Foreground(t.Grey).Render("No artifacts available"))
	} else {
		nameWidth := max(1, innerWidth-lipgloss.Width("  [ ] "))
		for i := start; i < end; i++ {
			prefix := "  "
			if i == d.cursor {
				prefix = "› "
			}

			checkbox := "[ ]"
			if d.selected[i] {
				checkbox = "[x]"
			}
			name := ansi.Truncate(d.artifacts[i].FileName, nameWidth, "…")
			row := prefix + checkbox + " " + name
			rowStyle := t.S().Base.Width(innerWidth)
			if d.selected[i] {
				rowStyle = rowStyle.Foreground(t.Success)
			}
			if i == d.cursor {
				rowStyle = rowStyle.Background(t.SelectedBackground).Foreground(t.FgBase)
			}
			rows = append(rows, rowStyle.Render(row))
		}
	}

	rows = append(rows, d.renderHelp(contentHeight)...)
	for i, row := range rows {
		rows[i] = util.PadWidth(row, innerWidth)
	}

	return border.Width(dialogWidth).Render(strings.Join(rows, "\n"))
}

func (d *Dialog) UpdateFooter() tea.Cmd {
	return nil
}

func (d *Dialog) CleanUp() tea.Cmd {
	return nil
}

func (d *Dialog) Dimensions() (int, int) {
	view := d.View()
	return lipgloss.Width(view), lipgloss.Height(view)
}

func (d *Dialog) KeyMap() help.KeyMap {
	return d.keymap
}

func (d *Dialog) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, d.keymap.Close):
		return util.CmdHandler(util.CloseDialogMsg{})
	case key.Matches(msg, d.keymap.Up):
		if d.cursor > 0 {
			d.cursor--
			d.ensureCursorVisible()
		}
	case key.Matches(msg, d.keymap.Down):
		if d.cursor < len(d.artifacts)-1 {
			d.cursor++
			d.ensureCursorVisible()
		}
	case key.Matches(msg, d.keymap.Toggle):
		if len(d.artifacts) > 0 {
			d.selected[d.cursor] = !d.selected[d.cursor]
			d.selectionRequired = false
		}
	case key.Matches(msg, d.keymap.DownloadAll):
		if len(d.artifacts) > 0 {
			return d.submit(d.artifacts)
		}
	case key.Matches(msg, d.keymap.Download):
		return d.handleDownloadMsg()
	}
	return nil
}

func (d *Dialog) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	d.setSize(msg.Width, msg.Height)
	d.ensureCursorVisible()
	return nil
}

func (d *Dialog) handleDownloadMsg() tea.Cmd {
	selected := d.selectedArtifacts()
	if len(selected) == 0 {
		d.selectionRequired = true
		return util.CmdHandler(util.FooterNotiMsg{
			Message: "Select at least one artifact",
			Type:    util.NotiWarning,
		})
	}
	return d.submit(selected)
}

func (d *Dialog) submit(artifacts []gojenkins.Artifact) tea.Cmd {
	cmds := []tea.Cmd{util.CmdHandler(util.CloseDialogMsg{})}
	if d.onDownload != nil {
		artifacts = append([]gojenkins.Artifact(nil), artifacts...)
		if cmd := d.onDownload(artifacts); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	// Close the chooser before starting I/O. A Batch would allow a fast
	// download result to replace this dialog and then be closed by the stale
	// CloseDialogMsg.
	return tea.Sequence(cmds...)
}

func (d *Dialog) selectedArtifacts() []gojenkins.Artifact {
	artifacts := make([]gojenkins.Artifact, 0, d.selectedCount())
	for i, selected := range d.selected {
		if selected {
			artifacts = append(artifacts, d.artifacts[i])
		}
	}
	return artifacts
}

func (d *Dialog) selectedCount() int {
	count := 0
	for _, selected := range d.selected {
		if selected {
			count++
		}
	}
	return count
}

func (d *Dialog) setSize(width, height int) {
	d.width = width
	d.height = height
	border := styles.GetTheme().S().DialogBorder
	innerWidth := max(1, styles.DialogWidth(width)-border.GetHorizontalFrameSize())
	d.help.SetWidth(innerWidth)
}

func (d *Dialog) visibleRowCount() int {
	if len(d.artifacts) == 0 {
		return 0
	}
	border := styles.GetTheme().S().DialogBorder
	contentHeight := max(1, styles.DialogHeight(d.height)-border.GetVerticalFrameSize())
	// Title, selection summary, and one or two help rows remain fixed.
	fixedRows := 2 + d.helpRowCount(contentHeight)
	return min(len(d.artifacts), max(1, contentHeight-fixedRows))
}

func (d *Dialog) visibleRange(rowCount int) (int, int) {
	if rowCount <= 0 || len(d.artifacts) == 0 {
		return 0, 0
	}
	maxOffset := max(0, len(d.artifacts)-rowCount)
	start := min(max(d.offset, 0), maxOffset)
	return start, min(start+rowCount, len(d.artifacts))
}

func (d *Dialog) ensureCursorVisible() {
	if len(d.artifacts) == 0 {
		d.cursor = 0
		d.offset = 0
		return
	}
	d.cursor = min(max(d.cursor, 0), len(d.artifacts)-1)
	rowCount := d.visibleRowCount()
	if d.cursor < d.offset {
		d.offset = d.cursor
	}
	if d.cursor >= d.offset+rowCount {
		d.offset = d.cursor - rowCount + 1
	}
	d.offset = min(max(d.offset, 0), max(0, len(d.artifacts)-rowCount))
}

func (d *Dialog) helpRowCount(contentHeight int) int {
	if contentHeight >= 5 {
		return 2
	}
	return 1
}

func (d *Dialog) renderHelp(contentHeight int) []string {
	if d.helpRowCount(contentHeight) == 1 {
		return []string{d.help.ShortHelpView([]key.Binding{
			d.keymap.DownloadAll,
			d.keymap.Download,
			d.keymap.Close,
		})}
	}
	return []string{
		d.help.ShortHelpView([]key.Binding{d.keymap.Up, d.keymap.Down, d.keymap.Toggle}),
		d.help.ShortHelpView([]key.Binding{d.keymap.Download, d.keymap.DownloadAll, d.keymap.Close}),
	}
}
