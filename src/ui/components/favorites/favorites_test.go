package favorites

import (
	"bytes"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/components/myruns"
	"github.com/atm1020/jenktui/src/ui/router"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func newTestModel(t *testing.T, runStore *store.Store) *Model {
	t.Helper()
	return &Model{
		ctx:     &context.ProgramContext{RunStore: runStore, Config: &context.Config{}},
		wWidth:  120,
		wHeight: 40,
	}
}

func TestSearch_UsesConfiguredKeyAndCapturesInput(t *testing.T) {
	runStore := store.New(t.TempDir())
	m := NewModel(&context.ProgramContext{
		RunStore: runStore,
		Config: &context.Config{KeyMapConfig: context.KeyMapConfig{
			Favorites: context.FavoritesKeyMapConfig{Search: context.KeyBinding{"s"}},
		}},
	}, 120, 40)
	t.Cleanup(func() { _ = runStore.Close() })
	m.favorites = []store.FavoriteRecord{{Name: "deploy", Kind: "job"}}
	m.buildList()

	m.handleKeyPress(tea.KeyPressMsg{Code: 's', Text: "s"})

	if m.list.FilterState() != list.Filtering {
		t.Fatalf("FilterState = %v, want Filtering", m.list.FilterState())
	}
	if !m.IsCapturingInput() {
		t.Fatal("favorites should capture input while search is active")
	}
}

func TestFavoriteItem_FilterValueIsName(t *testing.T) {
	item := favoriteItem{record: store.FavoriteRecord{Name: "my-job"}}
	if got := item.FilterValue(); got != "my-job" {
		t.Errorf("FilterValue() = %q, want my-job", got)
	}
}

func TestBuildList_FiltersByActiveTab(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.favorites = []store.FavoriteRecord{
		{URL: "http://a/job/x/", Name: "job-x", Kind: "job"},
		{URL: "http://a/job/y/", Name: "folder-y", Kind: "folder"},
	}
	m.activeTab = JobsTab
	m.buildList()

	if len(m.list.Items()) != 1 {
		t.Fatalf("len(items) = %d, want 1 (only job-kind favorite)", len(m.list.Items()))
	}
	if got := m.list.Items()[0].(favoriteItem).record.Name; got != "job-x" {
		t.Errorf("item = %q, want job-x", got)
	}
}

func TestSwitchTab_RebuildsListForFolders(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.favorites = []store.FavoriteRecord{
		{URL: "http://a/job/x/", Name: "job-x", Kind: "job"},
		{URL: "http://a/job/y/", Name: "folder-y", Kind: "folder"},
	}
	m.activeTab = JobsTab
	m.buildList()

	m.switchTab(FoldersTab)

	if m.activeTab != FoldersTab {
		t.Fatalf("activeTab = %v, want FoldersTab", m.activeTab)
	}
	if len(m.list.Items()) != 1 {
		t.Fatalf("len(items) = %d, want 1 (only folder-kind favorite)", len(m.list.Items()))
	}
	if got := m.list.Items()[0].(favoriteItem).record.Name; got != "folder-y" {
		t.Errorf("item = %q, want folder-y", got)
	}
}

func TestOpenRuns_FoldersTabReturnsWarningNoOp(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.favorites = []store.FavoriteRecord{{URL: "http://a/job/y/", Name: "folder-y", Kind: "folder"}}
	m.activeTab = FoldersTab
	m.buildList()

	cmd := m.openRuns()
	if cmd == nil {
		t.Fatal("want a footer notification command, got nil")
	}
	msg, ok := cmd().(util.FooterNotiMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want util.FooterNotiMsg", cmd())
	}
	if msg.Type != util.NotiWarning {
		t.Errorf("notification type = %v, want NotiWarning", msg.Type)
	}
}

func TestOpenRuns_MultibranchTabReturnsWarningNoOp(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.favorites = []store.FavoriteRecord{{URL: "http://a/job/z/", Name: "multibranch-z", Kind: "multibranch"}}
	m.activeTab = MultibranchTab
	m.buildList()

	cmd := m.openRuns()
	if cmd == nil {
		t.Fatal("want a footer notification command, got nil")
	}
	msg, ok := cmd().(util.FooterNotiMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want util.FooterNotiMsg", cmd())
	}
	if msg.Type != util.NotiWarning {
		t.Errorf("notification type = %v, want NotiWarning", msg.Type)
	}
}

func TestSwitchTab_RebuildsListForMultibranch(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.favorites = []store.FavoriteRecord{
		{URL: "http://a/job/x/", Name: "job-x", Kind: "job"},
		{URL: "http://a/job/z/", Name: "multibranch-z", Kind: "multibranch"},
	}
	m.activeTab = JobsTab
	m.buildList()

	m.switchTab(MultibranchTab)

	if m.activeTab != MultibranchTab {
		t.Fatalf("activeTab = %v, want MultibranchTab", m.activeTab)
	}
	if len(m.list.Items()) != 1 {
		t.Fatalf("len(items) = %d, want 1 (only multibranch-kind favorite)", len(m.list.Items()))
	}
	if got := m.list.Items()[0].(favoriteItem).record.Name; got != "multibranch-z" {
		t.Errorf("item = %q, want multibranch-z", got)
	}
}

func TestTabCounts_SplitsThreeWays(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.favorites = []store.FavoriteRecord{
		{Kind: "job"},
		{Kind: "job"},
		{Kind: "folder"},
		{Kind: "multibranch"},
	}

	jobs, folders, multibranch := m.tabCounts()
	if jobs != 2 || folders != 1 || multibranch != 1 {
		t.Errorf("tabCounts() = (%d, %d, %d), want (2, 1, 1)", jobs, folders, multibranch)
	}
}

func TestOpenRuns_JobsTabNavigatesToMyRuns(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.favorites = []store.FavoriteRecord{{URL: "http://a/job/x/", Name: "job-x", Kind: "job"}}
	m.activeTab = JobsTab
	m.buildList()

	cmd := m.openRuns()
	if cmd == nil {
		t.Fatal("want a navigate command, got nil")
	}
	navMsg, ok := cmd().(router.NavigateMsg)
	if !ok {
		t.Fatalf("cmd() = %T, want router.NavigateMsg", cmd())
	}
	if _, ok := navMsg.Model.(*myruns.Model); !ok {
		t.Fatalf("navMsg.Model = %T, want *myruns.Model", navMsg.Model)
	}
}

func TestFormatFavoriteName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		width int
		want  string
	}{
		{name: "pads short name to width", input: "abc", width: 10, want: "abc       "},
		{name: "name exactly at width is unchanged", input: "abcde", width: 5, want: "abcde"},
		{name: "truncates long name with ellipsis at width", input: "abcdefghij", width: 8, want: "abcde..."},
		{name: "zero width yields empty string", input: "anything", width: 0, want: ""},
		{name: "negative width yields empty string", input: "anything", width: -3, want: ""},
		{name: "width too narrow for ellipsis hard-cuts", input: "abcdef", width: 2, want: "ab"},
		{name: "width at ellipsis floor hard-cuts instead of all-dots", input: "abcdef", width: 3, want: "abc"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatFavoriteName(tt.input, tt.width)
			if got != tt.want {
				t.Errorf("formatFavoriteName(%q, %d) = %q, want %q", tt.input, tt.width, got, tt.want)
			}
			if len(got) != max(0, tt.width) {
				t.Errorf("formatFavoriteName(%q, %d) len = %d, want %d", tt.input, tt.width, len(got), max(0, tt.width))
			}
		})
	}
}

// TestFavoriteItemDelegate_RenderDoesNotWrap guards against a lipgloss
// quirk: its word-wrap miscounts width for strings containing embedded
// ANSI-styled substrings (the icon, the colored timestamp) and wraps one
// word early when content exactly fills the target Width(). The pure
// formatFavoriteName tests can't catch this since it only manifests once
// lipgloss actually renders the ANSI-styled line through Style.Width().
func TestFavoriteItemDelegate_RenderDoesNotWrap(t *testing.T) {
	names := []string{"x", "artifact-generator", "a-very-long-job-name-that-eventually-gets-truncated"}
	widths := []int{30, 40, 74, 75, 80, 120}

	for _, width := range widths {
		for _, name := range names {
			items := []list.Item{
				favoriteItem{record: store.FavoriteRecord{Name: name, Kind: "job", CreatedAt: 1782826560000}},
				favoriteItem{record: store.FavoriteRecord{Name: name, Kind: "job", CreatedAt: 1782826560000}},
			}
			l := list.New(items, favoriteItemDelegate{}, width, favoriteItemHeight*2)
			l.SetShowStatusBar(false)
			l.SetShowTitle(false)
			l.SetShowHelp(false)

			d := favoriteItemDelegate{}
			for _, idx := range []int{0, 1} { // 0: selected (matches default Index()==0), 1: unselected
				var buf bytes.Buffer
				d.Render(&buf, l, idx, items[idx])
				if got := lipgloss.Height(buf.String()); got != favoriteItemHeight {
					t.Errorf("width=%d name=%q idx=%d: rendered height = %d, want %d (row wrapped)\noutput=%q",
						width, name, idx, got, favoriteItemHeight, buf.String())
				}
			}
		}
	}
}

// TestRenderHeader_AlignsWithRowColumns guards the invariant that the
// header's NAME/UPDATED labels land at the same columns as the name and
// timestamp fields in the rows below it. This isn't derivable from the
// pure-function tests alone since renderHeader and Render independently
// call favoriteRowLayout/favoriteKindIcon and could drift apart.
func TestRenderHeader_AlignsWithRowColumns(t *testing.T) {
	tests := []struct {
		name string
		kind string
		tab  Tab
	}{
		{name: "job row aligns under job icon", kind: "job", tab: JobsTab},
		{name: "folder row aligns under wider folder icon", kind: "folder", tab: FoldersTab},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := NewModel(&context.ProgramContext{
				RunStore: store.New(t.TempDir()),
				Config:   &context.Config{},
			}, 80, 20)
			m.favorites = []store.FavoriteRecord{
				{URL: "http://a/job/x/", Name: "sample-name", Kind: tt.kind, CreatedAt: 1782826560000},
			}
			m.activeTab = tt.tab
			m.buildList()

			headerCol, rowCol := -1, -1
			headerEndCol, rowEndCol := -1, -1
			for _, l := range strings.Split(m.View(), "\n") {
				plain := ansi.Strip(l)
				if idx := strings.Index(plain, "NAME"); idx >= 0 {
					headerCol = lipgloss.Width(plain[:idx])
					updatedIdx := strings.Index(plain, "UPDATED")
					headerEndCol = lipgloss.Width(plain[:updatedIdx+len("UPDATED")])
				}
				if idx := strings.Index(plain, "sample-name"); idx >= 0 {
					rowCol = lipgloss.Width(plain[:idx])
					tsIdx := strings.Index(plain, "2026-")
					rowEndCol = lipgloss.Width(plain[:tsIdx+len("2026-06-30 15:36")])
				}
			}

			if headerCol == -1 || rowCol == -1 {
				t.Fatalf("could not locate NAME header (col=%d) or row name (col=%d) in rendered view", headerCol, rowCol)
			}
			if headerCol != rowCol {
				t.Errorf("NAME header at col %d, row name at col %d, want equal", headerCol, rowCol)
			}
			if headerEndCol != rowEndCol {
				t.Errorf("UPDATED header ends at col %d, timestamp ends at col %d, want equal", headerEndCol, rowEndCol)
			}
		})
	}
}

func TestDeleteFavorite_RemovesFromStore(t *testing.T) {
	runStore := store.New(t.TempDir())
	if err := runStore.AddFavorite(store.FavoriteRecord{URL: "http://a/job/x/", Name: "job-x", Kind: "job"}); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}
	m := newTestModel(t, runStore)

	cmd := m.deleteFavorite("http://a/job/x/")
	msg := cmd()
	deleted, ok := msg.(FavoriteDeletedMsg)
	if !ok || deleted.URL != "http://a/job/x/" {
		t.Fatalf("cmd() = %+v, want FavoriteDeletedMsg{URL: http://a/job/x/}", msg)
	}

	is, err := runStore.IsFavorite("http://a/job/x/")
	if err != nil || is {
		t.Fatalf("IsFavorite after delete = (%v, %v), want (false, nil)", is, err)
	}
}
