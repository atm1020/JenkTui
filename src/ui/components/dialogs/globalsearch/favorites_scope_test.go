package globalsearch

import (
	"strings"
	"testing"

	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

func TestFavoritesScopeFiltersAcrossKindsAndPreservesQuery(t *testing.T) {
	ctx := &appcontext.ProgramContext{Config: &appcontext.Config{}}
	ctx.Config.KeyMapConfig.Dialog.Globalsearch.ToggleScope = appcontext.KeyBinding{"S"}
	ctx.Config.KeyMapConfig.Dialog.Globalsearch.Search = appcontext.KeyBinding{"enter"}
	ctx.Config.KeyMapConfig.Dialog.Globalsearch.Select = appcontext.KeyBinding{"enter"}
	m := NewFavoritesDialog(ctx, 100, 35)
	m.Update(favoritesLoadedMsg{owner: m, records: []store.FavoriteRecord{
		{Name: "Payments", URL: "http://jenkins/job/Payments/", Kind: "folder"},
		{Name: "Deploy", URL: "http://jenkins/job/Payments/job/Deploy/", Kind: "job"},
		{Name: "API", URL: "http://jenkins/job/Payments/job/API/", Kind: "multibranch"},
		{Name: "Nightly", URL: "http://jenkins/job/Platform/job/Nightly/", Kind: "job"},
	}})
	highlight := strings.Split(lipgloss.NewStyle().Background(styles.GetTheme().SelectedBackground).Render("row"), "row")[0]
	if highlight == "" || strings.Contains(m.searchResultModel.View(), highlight) {
		t.Fatal("the unfocused list should not highlight its first result")
	}
	m.Update(tea.PasteMsg{Content: "payments"})
	if got := len(m.searchResultModel.list.Items()); got != 3 {
		t.Fatalf("payments returned %d items, want three kinds", got)
	}
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd != nil || m.focusedView != SearchRresult || !strings.Contains(m.searchResultModel.View(), highlight) {
		t.Fatal("first Enter should focus and highlight results without opening anything")
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := cmd().(SearchResultItemSelectedMsg).Target.Kind; got != jobref.KindFolder {
		t.Fatalf("selected kind = %v, want folder", got)
	}
	m.focusToSearchInput()
	if strings.Contains(m.searchResultModel.View(), highlight) {
		t.Fatal("returning to input should hide the highlight again")
	}
	m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	if !m.favoritesScope || m.searchInputModel.textInput.Value() != "paymentss" {
		t.Fatal("lowercase s should enter text without switching scopes")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	if m.favoritesScope {
		t.Fatal("toggle did not switch to Jenkins")
	}
	m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	if !m.favoritesScope || m.searchInputModel.textInput.Value() != "payments" || len(m.searchResultModel.list.Items()) != 3 {
		t.Fatal("toggle did not restore favorite results with the same query")
	}
}

func TestScopedSearchIgnoresStaleJenkinsResults(t *testing.T) {
	m := NewGlobalSearchDialogCmp(&appcontext.ProgramContext{Config: &appcontext.Config{}}, 100, 35)
	request := m.request
	m.toggleScope()
	m.toggleScope()
	m.Update(searchFetchedMsg{owner: m, request: request, items: []list.Item{favoriteItem{}}})
	m.Update(searchFetchedMsg{owner: &GlobalSearchDialogCmp{}, request: m.request, items: []list.Item{favoriteItem{}}})
	if len(m.searchResultModel.list.Items()) != 0 {
		t.Fatal("stale Jenkins results changed current scope")
	}
}
