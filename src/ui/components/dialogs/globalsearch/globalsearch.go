package globalsearch

import (
	stdctx "context"
	"fmt"
	"image/color"
	"strings"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type FocusedView int

const (
	SearchInput FocusedView = iota
	SearchRresult
)

// minimum server version that supports new global search
const newGlobalSearchReleasedServerVersion = "2.492"

type searchFetchedMsg struct {
	owner   *GlobalSearchDialogCmp
	request uint64
	items   []list.Item
	err     error
}

type GlobalSearchDialogCmp struct {
	ctx                 *context.ProgramContext
	wWidth              int
	wHeight             int
	keyMap              SearchInputKeyMap
	help                help.Model
	focusedView         FocusedView
	searchResultModel   *SearchResultModel
	searchInputModel    *SearchInputModel
	isLegacySearch      bool
	favoritesScope      bool
	favoriteKeys        favoriteKeys
	favorites           []store.FavoriteRecord
	favoritesLoading    bool
	favoritesError      error
	appWidth, appHeight int
	request             uint64
	cancelSearch        stdctx.CancelFunc
}

func NewGlobalSearchDialogCmp(ctx *context.ProgramContext, width, height int) *GlobalSearchDialogCmp {
	return newSearchDialog(ctx, width, height, false)
}

func NewFavoritesDialog(ctx *context.ProgramContext, width, height int) *GlobalSearchDialogCmp {
	return newSearchDialog(ctx, width, height, true)
}

func newSearchDialog(ctx *context.ProgramContext, width, height int, favoritesScope bool) *GlobalSearchDialogCmp {
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

	m := &GlobalSearchDialogCmp{
		appWidth:          width,
		appHeight:         height,
		favoritesScope:    favoritesScope,
		favoriteKeys:      newFavoriteKeys(ctx.Config.KeyMapConfig.Favorites),
		favoritesLoading:  ctx.RunStore != nil,
		wWidth:            wWidth,
		wHeight:           wHeight,
		help:              help,
		keyMap:            keymap,
		ctx:               ctx,
		focusedView:       SearchInput,
		searchResultModel: NewSearcResultModel(ctx, wWidth, wHeight, keymapConf),
		searchInputModel:  NewSearchInputModel(keymapConf),
		isLegacySearch:    ctx.Jenkins != nil && !ctx.Jenkins.IsVersionGreaterOrEqual(newGlobalSearchReleasedServerVersion),
	}
	m.searchInputModel.textInput.SetWidth(min(GlobalSearchDialogWidth, wWidth))
	m.updateScopeInput()
	return m
}

func (m *GlobalSearchDialogCmp) Init() tea.Cmd {
	return tea.Batch(m.searchInputModel.Init(), m.loadFavorites())
}

func (m *GlobalSearchDialogCmp) Update(msg tea.Msg) (util.JenkTuiDialogModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		return m, m.handleKeyPressMsg(msg)
	case tea.WindowSizeMsg:
		return m, m.resize(msg)
	case SearchResultItemSelectedMsg:
		return m, util.CmdHandler(util.OpenJobTargetMsg{Target: msg.Target})
	case searchFetchedMsg:
		return m, m.handleSearchFetched(msg)
	case favoritesLoadedMsg:
		return m, m.handleFavoritesLoaded(msg)
	case favoriteRemovedMsg:
		return m, m.handleFavoriteRemoved(msg)
	}
	before := m.searchInputModel.textInput.Value()
	cmd := m.updateComponents(msg)
	m.refreshOnInputChange(before)
	return m, cmd
}

func (m *GlobalSearchDialogCmp) View() string {
	var searchLineColor color.Color
	var resultLineColor color.Color

	t := styles.GetTheme()

	var keyMap help.KeyMap
	if m.focusedView == SearchInput {
		searchLineColor = t.StageFocusedBorder // Orange for focused
		resultLineColor = t.Border
		inputKeys := m.searchInputModel.keyMap
		if m.favoritesScope {
			inputKeys.Search.SetHelp(inputKeys.Search.Help().Key, "focus results")
		}
		keyMap = inputKeys
	} else {
		searchLineColor = t.Border
		resultLineColor = t.StageFocusedBorder // Orange for focused
		keyMap = m.searchResultModel.keyMap
	}

	resultView := m.searchResultModel.View()
	scope := "Jenkins"
	if m.favoritesScope {
		scope = "Favorites"
	}

	parts := []string{
		m.fillWidth(m.searchInputModel.View()),
		m.scopeDivider(scope, searchLineColor),
		m.fillWidth(resultView),
		m.fillWidth(util.HorizontalLine(m.wWidth, resultLineColor)),
		m.fillWidth(m.help.View(keyMap)),
	}
	if actions := m.favoriteActionsHelp(); actions != "" {
		parts = append(parts, m.fillWidth(actions))
	}

	if m.isLegacySearch && !m.favoritesScope {
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
	m.stopSearch()
	m.searchInputModel.textInput.Blur()
	return nil
}

func (m *GlobalSearchDialogCmp) resize(msg tea.WindowSizeMsg) tea.Cmd {
	m.appWidth, m.appHeight = msg.Width, msg.Height
	m.wWidth, m.wHeight = styles.DialogWidth(msg.Width), styles.DialogHeight(msg.Height)
	m.help.SetWidth(m.wWidth)
	m.searchInputModel.textInput.SetWidth(min(GlobalSearchDialogWidth, m.wWidth))
	return m.searchResultModel.handleWindowSize(tea.WindowSizeMsg{Width: m.wWidth, Height: m.wHeight})
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

func (m *GlobalSearchDialogCmp) fetchGlobalSearchResults(query string) tea.Cmd {
	m.stopSearch()
	ctx, cancel := m.ctx.RequestContext()
	m.cancelSearch = cancel
	jenkins, legacy, request := m.ctx.Jenkins, m.isLegacySearch, m.request
	return func() tea.Msg {
		defer cancel()
		msg := searchFetchedMsg{owner: m, request: request}
		if legacy {
			result, err := jenkins.GlobalSearchLegacy(ctx, query)
			msg.err = err
			if err == nil {
				for _, item := range result.Suggestions {
					msg.items = append(msg.items, LegacySearchResultItem{LegacySuggestion: item})
				}
			}
		} else {
			result, err := jenkins.GlobalSearch(ctx, query)
			msg.err = err
			if err == nil {
				for _, item := range result.Suggestions {
					msg.items = append(msg.items, NewSearchResultItem(item))
				}
			}
		}
		return msg
	}
}

func (m *GlobalSearchDialogCmp) stopSearch() {
	m.request++
	if m.cancelSearch != nil {
		m.cancelSearch()
		m.cancelSearch = nil
	}
}

func (m *GlobalSearchDialogCmp) startSearch() tea.Cmd {
	query := strings.TrimSpace(m.searchInputModel.textInput.Value())
	if query == "" || m.ctx.Jenkins == nil {
		return nil
	}
	m.searchResultModel.setItems(nil)
	return tea.Batch(m.searchResultModel.handleFetchResults(), m.fetchGlobalSearchResults(query))
}

func (m *GlobalSearchDialogCmp) handleKeyPressMsg(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, m.keyMap.ToggleScope) {
		return m.toggleScope()
	}
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
		return util.CmdHandler(util.CloseDialogMsg{Dialog: m})
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

	if m.focusedView == SearchInput {
		return m.handleInputKey(msg)
	}
	if m.favoritesScope {
		if cmd, handled := m.handleFavoriteAction(msg); handled {
			return cmd
		}
	}
	_, cmd := m.searchResultModel.Update(msg)
	return cmd
}

func (m *GlobalSearchDialogCmp) handleInputKey(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, m.searchInputModel.keyMap.Search) {
		if m.favoritesScope {
			if len(m.searchResultModel.list.Items()) > 0 {
				return m.focusToSearchResult()
			}
			return nil
		}
		return m.startSearch()
	}
	before := m.searchInputModel.textInput.Value()
	_, cmd := m.searchInputModel.Update(msg)
	m.refreshOnInputChange(before)
	return cmd
}

func (m *GlobalSearchDialogCmp) refreshOnInputChange(before string) {
	if before == m.searchInputModel.textInput.Value() {
		return
	}
	if m.favoritesScope {
		m.refreshFavorites()
	} else if m.searchResultModel.isLoading {
		m.stopSearch()
		m.searchResultModel.isLoading = false
	}
}

func (m *GlobalSearchDialogCmp) handleSearchFetched(msg searchFetchedMsg) tea.Cmd {
	if msg.owner != m || msg.request != m.request || m.favoritesScope {
		return nil
	}
	m.searchResultModel.setItems(msg.items)
	m.searchResultModel.emptyText = "No Jenkins results found"
	if msg.err != nil {
		return util.CmdHandler(util.ErrorMsg{Title: "Could not get result", Error: msg.err})
	}
	if len(msg.items) > 0 {
		return m.focusToSearchResult()
	}
	return nil
}

func (m *GlobalSearchDialogCmp) focusToSearchResult() tea.Cmd {
	if m.focusedView == SearchRresult {
		return nil
	}
	m.searchInputModel.textInput.Blur()
	m.focusedView = SearchRresult
	m.searchResultModel.setFocused(true)
	return nil
}

func (m *GlobalSearchDialogCmp) focusToSearchInput() tea.Cmd {
	if m.focusedView == SearchInput {
		return nil
	}
	cmd := m.searchInputModel.textInput.Focus()
	m.focusedView = SearchInput
	m.searchResultModel.setFocused(false)
	return cmd
}

func (m *GlobalSearchDialogCmp) scopeDivider(scope string, lineColor color.Color) string {
	label := ansi.Truncate(scope, m.wWidth, "…")
	prefixWidth := m.wWidth - lipgloss.Width(label) - 1
	if prefixWidth <= 0 {
		return lipgloss.PlaceHorizontal(m.wWidth, lipgloss.Right, label)
	}
	return util.HorizontalLine(prefixWidth, lineColor) + " " + label
}

func (m *GlobalSearchDialogCmp) favoriteActionsHelp() string {
	if !m.favoritesScope || m.focusedView != SearchRresult {
		return ""
	}
	return m.help.ShortHelpView([]key.Binding{m.favoriteKeys.remove, m.favoriteKeys.runs})
}
