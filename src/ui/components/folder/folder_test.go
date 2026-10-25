package folder

import (
	"bytes"
	cc "context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

func TestApplyFavoritesToItems_MarksMatchingRows(t *testing.T) {
	items := []list.Item{
		InnerJobsDelegate{Url: "http://a/job/x/", Name: "x"},
		InnerJobsDelegate{Url: "http://a/job/y/", Name: "y"},
	}
	l := list.New(items, InnerJobsDelegate{}, 80, 10)
	m := &Model{list: &l, favorites: map[string]bool{"http://a/job/x/": true}}

	m.applyFavoritesToItems()

	got := m.list.Items()
	if !got[0].(InnerJobsDelegate).IsFavorite {
		t.Error("item x should be marked favorite")
	}
	if got[1].(InnerJobsDelegate).IsFavorite {
		t.Error("item y should not be marked favorite")
	}
}

func TestRenderTitle_ShowsBookmarkForFavoriteCurrentContainer(t *testing.T) {
	theme := styles.GetTheme()
	for _, kind := range []jobref.Kind{jobref.KindFolder, jobref.KindMultiBranch} {
		t.Run(kind.FavoriteKind(), func(t *testing.T) {
			m := newTestModel(t, "http://jenkins", 15)
			m.target.Kind = kind
			m.loading = false
			m.handleFavoritesLoaded(FavoritesLoadedMsg{Favorites: []store.FavoriteRecord{{URL: m.target.URL}}})

			hasBookmark := func() bool {
				title := m.renderTitle(theme, theme.S().Base, 100)
				return strings.Contains(title, theme.S().StyledIcons.BookMark)
			}
			if !hasBookmark() {
				t.Error("favorite container title does not contain bookmark icon after favorites load")
			}

			m.handleFavoriteToggled(FavoriteToggledMsg{URL: m.target.URL, Favorited: false})
			if hasBookmark() {
				t.Error("container title still contains bookmark icon after removal")
			}

			m.handleFavoriteToggled(FavoriteToggledMsg{URL: m.target.URL, Favorited: true})
			if !hasBookmark() {
				t.Error("container title does not contain bookmark icon after live toggle")
			}
		})
	}
}

func TestToggleFavorite_TogglesCurrentContainerNotSelectedChild(t *testing.T) {
	tests := []struct {
		name string
		kind jobref.Kind
	}{
		{name: "folder", kind: jobref.KindFolder},
		{name: "multibranch", kind: jobref.KindMultiBranch},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			runStore := store.New(t.TempDir())
			const (
				containerURL = "http://a/job/current/"
				childURL     = "http://a/job/current/job/selected/"
			)
			items := []list.Item{InnerJobsDelegate{
				Url: childURL, Name: "selected", Class: "hudson.model.FreeStyleProject",
			}}
			l := list.New(items, InnerJobsDelegate{}, 80, 10)
			m := &Model{
				target: jobref.Target{
					URL: containerURL, Name: "current", Kind: tt.kind,
				},
				list: &l,
				ctx:  &context.ProgramContext{RunStore: runStore},
			}

			cmd := m.toggleFavorite()
			if cmd == nil {
				t.Fatal("toggleFavorite() returned nil")
			}
			msg := cmd()
			toggled, ok := msg.(FavoriteToggledMsg)
			if !ok || !toggled.Favorited || toggled.URL != containerURL {
				t.Errorf("first toggle = %+v, want current container %q favorited", msg, containerURL)
			}

			favorites, err := runStore.GetAllFavorites()
			if err != nil {
				t.Fatalf("GetAllFavorites() after add: %v", err)
			}
			if len(favorites) != 1 || favorites[0].URL != containerURL ||
				favorites[0].Name != "current" || favorites[0].Kind != tt.kind.FavoriteKind() {
				t.Errorf("favorites after add = %+v, want current %s container only", favorites, tt.name)
			}
			childFavorite, err := runStore.IsFavorite(childURL)
			if err != nil {
				t.Fatalf("IsFavorite(selected child): %v", err)
			}
			if childFavorite {
				t.Error("selected child was favorited; favorite must apply to the current container")
			}

			if ok {
				m.handleFavoriteToggled(toggled)
			}
			cmd = m.toggleFavorite()
			if cmd == nil {
				t.Fatal("second toggleFavorite() returned nil")
			}
			msg = cmd()
			toggled, ok = msg.(FavoriteToggledMsg)
			if !ok || toggled.Favorited || toggled.URL != containerURL {
				t.Errorf("second toggle = %+v, want current container %q unfavorited", msg, containerURL)
			}

			favorites, err = runStore.GetAllFavorites()
			if err != nil {
				t.Fatalf("GetAllFavorites() after remove: %v", err)
			}
			if len(favorites) != 0 {
				t.Errorf("favorites after remove = %+v, want none", favorites)
			}
		})
	}
}

// TestInnerJobsDelegate_HeightMatchesRenderedLineCount guards the list's
// pagination math against silently drifting out of sync with Render again:
// bubbles/list packs exactly as many rows into its configured height as
// Height() claims each one costs, so Height() must equal what Render()
// actually produces (3 physical lines per row: top border, content, bottom
// border — see Render's full Border() wrap), not the number of logical
// fields on the row.
func TestInnerJobsDelegate_HeightMatchesRenderedLineCount(t *testing.T) {
	d := InnerJobsDelegate{Name: "branch", Class: "org.jenkinsci.plugins.workflow.job.WorkflowJob", Widths: computeColumnWidths(80)}
	items := []list.Item{d}
	l := list.New(items, d, 80, 10)

	var buf bytes.Buffer
	d.Render(&buf, l, 0, d)

	gotLines := strings.Count(buf.String(), "\n") + 1
	if gotLines != d.Height() {
		t.Errorf("Render() produced %d lines, but Height() = %d — list pagination will overflow its allotted space", gotLines, d.Height())
	}
}

// TestHandleWindowSizeMsg_ListHeightAccountsForBorderedRows reproduces the
// reported overflow: the list's height budget was computed as
// len(items) + 2 + 4 capped at wHeight/4, as if every row were 1 line, but
// each row actually renders 3 physical lines (see the Height test above).
// list.Model packs exactly as many items as its configured height claims it
// can hold, so an under-counted height doesn't just look wrong — the block
// list.View() returns is taller than the pane, and the trailing rows spill
// past the visible viewport with no way to scroll to them.
func TestHandleWindowSizeMsg_ListHeightAccountsForBorderedRows(t *testing.T) {
	items := make([]list.Item, 22)
	for i := range items {
		items[i] = InnerJobsDelegate{Name: "branch"}
	}
	l := list.New(items, InnerJobsDelegate{}, 80, 10)
	m := &Model{list: &l, wWidth: 120, wHeight: 60}

	m.handleWindowSizeMsg(tea.WindowSizeMsg{Width: 120, Height: 60})

	rowHeight := InnerJobsDelegate{}.Height()
	minWanted := min(len(items)*rowHeight+6, 60-8)
	if got := m.list.Height(); got < minWanted {
		t.Errorf("list.Height() = %d, want at least %d (22 bordered rows at %d lines each need more than 1/4 of a 60-line window)", got, minWanted, rowHeight)
	}
}

// --- container loading -------------------------------------------------

const testMultiBranchClass = "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject"

// newTestModel builds a container page pointed at srvURL, the way the router
// would after a known MultiBranch target was selected.
func newTestModel(t *testing.T, srvURL string, timeoutSeconds int) *Model {
	t.Helper()
	api := gojenkins.CreateJenkins(nil, srvURL)
	cfg := config.GetDefaultConfig()
	cfg.JenkinsRequestTimeoutSeconds = timeoutSeconds
	ctx := &context.ProgramContext{Jenkins: api, Config: cfg, RunStore: store.New(t.TempDir())}
	target := jobref.Target{URL: srvURL + "/job/mb/", Name: "mb", Kind: jobref.KindMultiBranch}
	return NewModel(ctx, 120, 40, target, api.JobRef("mb"))
}

// runChildRequest drains the Init batch and returns the ChildrenLoadedMsg the
// child request produced, exercising the real command seam rather than
// calling the loader directly.
func runChildRequest(t *testing.T, m *Model) ChildrenLoadedMsg {
	t.Helper()
	cmd := m.loadChildren()
	msg, ok := cmd().(ChildrenLoadedMsg)
	if !ok {
		t.Fatalf("child request produced %T, want ChildrenLoadedMsg", msg)
	}
	return msg
}

// TestModel_OpensImmediatelyWithKnownTitleAndLoader is the visible payoff of
// carrying a typed target: the page paints its own name and a loader before
// Jenkins has said anything, instead of showing an empty or nameless folder.
func TestModel_OpensImmediatelyWithKnownTitleAndLoader(t *testing.T) {
	m := newTestModel(t, "http://jenkins", 15)
	m.startLoadingChildren()
	defer func() { _ = m.CleanUp() }()

	if !m.loading {
		t.Fatal("a freshly opened container should be loading")
	}
	view := m.View()
	if !strings.Contains(view, "mb") {
		t.Errorf("view should show the known container name before loading finishes:\n%s", view)
	}
	if strings.Contains(view, "0 items") {
		t.Errorf("a loading container must not claim it has 0 items:\n%s", view)
	}
}

// TestModel_LoadsChildrenWithOneRequest covers the whole loading-to-loaded
// path: one focused request, then the authoritative list in Jenkins' order.
func TestModel_LoadsChildrenWithOneRequest(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jobs":[
			{"_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","name":"main","url":"http://jenkins/job/mb/job/main/","color":"blue","buildable":true},
			{"_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","name":"merge-queue%2FPR-48","url":"http://jenkins/job/mb/job/merge-queue%2FPR-48/","color":"notbuilt","buildable":false},
			{"_class":"com.cloudbees.hudson.plugins.folder.Folder","name":"nested","url":"http://jenkins/job/mb/job/nested/"}
		]}`)
	}))
	defer srv.Close()

	m := newTestModel(t, srv.URL, 15)
	m.startLoadingChildren()
	if cmd := m.handleChildrenLoaded(runChildRequest(t, m)); cmd != nil {
		t.Fatalf("successful load should not emit a message, got %v", cmd())
	}

	if requests != 1 {
		t.Errorf("loading children took %d requests, want 1", requests)
	}
	if m.loading {
		t.Error("page should be loaded once children arrive")
	}

	items := m.list.Items()
	if len(items) != 3 {
		t.Fatalf("got %d rows, want 3", len(items))
	}
	wantNames := []string{"main", "merge-queue/PR-48", "nested"}
	for i, want := range wantNames {
		got := items[i].(InnerJobsDelegate).Name
		if got != want {
			t.Errorf("row %d = %q, want %q (Jenkins response order, names decoded)", i, got, want)
		}
	}
	if !strings.Contains(m.View(), "3 items") {
		t.Error("loaded view should show the item count badge")
	}

	// Rows carry the container they came from, so a WorkflowJob listed by a
	// MultiBranch Pipeline reads as the branch it actually is — while a
	// nested folder is unaffected.
	d := InnerJobsDelegate{}
	if got := d.getClassLabel(items[0].(InnerJobsDelegate)); got != "Branch" {
		t.Errorf("row 0 type = %q, want Branch (child of a MultiBranch page)", got)
	}
	if got := d.getClassLabel(items[2].(InnerJobsDelegate)); got != "Folder" {
		t.Errorf("row 2 type = %q, want Folder", got)
	}
}

// TestModel_EmptyContainerBecomesEmptyList guards against an empty folder
// spinning forever: no children is an answer, not a missing one.
func TestModel_EmptyContainerBecomesEmptyList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"jobs":[]}`)
	}))
	defer srv.Close()

	m := newTestModel(t, srv.URL, 15)
	m.startLoadingChildren()
	m.handleChildrenLoaded(runChildRequest(t, m))

	if m.loading {
		t.Error("an empty container must leave the loading state")
	}
	if got := len(m.list.Items()); got != 0 {
		t.Errorf("got %d rows, want 0", got)
	}
	if !strings.Contains(m.View(), "0 items") {
		t.Error("a loaded empty container should report 0 items")
	}
}

// TestSetChildren_RestoresSelectionByURL covers re-entering a container
// (Back re-runs Init, which reloads the list): the cursor should stay on the
// row the user left it on, not jump to the top.
func TestSetChildren_RestoresSelectionByURL(t *testing.T) {
	m := newTestModel(t, "http://jenkins", 15)
	m.setChildren([]gojenkins.DetailedInnerJob{
		{Name: "a", Url: "http://jenkins/job/mb/job/a/"},
		{Name: "b", Url: "http://jenkins/job/mb/job/b/"},
		{Name: "c", Url: "http://jenkins/job/mb/job/c/"},
	})
	m.list.Select(2)

	// Reload with "c" now in a different position.
	m.setChildren([]gojenkins.DetailedInnerJob{
		{Name: "c", Url: "http://jenkins/job/mb/job/c/"},
		{Name: "a", Url: "http://jenkins/job/mb/job/a/"},
	})

	if got := m.selectedURL(); got != "http://jenkins/job/mb/job/c/" {
		t.Errorf("selected URL after reload = %q, want the previously selected child", got)
	}

	// A selection that no longer exists simply doesn't survive.
	m.list.Select(1)
	m.setChildren([]gojenkins.DetailedInnerJob{{Name: "z", Url: "http://jenkins/job/mb/job/z/"}})
	if got := m.selectedURL(); got != "http://jenkins/job/mb/job/z/" {
		t.Errorf("selected URL = %q, want the only remaining row", got)
	}
}

// --- navigation safety -------------------------------------------------

// TestLoadChildren_RespectsRequestTimeout keeps a hung Jenkins from trapping
// the user on a permanent loader.
func TestLoadChildren_RespectsRequestTimeout(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	defer srv.Close()
	defer close(release)

	m := newTestModel(t, srv.URL, 1)
	m.startLoadingChildren()

	start := time.Now()
	msg := runChildRequest(t, m)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("request ran for %s, want it capped by the 1s configured timeout", elapsed)
	}
	if !errors.Is(msg.Error, cc.DeadlineExceeded) {
		t.Fatalf("error = %v, want a deadline-exceeded failure", msg.Error)
	}

	cmd := m.handleChildrenLoaded(msg)
	if m.loading {
		t.Error("a timed-out request must leave the loading state")
	}
	if cmd == nil {
		t.Fatal("a timeout is a genuine failure and should be reported")
	}
	if _, ok := cmd().(util.ErrorMsg); !ok {
		t.Errorf("timeout reported as %T, want jenktui's standard util.ErrorMsg", cmd())
	}
}

// TestCleanUp_CancelsInFlightRequest covers leaving a slow container: the
// request is aborted, and its eventual cancellation error is not shown to
// the user as a failure they caused.
func TestCleanUp_CancelsInFlightRequest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer srv.Close()

	m := newTestModel(t, srv.URL, 30)
	m.startLoadingChildren()
	cmd := m.loadChildren()

	done := make(chan tea.Msg, 1)
	go func() { done <- cmd() }()

	m.CleanUp()

	select {
	case raw := <-done:
		msg := raw.(ChildrenLoadedMsg)
		if !errors.Is(msg.Error, cc.Canceled) {
			t.Fatalf("error = %v, want context.Canceled after leaving the page", msg.Error)
		}
		if got := m.handleChildrenLoaded(msg); got != nil {
			t.Errorf("leaving a page emitted %v, want no error dialog", got())
		}
	case <-time.After(10 * time.Second):
		t.Fatal("CleanUp did not cancel the in-flight request")
	}
}

// TestHandleChildrenLoaded_IgnoresStaleResults protects a page from a reply
// that belongs to another page, or to a request it has already superseded.
func TestHandleChildrenLoaded_IgnoresStaleResults(t *testing.T) {
	m := newTestModel(t, "http://jenkins", 15)
	m.startLoadingChildren()
	m.loadChildren() // takes generation 1

	tests := []struct {
		name string
		msg  ChildrenLoadedMsg
	}{
		{
			name: "another_containers_result",
			msg: ChildrenLoadedMsg{
				ContainerURL: "http://jenkins/job/other/",
				Generation:   m.generation,
				Children:     []gojenkins.DetailedInnerJob{{Name: "intruder", Url: "http://jenkins/job/other/job/x/"}},
			},
		},
		{
			name: "superseded_generation",
			msg: ChildrenLoadedMsg{
				ContainerURL: m.target.URL,
				Generation:   m.generation - 1,
				Children:     []gojenkins.DetailedInnerJob{{Name: "stale", Url: "http://jenkins/job/mb/job/stale/"}},
			},
		},
		{
			name: "another_containers_failure",
			msg: ChildrenLoadedMsg{
				ContainerURL: "http://jenkins/job/other/",
				Generation:   m.generation,
				Error:        errors.New("boom"),
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if cmd := m.handleChildrenLoaded(tt.msg); cmd != nil {
				t.Errorf("stale result emitted %v, want it dropped", cmd())
			}
			if len(m.list.Items()) != 0 {
				t.Error("stale result wrote rows into the page")
			}
			if !m.loading {
				t.Error("stale result changed the page's loading state")
			}
		})
	}
}

// TestLoadingPageStaysNavigable: the app shell withholds its global letter
// shortcuts only while a view captures input, so a loading container must not
// claim to — otherwise Back would strand the user on the loader.
func TestLoadingPageStaysNavigable(t *testing.T) {
	m := newTestModel(t, "http://jenkins", 15)
	m.startLoadingChildren()
	defer func() { _ = m.CleanUp() }()

	if m.IsCapturingInput() {
		t.Error("a loading container must not capture input — Back/Forward have to keep working")
	}
	// List-level actions have nothing to act on yet and are withheld.
	if cmd := m.handleKeyMsg(tea.KeyPressMsg{Code: 'F', Text: "F"}); cmd != nil {
		t.Errorf("row action fired while loading: %v", cmd())
	}
}

// TestModel_FailedLoadIsNotMistakenForEmpty: after the error dialog is
// dismissed the page is still on screen, and a container that failed to load
// must not sit there claiming it has no children.
func TestModel_FailedLoadIsNotMistakenForEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer srv.Close()

	m := newTestModel(t, srv.URL, 15)
	m.startLoadingChildren()
	cmd := m.handleChildrenLoaded(runChildRequest(t, m))

	if cmd == nil {
		t.Fatal("a failed load should be reported")
	}
	errMsg, ok := cmd().(util.ErrorMsg)
	if !ok {
		t.Fatalf("failure reported as %T, want util.ErrorMsg", cmd())
	}
	// A bare "403" in a dialog tells the user nothing about what happened.
	if !strings.Contains(errMsg.Error.Error(), "403") || len(errMsg.Error.Error()) < 10 {
		t.Errorf("error text = %q, want a readable message carrying the status code", errMsg.Error)
	}

	view := m.View()
	if strings.Contains(view, "0 items") {
		t.Errorf("a failed container must not report an item count:\n%s", view)
	}
	if !strings.Contains(view, "Could not load children") {
		t.Errorf("a failed container should say so:\n%s", view)
	}

	// Re-entering the page clears the failure and tries again.
	m.startLoadingChildren()
	defer func() { _ = m.CleanUp() }()
	if m.loadFailed {
		t.Error("reopening the page should clear the previous failure")
	}
}

// TestContainerIcon_MatchesWhatThePageActuallyIs: the header icon was
// hardcoded to a folder, so opening a MultiBranch Pipeline announced itself
// as a folder — while a row pointing at that same project showed the branch
// icon two lines above.
func TestContainerIcon_MatchesWhatThePageActuallyIs(t *testing.T) {
	t.Parallel()
	theme := styles.GetTheme()

	tests := []struct {
		kind jobref.Kind
		want string
	}{
		{jobref.KindMultiBranch, theme.S().StyledIcons.Branch},
		{jobref.KindFolder, theme.S().StyledIcons.Folder},
		// Organization Folders classify as KindFolder; anything unclassified
		// falls back to the folder icon rather than inventing one.
		{jobref.KindUnknown, theme.S().StyledIcons.Folder},
	}
	for _, tt := range tests {
		if got := containerIcon(theme, tt.kind); got != tt.want {
			t.Errorf("containerIcon(%v) = %q, want %q", tt.kind, got, tt.want)
		}
	}
}
