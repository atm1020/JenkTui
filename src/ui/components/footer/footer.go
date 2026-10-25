package footer

import (
	"fmt"
	"time"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type Status string

const (
	Running Status = "Running"
	Error
	Ready
)

const notificationTTLSeconds = 5

type Model struct {
	ctx   *context.ProgramContext
	wWith int

	currentWidth  int
	currentHeight int

	currentPage string

	user string

	currentMsg     string
	currentMsgType util.NotiType
}

func NewModel(ctx *context.ProgramContext, user string) *Model {
	return &Model{
		ctx:         ctx,
		currentPage: "home",
		user:        user,
	}
}

func (m *Model) Init() tea.Cmd {
	return nil
}

func (m *Model) Update(msg tea.Msg) (*Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.wWith = msg.Width
	case time.Time:
		m.currentMsg = ""
	case util.FooterUrlMsg:
		m.currentPage = msg.Url
	case util.FooterNotiMsg:
		m.currentMsg = msg.Message
		m.currentMsgType = msg.Type
		return m, m.scheduleFooterMsgClean()
	case FooterCleanProcessMsg:
		m.currentMsg = ""
	}
	return m, nil
}

func (m *Model) View() string {
	t := styles.GetTheme()

	w := lipgloss.Width

	userStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Black).
		Background(lipgloss.White).
		Padding(0, 1).
		Render(styles.UserIcon + m.user)

	helpIndicatorStyle := lipgloss.NewStyle().
		Background(t.Warning).
		Foreground(t.BgBase).
		Padding(0, 1)

	msg := ""
	if m.currentMsg != "" {
		bgColor := t.Info // Default to info
		switch m.currentMsgType {
		case util.NotiWarning:
			bgColor = t.Warning
		case util.NotiError:
			bgColor = t.Error
		case util.NotiSuccess:
			bgColor = t.Success
		case util.NotiSkipped:
			bgColor = t.Grey
		}
		msgStyle := lipgloss.NewStyle().Foreground(t.BgBase).Background(bgColor).Bold(true).Padding(0, 1)
		msg = msgStyle.Render(m.currentMsg)
		// Add margin between notification and help when notification is shown
		helpIndicatorStyle = helpIndicatorStyle.MarginLeft(1)
	}

	helpIndicator := helpIndicatorStyle.Render(fmt.Sprintf("%s help", m.ctx.Config.KeyMapConfig.Global.HelpToggle))

	pageDisplay := lipgloss.NewStyle().Foreground(t.FooterPageFg).Render(m.currentPage)

	statusVal := t.S().Footer.Padding(0, 1).Width(m.wWith - w(userStyle) - w(msg) - w(helpIndicator)).Render(pageDisplay)

	bar := lipgloss.JoinHorizontal(lipgloss.Left,
		userStyle,
		statusVal,
		msg,
		helpIndicator,
	)

	return lipgloss.NewStyle().Width(m.wWith).Render(bar)
}

func (m *Model) SetCurrentPage(page string) {
	m.currentPage = page
}

func (m *Model) scheduleFooterMsgClean() tea.Cmd {
	return tea.Tick(notificationTTLSeconds*time.Second, func(t time.Time) tea.Msg {
		return FooterCleanProcessMsg{}
	})
}
