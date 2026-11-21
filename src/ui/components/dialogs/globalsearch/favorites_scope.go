package globalsearch

import (
	"fmt"
	"strings"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/components/myruns"
	"github.com/atm1020/jenktui/src/ui/router"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
)

type favoriteKeys struct{ remove, runs key.Binding }

func newFavoriteKeys(conf context.FavoritesKeyMapConfig) favoriteKeys {
	return favoriteKeys{
		remove: key.NewBinding(key.WithKeys(conf.Delete...), key.WithHelp(conf.Delete.HelpText(), "remove favorite")),
		runs:   key.NewBinding(key.WithKeys(conf.OpenRuns...), key.WithHelp(conf.OpenRuns.HelpText(), "run history")),
	}
}

type favoriteItem struct {
	store.FavoriteRecord
	path   string
	parent string
	leaf   string
}

func (i favoriteItem) FilterValue() string { return i.Name + " " + i.path }

func (i favoriteItem) target() jobref.Target {
	return jobref.Target{URL: i.URL, Name: i.Name, Kind: jobref.KindFromFavoriteKind(i.Kind)}
}

type favoritesLoadedMsg struct {
	owner   *GlobalSearchDialogCmp
	records []store.FavoriteRecord
	err     error
}

type favoriteRemovedMsg struct {
	owner *GlobalSearchDialogCmp
	err   error
}

func (m *GlobalSearchDialogCmp) updateScopeInput() {
	if m.favoritesScope {
		m.searchInputModel.textInput.Placeholder = "Search Favorites..."
		m.refreshFavorites()
	} else {
		m.searchInputModel.textInput.Placeholder = "Global Search..."
		m.searchResultModel.emptyText = "Type a query and press Enter to search Jenkins"
	}
}

func (m *GlobalSearchDialogCmp) toggleScope() tea.Cmd {
	m.stopSearch()
	m.favoritesScope = !m.favoritesScope
	m.searchResultModel.setItems(nil)
	m.updateScopeInput()
	focus := m.focusToSearchInput()
	if m.favoritesScope {
		return focus
	}
	return tea.Batch(focus, m.startSearch())
}

func (m *GlobalSearchDialogCmp) loadFavorites() tea.Cmd {
	db := m.ctx.RunStore
	if db == nil {
		return nil
	}
	return func() tea.Msg {
		records, err := db.GetAllFavorites()
		return favoritesLoadedMsg{owner: m, records: records, err: err}
	}
}

func (m *GlobalSearchDialogCmp) handleFavoritesLoaded(msg favoritesLoadedMsg) tea.Cmd {
	if msg.owner != m {
		return nil
	}
	m.favoritesLoading = false
	m.favoritesError = msg.err
	m.favorites = msg.records
	if m.favoritesScope {
		m.refreshFavorites()
	}
	return nil
}

func (m *GlobalSearchDialogCmp) refreshFavorites() {
	query := strings.ToLower(strings.TrimSpace(m.searchInputModel.textInput.Value()))
	var items []list.Item
	for _, record := range m.favorites {
		leaf, parents := jobref.ToIDAndParents(record.URL)
		for i := range parents {
			parents[i] = jobref.DecodeName(parents[i])
		}
		item := favoriteItem{
			FavoriteRecord: record,
			path:           strings.Join(append(parents, jobref.DecodeName(leaf)), "/"),
			parent:         strings.Join(parents, breadcrumbSep),
			leaf:           leaf,
		}
		if strings.Contains(strings.ToLower(record.Name), query) || strings.Contains(strings.ToLower(item.path), query) {
			items = append(items, item)
		}
	}
	m.searchResultModel.setItems(items)
	switch {
	case m.favoritesLoading:
		m.searchResultModel.emptyText = "Loading favorites..."
	case m.favoritesError != nil:
		m.searchResultModel.emptyText = "Could not load favorites"
	case len(m.favorites) == 0:
		m.searchResultModel.emptyText = "No saved favorites"
	default:
		m.searchResultModel.emptyText = "No matching favorites"
	}
}

func (m *GlobalSearchDialogCmp) handleFavoriteAction(msg tea.KeyMsg) (tea.Cmd, bool) {
	remove := key.Matches(msg, m.favoriteKeys.remove)
	if !remove && !key.Matches(msg, m.favoriteKeys.runs) {
		return nil, false
	}
	item, ok := m.searchResultModel.list.SelectedItem().(favoriteItem)
	if !ok {
		return nil, true
	}
	if remove {
		db := m.ctx.RunStore
		if db == nil {
			return nil, true
		}
		return func() tea.Msg { return favoriteRemovedMsg{owner: m, err: db.RemoveFavorite(item.URL)} }, true
	}
	if item.target().Kind.IsContainer() {
		return util.CmdHandler(util.FooterNotiMsg{Message: "Containers have no run history", Type: util.NotiWarning}), true
	}
	return util.CmdHandler(router.NavigateMsg{Model: myruns.NewModelWithQuery(m.ctx, m.appWidth, m.appHeight, store.RunQuery{
		JobName: item.Name, SortBy: store.RunSortDate, Direction: store.SortDescending,
	})}), true
}

func (m *GlobalSearchDialogCmp) handleFavoriteRemoved(msg favoriteRemovedMsg) tea.Cmd {
	if msg.owner != m {
		return nil
	}
	if msg.err != nil {
		return util.CmdHandler(util.FooterNotiMsg{Message: fmt.Sprintf("Failed to remove favorite: %s", msg.err), Type: util.NotiError})
	}
	return tea.Batch(m.loadFavorites(), util.CmdHandler(util.FooterNotiMsg{Message: "Removed from favorites", Type: util.NotiSuccess}))
}
