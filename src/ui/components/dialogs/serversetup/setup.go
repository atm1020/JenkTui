package serversetup

import (
	"context"
	"strings"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	appcontext "github.com/atm1020/jenktui/src/context"
	setup "github.com/atm1020/jenktui/src/serversetup"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"
	"github.com/google/uuid"
)

// Settings is deliberately credential-free. The form cannot load a stored token.
type Settings struct{ ID, Name, URL, Username, CAPath string }

type SavedMsg struct {
	Dialog *Dialog
	ID     string
}
type (
	CancelMsg   struct{ Dialog *Dialog }
	verifiedMsg struct {
		owner    *Dialog
		verified *setup.Verified
		err      error
	}
)

type savedMsg struct {
	owner *Dialog
	err   error
}

type Dialog struct {
	dir, id                 string
	editing                 bool
	inputs                  []textinput.Model
	focus                   int
	advanced                bool
	width                   int
	keys                    KeyMap
	help                    help.Model
	testing, saving, closed bool
	cancel                  context.CancelFunc
	errorText               string
}

func New(dir string, settings Settings, width int, conf appcontext.ServerSetupDialogKeyMapConfig) *Dialog {
	d := &Dialog{dir: dir, id: settings.ID, editing: settings.ID != "", width: width, keys: UserKeyMap(conf), help: help.New()}
	if d.id == "" {
		d.id = uuid.NewString()
	}
	values := []string{settings.Name, settings.URL, settings.Username, "", settings.CAPath}
	placeholders := []string{"My Jenkins", "https://jenkins.example.com", "Username", "API token", "Optional PEM certificate path"}
	for i := range values {
		input := textinput.New()
		input.SetVirtualCursor(true)
		input.Prompt = "> "
		input.Placeholder = placeholders[i]
		input.SetValue(values[i])
		if i == 3 {
			input.EchoMode = textinput.EchoPassword
			input.EchoCharacter = '*'
			if d.editing {
				input.Placeholder = "**.."
			}
		}
		d.inputs = append(d.inputs, input)
	}
	d.resize(width)
	return d
}

func (d *Dialog) Init() tea.Cmd { return d.inputs[d.focus].Focus() }

func (d *Dialog) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return d, d.handleKey(msg)
	case tea.WindowSizeMsg:
		d.resize(msg.Width)
	case verifiedMsg:
		return d, d.handleVerified(msg)
	case savedMsg:
		return d, d.handleSaved(msg)
	default:
		if !d.testing && !d.saving && !d.closed {
			var cmd tea.Cmd
			d.inputs[d.focus], cmd = d.inputs[d.focus].Update(msg)
			return d, cmd
		}
	}
	return d, nil
}

func (d *Dialog) View() string {
	t := styles.GetTheme()
	title := "Set up Jenkins server"
	if d.editing {
		title = "Edit Jenkins server"
	}
	parts := []string{lipgloss.NewStyle().Bold(true).Foreground(t.Primary).Render(title), ""}
	labels := []string{"Name", "URL", "Username", "API token", "CA certificate (PEM)"}
	for i := 0; i < d.fieldCount(); i++ {
		parts = append(parts, labels[i], d.inputs[i].View())
	}
	if d.editing {
		parts = append(parts, "Leave token unchanged to keep the stored credential.")
	}
	if d.testing {
		parts = append(parts, "Testing connection… (five-second timeout)")
	}
	if d.saving {
		parts = append(parts, "Saving server…")
	}
	if d.errorText != "" {
		parts = append(parts, lipgloss.NewStyle().Foreground(t.Warning).Render(d.errorText))
	}
	parts = append(parts, "", d.help.ShortHelpView(d.keys.ShortHelp()))
	return t.S().DialogBorder.Width(d.contentWidth()).Render(strings.Join(parts, "\n"))
}

func (d *Dialog) UpdateFooter() tea.Cmd { return util.CmdFooterUrl("Server Setup") }
func (d *Dialog) CleanUp() tea.Cmd {
	d.closed = true
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	d.inputs[3].SetValue("")
	return nil
}

func (d *Dialog) Dimensions() (int, int) {
	view := d.View()
	return lipgloss.Width(view), lipgloss.Height(view)
}
func (d *Dialog) KeyMap() help.KeyMap { return d.keys }

// The form's fields read best well short of the shared dialog ceiling, so
// keep its own 68-col cap on top of the shared floor.
func (d *Dialog) contentWidth() int { return min(68, styles.ClampDialogWidth(d.width-6, d.width)) }

func (d *Dialog) resize(width int) {
	d.width = width
	for i := range d.inputs {
		d.inputs[i].SetWidth(max(1, d.contentWidth()-4))
	}
	d.help.SetWidth(d.contentWidth())
}

func (d *Dialog) fieldCount() int {
	if d.advanced {
		return 5
	}
	return 4
}

func (d *Dialog) handleKey(msg tea.KeyPressMsg) tea.Cmd {
	if d.closed || d.saving {
		return nil
	}
	if key.Matches(msg, d.keys.Close) {
		_ = d.CleanUp()
		return util.CmdHandler(CancelMsg{Dialog: d})
	}
	if d.testing {
		return nil
	}
	switch {
	case key.Matches(msg, d.keys.Next):
		return d.move(1)
	case key.Matches(msg, d.keys.Previous):
		return d.move(-1)
	case key.Matches(msg, d.keys.Advanced):
		d.advanced = !d.advanced
		if d.focus >= d.fieldCount() {
			return d.move(-1)
		}
		return nil
	case key.Matches(msg, d.keys.Submit):
		return d.test()
	default:
		var cmd tea.Cmd
		d.inputs[d.focus], cmd = d.inputs[d.focus].Update(msg)
		return cmd
	}
}

func (d *Dialog) move(delta int) tea.Cmd {
	d.inputs[d.focus].Blur()
	d.focus = (d.focus + delta + d.fieldCount()) % d.fieldCount()
	return d.inputs[d.focus].Focus()
}

func (d *Dialog) test() tea.Cmd {
	d.testing = true
	d.errorText = ""
	draft := setup.Draft{ID: d.id, Name: d.inputs[0].Value(), URL: d.inputs[1].Value(), Username: d.inputs[2].Value(), ReplacementToken: d.inputs[3].Value(), CAPath: d.inputs[4].Value()}
	ctx, cancel := context.WithCancel(context.Background())
	d.cancel = cancel
	dir := d.dir
	return func() tea.Msg {
		verified, err := setup.Verify(ctx, dir, draft)
		return verifiedMsg{owner: d, verified: verified, err: err}
	}
}

func (d *Dialog) handleVerified(msg verifiedMsg) tea.Cmd {
	if msg.owner != d || d.closed || !d.testing {
		return nil
	}
	d.testing = false
	if d.cancel != nil {
		d.cancel()
		d.cancel = nil
	}
	if msg.err != nil {
		d.errorText = msg.err.Error()
		return nil
	}
	d.saving = true
	dir := d.dir
	return func() tea.Msg { return savedMsg{owner: d, err: msg.verified.Save(dir)} }
}

func (d *Dialog) handleSaved(msg savedMsg) tea.Cmd {
	if msg.owner != d || d.closed || !d.saving {
		return nil
	}
	if msg.err != nil {
		d.saving = false
		d.errorText = msg.err.Error()
		return nil
	}
	d.inputs[3].SetValue("")
	return util.CmdHandler(SavedMsg{Dialog: d, ID: d.id})
}
