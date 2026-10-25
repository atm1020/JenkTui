package globalsearch

import (
	"fmt"
	"image/color"
	"strings"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
)

type GlobalSearchLegacyFetchedMsg struct {
	Results *gojenkins.LegacySearchResponse
}

type FocusedView int

const (
	SearchInput FocusedView = iota
	SearchRresult
)

// minimum server version that supports new global search
const newGlobalSearchReleasedServerVersion = "2.492"

type GlobalSearchFetchedMsg struct {
	Results *gojenkins.SearchResponse
}

type GlobalSearchDialogCmp struct {
	ctx               *context.ProgramContext
	wWidth            int
	wHeight           int
	keyMap            SearchInputKeyMap
	help              help.Model
	focusedView       FocusedView
	searchResultModel *SearchResultModel
	searchInputModel  *SearchInputModel
	isLegacySearch    bool
}

func NewGlobalSearchDialogCmp(ctx *context.ProgramContext, width, height int) *GlobalSearchDialogCmp {
	// Clamp so the overlay stays readable and inside the window: the old width/3,
	// height/4 shrank to an unusable sliver on narrow/short terminals and left the
	// results list only a handful of rows tall.
	wWidth := styles.DialogWidth(width)
	wHeight := styles.DialogHeight(height)

	help := help.New()
	help.SetWidth(wWidth)

	// Use user's keymap config from context
	keymapConf := ctx.Config.KeyMapConfig.Dialog.Globalsearch
	keymap := UserSearchInputKeyMap(keymapConf) // init view is search input

	log.Debug("JENINS INSTANCE VERSION %s", ctx.Jenkins.Version)
	return &GlobalSearchDialogCmp{
		wWidth:            wWidth,
		wHeight:           wHeight,
		help:              help,
		keyMap:            keymap,
		ctx:               ctx,
		focusedView:       SearchInput,
		searchResultModel: NewSearcResultModel(ctx, wWidth, wHeight, keymapConf),
		searchInputModel:  NewSearchInputModel(keymapConf),
		isLegacySearch:    !ctx.Jenkins.IsVersionGreaterOrEqual(newGlobalSearchReleasedServerVersion),
	}
}

func (m *GlobalSearchDialogCmp) Init() tea.Cmd {
	return m.searchInputModel.Init()
}

func (m *GlobalSearchDialogCmp) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m, m.handleKeyPressMsg(msg)
	case FetchGlobalSearchResultsMsg:
		_, cmd = m.searchResultModel.Update(msg)
		return m, tea.Batch(cmd, m.fetchGlobalSearchResults(msg.Query))
	case SearchResultItemSelectedMsg:
		return m, util.CmdHandler(util.OpenJobTargetMsg{Target: msg.Target})
	case GlobalSearchFetchedMsg:
		return m, m.handleGlobalSearchFetchedMsg(msg)
	case GlobalSearchLegacyFetchedMsg:
		return m, m.handleGlobalSearchLegacyFetchedMsg(msg)
	}
	return m, m.updateComponents(msg)
}

func (m *GlobalSearchDialogCmp) View() string {
	var searchLineColor color.Color
	var resultLineColor color.Color

	t := styles.GetTheme()

	var keyMap help.KeyMap
	if m.focusedView == SearchInput {
		searchLineColor = t.StageFocusedBorder // Orange for focused
		resultLineColor = t.Border
		keyMap = m.searchInputModel.keyMap
	} else {
		searchLineColor = t.Border
		resultLineColor = t.StageFocusedBorder // Orange for focused
		keyMap = m.searchResultModel.keyMap
	}

	resultView := m.searchResultModel.View()

	parts := []string{
		m.fillWidth(m.searchInputModel.View()),
		m.fillWidth(util.HorizontalLine(m.wWidth, searchLineColor)),
		m.fillWidth(resultView),
		m.fillWidth(util.HorizontalLine(m.wWidth, resultLineColor)),
		m.fillWidth(m.help.View(keyMap)),
	}

	if m.isLegacySearch {
		msg := fmt.Sprintf("Legacy search enabled (server version < %s)", newGlobalSearchReleasedServerVersion)
		parts = append(parts,
			m.fillWidth(util.HorizontalLine(m.wWidth, t.Border)),
			m.fillWidth(lipgloss.NewStyle().Italic(true).Foreground(t.Warning).Render(msg)),
		)
	}

	// Join with plain newlines (not JoinVertical, which inserts unstyled
	// padding) and let the fixed-width container own the frame.
	contenet := strings.Join(parts, "\n")
	border := t.S().DialogBorder
	return border.Width(m.wWidth + border.GetHorizontalFrameSize()).Render(contenet)
}

func (m *GlobalSearchDialogCmp) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl("Global Search")
}

func (m *GlobalSearchDialogCmp) CleanUp() tea.Cmd {
	m.searchInputModel.textInput.Blur()
	return nil
}

func (m *GlobalSearchDialogCmp) Dimensions() (int, int) {
	return m.wWidth, m.wHeight
}

func (m *GlobalSearchDialogCmp) KeyMap() help.KeyMap {
	return m.keyMap
}

// fillWidth paints a dialog part across the full inner width with the base
// background so no unstyled cells leak the terminal's default background.
func (m *GlobalSearchDialogCmp) fillWidth(s string) string {
	return util.PadWidth(s, m.wWidth)
}

func (s *GlobalSearchDialogCmp) updateComponents(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	var cmd tea.Cmd

	_, cmd = s.searchInputModel.Update(msg)
	cmds = append(cmds, cmd)

	_, cmd = s.searchResultModel.Update(msg)
	cmds = append(cmds, cmd)

	return tea.Batch(cmds...)
}

func (s *GlobalSearchDialogCmp) fetchGlobalSearchResults(searchText string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := s.ctx.RequestContext()
		defer cancel()
		if s.isLegacySearch {
			result, err := s.ctx.Jenkins.GlobalSearchLegacy(ctx, searchText)
			if err != nil {
				return util.ErrorMsg{
					Title: "Could not get result",
					Error: err,
				}
			}
			log.Warn("Legacy search result: '%s'", result)
			return GlobalSearchLegacyFetchedMsg{Results: result}
		}

		result, err := s.ctx.Jenkins.GlobalSearch(ctx, searchText)
		if err != nil {
			return util.ErrorMsg{
				Title: "Could not get result",
				Error: err,
			}
		}
		log.Warn("Search result: '%s'", result)
		return GlobalSearchFetchedMsg{Results: result}
	}
}

func (m *GlobalSearchDialogCmp) handleKeyPressMsg(msg tea.KeyMsg) tea.Cmd {
	// While the results list is capturing a filter query, every key belongs to it
	// (typing, enter to apply, esc to cancel). Route straight to it so the dialog's
	// own focus/quit bindings don't steal keystrokes.
	if m.focusedView == SearchRresult && m.searchResultModel.IsFiltering() {
		_, cmd := m.searchResultModel.Update(msg)
		return cmd
	}

	if key.Matches(msg, m.keyMap.Help) {
		m.help.ShowAll = !m.help.ShowAll
	}

	if key.Matches(msg, m.keyMap.Quit) {
		return util.CmdHandler(util.CloseDialogMsg{})
	}

	if key.Matches(msg, m.keyMap.ToggleFocus) {
		if m.focusedView == SearchInput {
			return m.focusToSearchResult()
		} else {
			return m.focusToSearchInput()
		}
	}

	if key.Matches(msg, m.keyMap.FocusDown) {
		if m.focusedView == SearchInput {
			return m.focusToSearchResult()
		}
		return nil
	}

	if key.Matches(msg, m.keyMap.FocusUp) {
		if m.focusedView == SearchRresult {
			return m.focusToSearchInput()
		}
		return nil
	}

	var cmd tea.Cmd
	if m.focusedView == SearchInput {
		_, cmd = m.searchInputModel.Update(msg)
	}

	if m.focusedView == SearchRresult {
		_, cmd = m.searchResultModel.Update(msg)
	}

	return cmd
}

func (m *GlobalSearchDialogCmp) handleGlobalSearchFetchedMsg(msg GlobalSearchFetchedMsg) tea.Cmd {
	var cmd tea.Cmd
	items := make([]list.Item, len(msg.Results.Suggestions))
	for idx, suggestion := range msg.Results.Suggestions {
		items[idx] = NewSearchResultItem(suggestion)
	}
	_, cmd = m.searchResultModel.Update(UpdateListItemMsg{
		Items:  items,
		Width:  m.wWidth,
		Height: m.wHeight,
	})

	if len(items) > 0 {
		return tea.Batch(cmd, m.focusToSearchResult())
	}

	return cmd
}

func (m *GlobalSearchDialogCmp) focusToSearchResult() tea.Cmd {
	if m.focusedView == SearchRresult {
		return nil
	}
	m.searchInputModel.textInput.Blur()
	m.focusedView = SearchRresult
	return nil
}

func (m *GlobalSearchDialogCmp) focusToSearchInput() tea.Cmd {
	if m.focusedView == SearchInput {
		return nil
	}
	cmd := m.searchInputModel.textInput.Focus()
	m.focusedView = SearchInput
	return cmd
}

func (m *GlobalSearchDialogCmp) handleGlobalSearchLegacyFetchedMsg(msg GlobalSearchLegacyFetchedMsg) tea.Cmd {
	var cmd tea.Cmd
	items := make([]list.Item, len(msg.Results.Suggestions))
	for idx, suggestion := range msg.Results.Suggestions {
		items[idx] = LegacySearchResultItem{LegacySuggestion: suggestion}
	}
	_, cmd = m.searchResultModel.Update(UpdateListItemMsg{
		Items:  items,
		Width:  m.wWidth,
		Height: m.wHeight,
	})

	if len(items) > 0 {
		return tea.Batch(cmd, m.focusToSearchResult())
	}

	return cmd
}
