package util

import (
	"fmt"

	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Spinner struct {
	id      int
	spinner spinner.Model
	label   string
	active  bool
}

func NewSpinner(label string) Spinner {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(styles.GetTheme().SpinnerColor)

	return Spinner{
		id:      nextLoadingID(),
		spinner: s,
		label:   label,
		active:  false,
	}
}

var lastLoadingID int

func nextLoadingID() int {
	lastLoadingID++
	return lastLoadingID
}

func (l *Spinner) Init() tea.Cmd {
	return l.spinner.Tick
}

func (l *Spinner) Update(msg tea.Msg) tea.Cmd {
	if !l.active {
		return nil
	}

	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		l.spinner, cmd = l.spinner.Update(msg)
		return cmd
	}
	return nil
}

func (l *Spinner) View() string {
	if !l.active {
		return ""
	}
	return fmt.Sprintf("%s %s", l.spinner.View(), l.label)
}

func (l *Spinner) Start() {
	l.active = true
}

func (l *Spinner) Stop() {
	l.active = false
}
