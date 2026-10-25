package globalsearch

import (
	"fmt"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
)

// SearchResultItemSelectedMsg carries the navigation target of the picked
// result. Modern Jenkins search reports each suggestion's class, so a
// searched-for folder keeps its classification instead of being rediscovered.
// Whether JenkTui has a page for the target is HandleJobTarget's decision.
type SearchResultItemSelectedMsg struct {
	Target jobref.Target
}

type UpdatedListItemsMsg struct {
	Items []list.Item
}

type SearchResultItem struct {
	*gojenkins.Suggestion
}

type UpdateListItemMsg struct {
	Items  []list.Item
	Width  int
	Height int
}

func (d SearchResultItem) FilterValue() string {
	return d.Name
}

func (d SearchResultItem) Title() string {
	return d.Name
}

func (d SearchResultItem) Description() string {
	return fmt.Sprintf("%s [%s]", d.Group, d.URL)
}

func NewSearchResultItem(suggestion *gojenkins.Suggestion) SearchResultItem {
	return SearchResultItem{
		Suggestion: suggestion,
	}
}

type LegacySearchResultItem struct {
	*gojenkins.LegacySuggestion
}

func (d LegacySearchResultItem) FilterValue() string { return d.Name }
func (d LegacySearchResultItem) Title() string       { return d.Name }
func (d LegacySearchResultItem) Description() string {
	return fmt.Sprintf("Items [%s]", d.URL)
}

type SearchResultModel struct {
	wWidth    int
	wHeight   int
	list      list.Model
	itemSize  int
	keyMap    SearchResultKeyMap
	spinner   spinner.Model
	isLoading bool
	ctx       *context.ProgramContext
}

func NewSearcResultModel(ctx *context.ProgramContext, width, height int, keymapConf context.GlobalsearchDialogKeyMapConfig) *SearchResultModel {
	spin := spinner.New()
	spin.Spinner = spinner.Dot
	spin.Style = lipgloss.NewStyle().
		Foreground(styles.GetTheme().Primary)

	m := &SearchResultModel{
		wWidth:  width,
		wHeight: height,
		keyMap:  UserSearchResultKeyMap(keymapConf),
		list:    buildListModel(width, height),
		spinner: spin,
		ctx:     ctx,
	}
	// Drive the list's built-in filter with the configured key so the filter key
	// ("/" by default) starts filtering the results.
	m.list.KeyMap.Filter = m.keyMap.Filter
	return m
}

func (s *SearchResultModel) Init() tea.Cmd {
	return nil
}

func (s *SearchResultModel) Update(msg tea.Msg) (*SearchResultModel, tea.Cmd) {
	switch msg := msg.(type) {
	case FetchGlobalSearchResultsMsg:
		return s, s.handleFetchResults()
	case UpdateListItemMsg:
		return s, s.handleListItems(msg)
	case tea.WindowSizeMsg:
		return s, s.handleWindowSize(msg)
	case tea.KeyMsg:
		return s, s.handleKeyPress(msg)
	case spinner.TickMsg:
		return s, s.handleSpinnerTick(msg)
	default:
		return s, s.updateList(msg)
	}
}

func (s *SearchResultModel) View() string {
	t := styles.GetTheme()
	if len(s.list.Items()) > 0 {
		return s.list.View()
	}

	base := t.S().Base
	msg := base.Foreground(t.Border).Render("No results found")
	if s.isLoading {
		msg = lipgloss.JoinHorizontal(
			lipgloss.Left,
			s.spinner.View(),
			"",
			"Searching...",
		)
	}

	// Paint both the centered text and the surrounding whitespace fill with the
	// dialog background so the empty/loading state doesn't leak the terminal's
	// default background.
	return lipgloss.Place(
		s.wWidth,
		s.wHeight,
		lipgloss.Center,
		lipgloss.Center,
		msg,
		lipgloss.WithWhitespaceStyle(base),
	)
}

func (s *SearchResultModel) handleKeyPressMsg(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, s.keyMap.Filter) {
		// Hand the keypress to the list so it enters filter mode.
		var cmd tea.Cmd
		s.list, cmd = s.list.Update(msg)
		return cmd
	}
	if key.Matches(msg, s.keyMap.Down) {
		s.list.CursorDown()
	}
	if key.Matches(msg, s.keyMap.Up) {
		s.list.CursorUp()
	}

	if key.Matches(msg, s.keyMap.PrevPage) {
		s.list.PrevPage()
	}

	if key.Matches(msg, s.keyMap.NextPage) {
		s.list.NextPage()
	}

	if key.Matches(msg, s.keyMap.Select) {
		switch item := s.list.SelectedItem().(type) {
		case SearchResultItem:
			log.Debug("SearchResultModel item selected", "item", item)
			return util.CmdHandler(s.resultSelectedMsg(item.Name, item.URL, jobref.KindFromClass(item.Type)))
		case LegacySearchResultItem:
			log.Debug("LegacySearchResultModel item selected", "item", item)
			return func() tea.Msg {
				ctx, cancel := s.ctx.RequestContext()
				defer cancel()
				resolved, err := item.Resolve(ctx, s.ctx.Jenkins)
				if err != nil {
					return util.ErrorMsg{Title: "Could not resolve", Error: err}
				}
				// The legacy HTML endpoint carries no class, so the target
				// stays unknown and the router classifies it.
				return s.resultSelectedMsg(resolved.Name, resolved.URL, jobref.KindUnknown)
			}
		}
	}
	return nil
}

// resultSelectedMsg turns a picked suggestion into a navigation target.
// Unsupported destinations are rejected later by HandleJobTarget so every
// entry point shares the same warning.
func (s *SearchResultModel) resultSelectedMsg(name, resultURL string, kind jobref.Kind) tea.Msg {
	return SearchResultItemSelectedMsg{Target: jobref.Target{
		URL:  jobref.ToAbsolute(s.ctx.Jenkins.Server, resultURL),
		Name: name,
		Kind: kind,
	}}
}

// IsFiltering reports whether the results list is actively capturing a filter
// query, so the parent dialog can hand it every keystroke instead of consuming them
// with its own focus/quit bindings.
func (s *SearchResultModel) IsFiltering() bool {
	return s.list.FilterState() == list.Filtering
}

func (s *SearchResultModel) handleFetchResults() tea.Cmd {
	s.isLoading = true
	return s.spinner.Tick
}

func (s *SearchResultModel) handleListItems(msg UpdateListItemMsg) tea.Cmd {
	log.Error("items %s", msg.Items)
	s.list.SetItems(msg.Items)
	s.isLoading = false
	return nil
}

func (s *SearchResultModel) handleWindowSize(msg tea.WindowSizeMsg) tea.Cmd {
	s.wWidth = msg.Width
	s.wHeight = msg.Height
	s.list.SetWidth(s.wWidth)
	s.list.SetHeight(s.wHeight)
	s.list.SetDelegate(SearchDelegate{wWidth: s.wWidth})
	return nil
}

func (s *SearchResultModel) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	var cmd tea.Cmd
	switch s.list.FilterState() {
	case list.Filtering:
		// Typing a filter query: the list owns every key (input, enter to apply,
		// esc to cancel).
		s.list, cmd = s.list.Update(msg)
		return cmd
	case list.FilterApplied:
		// Let the list clear an applied filter; other keys fall through to
		// navigation/select over the filtered items.
		if key.Matches(msg, s.list.KeyMap.ClearFilter) {
			s.list, cmd = s.list.Update(msg)
			return cmd
		}
	}
	return s.handleKeyPressMsg(msg)
}

func (s *SearchResultModel) handleSpinnerTick(msg spinner.TickMsg) tea.Cmd {
	if !s.isLoading {
		return s.updateList(msg)
	}
	var cmd tea.Cmd
	s.spinner, cmd = s.spinner.Update(msg)
	return cmd
}

func (s *SearchResultModel) updateList(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	s.list, cmd = s.list.Update(msg)
	return cmd
}

func buildListModel(width, height int) list.Model {
	delegate := SearchDelegate{
		wWidth: width,
	}
	l := list.New([]list.Item{}, delegate, width, height)
	l.SetShowHelp(false)
	l.SetShowTitle(false)
	l.SetFilteringEnabled(true)
	l.FilterInput.Prompt = "Find: "
	l.DisableQuitKeybindings()
	return l
}
