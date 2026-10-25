package themeselector

import (
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

type ThemeSelectorDialog struct {
	wWidth, wHeight     int
	list                *list.Model
	keyMap              ThemeSelectorKeyMap
	help                help.Model
	currentTheme        styles.ThemeType
	horizontalLineWidth int
}

func NewThemeSelectorDialog(ctx *context.ProgramContext, width, height int, currentTheme styles.ThemeType) *ThemeSelectorDialog {
	// Use user's keymap config from context
	keymapConf := ctx.Config.KeyMapConfig.Dialog.ThemeSelector
	keymap := UserKeyMap(keymapConf)

	h := help.New()

	m := &ThemeSelectorDialog{
		keyMap:       keymap,
		help:         h,
		currentTheme: currentTheme,
	}
	m.list = initList(1, 1, 2, currentTheme)
	m.resize(width, height)
	return m
}

// resize recomputes the dialog's width/height from the window size — through
// the shared dialog floor/ceiling — and resizes everything derived from it
// (the list, help, and divider lines). Called both at construction and on
// every tea.WindowSizeMsg so the dialog never drifts back to the raw window
// size after the first resize.
func (m *ThemeSelectorDialog) resize(width, height int) {
	// The theme list never grows past ~a dozen items, so on top of the shared
	// floor it keeps its own small content ceiling instead of stretching to
	// the shared dialog ceiling and leaving a tall empty void below the list.
	wWidth := min(50, styles.ClampDialogWidth(width/3, width))
	wHeight := min(12, styles.ClampDialogHeight(height-10, height))
	horizontalLineWidth := max(1, wWidth-8)

	m.wWidth = wWidth
	m.wHeight = wHeight
	m.horizontalLineWidth = horizontalLineWidth
	m.help.SetWidth(wWidth)
	m.list.SetSize(wWidth, wHeight)
	m.list.SetDelegate(ThemeDelegate{width: wWidth, horizontalLineWidth: horizontalLineWidth})
}

func (m *ThemeSelectorDialog) Init() tea.Cmd {
	return nil
}

func (m *ThemeSelectorDialog) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Let the Bubbles list own text keys while its filter input is active;
		// otherwise configured j/k navigation would make those letters
		// impossible to type. Unhandled keys (notably "/") also fall through to
		// the list so filtering can start.
		if key.Matches(msg, m.keyMap.Close) || !m.list.SettingFilter() {
			if cmd := m.handleKeyPress(msg); cmd != nil {
				return m, cmd
			}
		}
	case tea.WindowSizeMsg:
		m.resize(msg.Width, msg.Height)
	}

	list, cmd := m.list.Update(msg)
	m.list = &list

	return m, cmd
}

func (m *ThemeSelectorDialog) View() string {
	t := styles.GetTheme()

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(t.Primary).
		Padding(0, 1).
		Render("Select Theme")

	listView := m.list.View()

	helpView := lipgloss.NewStyle().
		Foreground(t.Grey).
		Padding(0, 1).
		Render(m.help.ShortHelpView(m.keyMap.ShortHelp()))

	// Pad every part to the widest part's width, then size the container to fit.
	content, w := util.JoinDialogParts(
		title,
		util.HorizontalLine(m.horizontalLineWidth, t.Border),
		listView,
		util.HorizontalLine(m.horizontalLineWidth, t.Border),
		helpView,
	)

	border := t.S().DialogBorder
	return border.Width(w + border.GetHorizontalFrameSize()).Render(content)
}

// UpdateFooter updates the footer (required by JenkTuiDialogModel interface)
func (m *ThemeSelectorDialog) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl("Theme Selector")
}

func (m *ThemeSelectorDialog) CleanUp() tea.Cmd {
	return nil
}

// Dimensions returns the dialog dimensions
func (m *ThemeSelectorDialog) Dimensions() (int, int) {
	return m.wWidth, m.wHeight
}

func (m *ThemeSelectorDialog) KeyMap() help.KeyMap {
	return m.keyMap
}

// handleKeyPress handles key press events
func (m *ThemeSelectorDialog) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, m.keyMap.Close) {
		styles.SwitchTheme(m.currentTheme)
		return util.CmdHandler(util.CloseDialogMsg{})
	}

	if key.Matches(msg, m.keyMap.Up) {
		m.list.CursorUp()
		styles.SwitchTheme(m.getSelectedThemeTypeItem())
		return util.CmdHandler(util.ThemePreviewMsg{})
	}

	if key.Matches(msg, m.keyMap.Down) {
		m.list.CursorDown()
		styles.SwitchTheme(m.getSelectedThemeTypeItem())
		return util.CmdHandler(util.ThemePreviewMsg{})
	}

	if key.Matches(msg, m.keyMap.Select) {
		selectedItem := m.list.SelectedItem()
		if selectedItem != nil {
			themeItem, ok := selectedItem.(ThemeItem)
			if ok {
				// styles.SwitchTheme(themeItem.themeType)
				return util.CmdHandler(util.ThemeChangedMsg{
					ThemeType: themeItem.themeType,
				})
			}
		}
	}

	return nil
}

func (m *ThemeSelectorDialog) getSelectedThemeTypeItem() styles.ThemeType {
	selectedItem := m.list.SelectedItem()
	themeItem, _ := selectedItem.(ThemeItem)
	return themeItem.themeType
}

func initList(horizontalLineWidth, wWidth, wHeight int, currentTheme styles.ThemeType) *list.Model {
	themes := styles.AvailableThemeTypes()
	items := make([]list.Item, len(themes))
	currentIndex := 0
	for i, themeType := range themes {
		isCurrent := themeType == currentTheme
		items[i] = NewThemeItem(themeType, isCurrent)
		if isCurrent {
			currentIndex = i
		}
	}

	delegate := ThemeDelegate{
		width:               wWidth,
		horizontalLineWidth: horizontalLineWidth,
	}

	l := list.New(items, delegate, wWidth, wHeight)
	l.SetShowHelp(false)
	l.SetShowTitle(false)
	l.SetShowStatusBar(false)
	l.SetShowPagination(true)
	l.SetFilteringEnabled(true)
	l.DisableQuitKeybindings()
	l.Select(currentIndex)
	return &l
}
