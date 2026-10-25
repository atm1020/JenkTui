package router

import (
	"testing"

	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

type mockModel struct {
	id          string
	initCalled  int
	cleanCalled int
}

func newMock(id string) *mockModel {
	return &mockModel{id: id}
}

func (m *mockModel) Init() tea.Cmd                               { m.initCalled++; return nil }
func (m *mockModel) Update(tea.Msg) (util.JenkTuiModel, tea.Cmd) { return m, nil }
func (m *mockModel) View() string                                { return m.id }
func (m *mockModel) ID() string                                  { return m.id }
func (m *mockModel) UpdateFooter() tea.Cmd                       { return nil }
func (m *mockModel) CleanUp() tea.Cmd                            { m.cleanCalled++; return nil }
func (m *mockModel) KeyMap() help.KeyMap                         { return m }
func (m *mockModel) ShortHelp() []key.Binding                    { return nil }
func (m *mockModel) FullHelp() [][]key.Binding                   { return nil }

func assertCurrentView(t *testing.T, r *Router, expectedID string) {
	t.Helper()
	view := r.CurrentView()
	if view == nil {
		t.Fatalf("CurrentView() returned nil, expected %q", expectedID)
	}
	if view.ID() != expectedID {
		t.Fatalf("CurrentView().ID() = %q, expected %q", view.ID(), expectedID)
	}
}

func TestNewRouter(t *testing.T) {
	r := NewRouter()
	if r == nil {
		t.Fatal("NewRouter returned nil")
	}
	if r.views == nil {
		t.Fatal("views map not initialized")
	}
	if r.currentView != "" {
		t.Fatalf("expected empty currentView, got %q", r.currentView)
	}
}

func TestRegisterView(t *testing.T) {
	r := NewRouter()
	m := newMock("view-a")

	r.RegisterView(m)

	if _, ok := r.views["view-a"]; !ok {
		t.Fatal("view not registered")
	}
}

func TestRegisterViewNil(t *testing.T) {
	r := NewRouter()
	r.RegisterView(nil) // should not panic
	if len(r.views) != 0 {
		t.Fatal("nil model should not be registered")
	}
}

func TestSetInitialView(t *testing.T) {
	r := NewRouter()
	m := newMock("home")

	r.SetInitialView(m)

	if r.currentView != "home" {
		t.Fatalf("expected currentView 'home', got %q", r.currentView)
	}
	if _, ok := r.views["home"]; !ok {
		t.Fatal("initial view not registered")
	}
	if r.backStack.len() != 0 {
		t.Fatal("backStack should be empty after SetInitialView")
	}
	assertCurrentView(t, r, "home")
}

func TestNavigateTo(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")

	r.SetInitialView(a)
	r.RegisterView(b)
	r.NavigateTo("b")

	if r.currentView != "b" {
		t.Fatalf("expected currentView 'b', got %q", r.currentView)
	}
	if r.backStack.len() != 1 || r.backStack.items()[0] != "a" {
		t.Fatalf("expected backStack [a], got %v", r.backStack.items())
	}
	assertCurrentView(t, r, "b")
}

func TestNavigateToUnregistered(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	r.SetInitialView(a)

	cmd := r.NavigateTo("nonexistent")

	if cmd != nil {
		t.Fatal("expected nil cmd for unregistered view")
	}
	if r.currentView != "a" {
		t.Fatalf("currentView should remain 'a', got %q", r.currentView)
	}
	assertCurrentView(t, r, "a")
}

func TestNavigateToSameView(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	r.SetInitialView(a)

	r.NavigateTo("a")

	if r.backStack.len() != 0 {
		t.Fatal("navigating to same view should not push to backStack")
	}
	assertCurrentView(t, r, "a")
}

func TestNavigateToClearsForwardStack(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")
	c := newMock("c")

	r.SetInitialView(a)
	r.RegisterView(b)
	r.RegisterView(c)
	r.NavigateTo("b")
	r.Back()
	// forwardStack should have "b"
	if r.forwardStack.len() != 1 {
		t.Fatalf("expected forwardStack length 1, got %d", r.forwardStack.len())
	}

	r.NavigateTo("c")

	if r.forwardStack.len() != 0 {
		t.Fatal("NavigateTo should clear forwardStack")
	}
	assertCurrentView(t, r, "c")
}

func TestBack(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")

	r.SetInitialView(a)
	r.RegisterView(b)
	r.NavigateTo("b")
	r.Back()

	if r.currentView != "a" {
		t.Fatalf("expected currentView 'a', got %q", r.currentView)
	}
	if r.backStack.len() != 0 {
		t.Fatalf("expected empty backStack, got %v", r.backStack.items())
	}
	if r.forwardStack.len() != 1 || r.forwardStack.items()[0] != "b" {
		t.Fatalf("expected forwardStack [b], got %v", r.forwardStack.items())
	}
	assertCurrentView(t, r, "a")
}

func TestBackDouble(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")
	c := newMock("c")

	r.SetInitialView(a)
	r.RegisterView(b)
	r.RegisterView(c)

	r.NavigateTo("b")
	r.NavigateTo("c")

	r.Back()
	r.Back()

	if r.currentView != "a" {
		t.Fatalf("expected currentView 'a', got %q", r.currentView)
	}
	if r.backStack.len() != 0 {
		t.Fatalf("expected empty backStack, got %v", r.backStack.items())
	}
	if r.forwardStack.len() != 2 {
		t.Fatalf("expected forwardStack size 2, got %d", r.forwardStack.len())
	}
	assertCurrentView(t, r, "a")
}

func TestBackEmptyStack(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	r.SetInitialView(a)

	cmd := r.Back()

	if cmd != nil {
		t.Fatal("Back with empty stack should return nil")
	}
	if r.currentView != "a" {
		t.Fatalf("currentView should remain 'a', got %q", r.currentView)
	}
	assertCurrentView(t, r, "a")
}

func TestForward(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")

	r.SetInitialView(a)
	r.RegisterView(b)
	r.NavigateTo("b")
	r.Back()
	r.Forward()

	if r.currentView != "b" {
		t.Fatalf("expected currentView 'b', got %q", r.currentView)
	}
	if r.forwardStack.len() != 0 {
		t.Fatalf("expected empty forwardStack, got %v", r.forwardStack.items())
	}
	if r.backStack.len() != 1 || r.backStack.items()[0] != "a" {
		t.Fatalf("expected backStack [a], got %v", r.backStack.items())
	}
	assertCurrentView(t, r, "b")
}

func TestForwardEmptyStack(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	r.SetInitialView(a)

	cmd := r.Forward()

	if cmd != nil {
		t.Fatal("Forward with empty stack should return nil")
	}
}

func TestBackForwardRoundTrip(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")
	c := newMock("c")

	r.SetInitialView(a)
	r.RegisterView(b)
	r.RegisterView(c)

	r.NavigateTo("b")
	r.NavigateTo("c")
	// backStack: [a, b], current: c

	r.Back()
	// backStack: [a], current: b, forward: [c]
	if r.currentView != "b" {
		t.Fatalf("expected 'b', got %q", r.currentView)
	}
	assertCurrentView(t, r, "b")

	r.Back()
	// backStack: [], current: a, forward: [c, b]
	if r.currentView != "a" {
		t.Fatalf("expected 'a', got %q", r.currentView)
	}
	assertCurrentView(t, r, "a")

	r.Forward()
	// backStack: [a], current: b, forward: [c]
	if r.currentView != "b" {
		t.Fatalf("expected 'b', got %q", r.currentView)
	}
	assertCurrentView(t, r, "b")

	r.Forward()
	// backStack: [a, b], current: c, forward: []
	if r.currentView != "c" {
		t.Fatalf("expected 'c', got %q", r.currentView)
	}
	assertCurrentView(t, r, "c")
}

func TestStackEviction(t *testing.T) {
	r := NewRouter()
	views := make([]*mockModel, 5)
	for i := range views {
		views[i] = newMock(string(rune('a' + i)))
		r.RegisterView(views[i])
	}
	r.currentView = "a"

	// Navigate through a -> b -> c -> d -> e
	// maxHistorySize is 3, so backStack should evict oldest
	r.NavigateTo("b")
	r.NavigateTo("c")
	r.NavigateTo("d")
	r.NavigateTo("e")

	// backStack should be [b, c, d] (a evicted)
	if r.backStack.len() != maxHistorySize {
		t.Fatalf("expected backStack length %d, got %d: %v", maxHistorySize, r.backStack.len(), r.backStack.items())
	}
	if r.backStack.items()[0] != "b" {
		t.Fatalf("expected first backStack entry 'b', got %q", r.backStack.items()[0])
	}

	// Evicted view "a" should have had CleanUp called and be deregistered
	if _, ok := r.views["a"]; ok {
		t.Fatal("evicted view 'a' should be deregistered")
	}
	if views[0].cleanCalled == 0 {
		t.Fatal("evicted view 'a' should have CleanUp called")
	}
	assertCurrentView(t, r, "e")
}

func TestCurrentView(t *testing.T) {
	r := NewRouter()
	m := newMock("test")
	r.SetInitialView(m)

	view := r.CurrentView()
	if view == nil {
		t.Fatal("CurrentView returned nil")
	}
	if view.ID() != "test" {
		t.Fatalf("expected ID 'test', got %q", view.ID())
	}
}

func TestCurrentViewNoView(t *testing.T) {
	r := NewRouter()
	view := r.CurrentView()
	if view != nil {
		t.Fatal("expected nil when no view set")
	}
}

func TestView(t *testing.T) {
	r := NewRouter()
	m := newMock("hello")
	r.SetInitialView(m)

	output := r.View()
	if output != "hello" {
		t.Fatalf("expected 'hello', got %q", output)
	}
}

func TestViewNoCurrentView(t *testing.T) {
	r := NewRouter()
	output := r.View()
	if output != "No view available" {
		t.Fatalf("expected 'No view available', got %q", output)
	}
}

func TestUpdateNavigateMsg(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")
	r.SetInitialView(a)

	r.Update(NavigateMsg{Model: b})

	if r.currentView != "b" {
		t.Fatalf("expected currentView 'b', got %q", r.currentView)
	}
	assertCurrentView(t, r, "b")
}

func TestUpdateBackMsg(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")
	r.SetInitialView(a)
	r.RegisterView(b)
	r.NavigateTo("b")

	r.Update(BackMsg{})

	if r.currentView != "a" {
		t.Fatalf("expected currentView 'a', got %q", r.currentView)
	}
	assertCurrentView(t, r, "a")
}

func TestUpdateForwardMsg(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")
	r.SetInitialView(a)
	r.RegisterView(b)
	r.NavigateTo("b")
	r.Back()

	r.Update(ForwardMsg{})

	if r.currentView != "b" {
		t.Fatalf("expected currentView 'b', got %q", r.currentView)
	}
	assertCurrentView(t, r, "b")
}

func TestUpdateDelegatesToCurrentView(t *testing.T) {
	r := NewRouter()
	m := newMock("a")
	r.SetInitialView(m)

	// Send an arbitrary message - it should be delegated to the current view's Update
	r.Update(tea.WindowSizeMsg{Width: 100, Height: 50})

	// The mock's Update just returns itself, so currentView should remain "a"
	if r.currentView != "a" {
		t.Fatalf("expected currentView 'a', got %q", r.currentView)
	}
	assertCurrentView(t, r, "a")
}

func TestCleanUpCalledOnNavigation(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")
	r.SetInitialView(a)
	r.RegisterView(b)

	r.NavigateTo("b")

	if a.cleanCalled == 0 {
		t.Fatal("CleanUp should be called on the view being navigated away from")
	}
	assertCurrentView(t, r, "b")
}

// Evicting a history entry must not deregister the navigation target.
func TestEvictionDeregistersNavigationTarget(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")
	c := newMock("c")

	r.SetInitialView(a)
	r.RegisterView(b)
	r.RegisterView(c)

	// a -> b -> a -> c
	r.NavigateTo("b") // backStack: [a]
	r.RegisterView(newMock("a"))
	r.NavigateTo("a") // backStack: [a, b]
	r.NavigateTo("c") // backStack: [a, b, a]

	// Now navigate back to "a" — pushToStack adds "c", evicts oldest "a"
	r.RegisterView(newMock("a"))
	r.NavigateTo("a")

	view := r.CurrentView()
	if view == nil {
		t.Fatal("BUG: CurrentView() returned nil - eviction deregistered the navigation target")
	}
	if view.ID() != "a" {
		t.Fatalf("expected currentView ID 'a', got %q", view.ID())
	}
}

func TestBackEvictsNavigationTarget(t *testing.T) {
	r := NewRouter()
	r.RegisterView(newMock("b"))
	r.RegisterView(newMock("x"))
	r.RegisterView(newMock("y"))
	r.RegisterView(newMock("z"))
	r.currentView = "z"
	r.backStack = newStackSetFrom("b")
	r.forwardStack = newStackSetFrom("b", "x", "y")

	// Evict "b" from forward history while navigating to it from back history.
	r.Back()

	assertCurrentView(t, r, "b")
}

func TestForwardEvictsNavigationTarget(t *testing.T) {
	r := NewRouter()
	r.RegisterView(newMock("a"))
	r.RegisterView(newMock("x"))
	r.RegisterView(newMock("y"))
	r.RegisterView(newMock("z"))
	r.currentView = "z"
	r.backStack = newStackSetFrom("a", "x", "y")
	r.forwardStack = newStackSetFrom("a")

	r.Forward()

	assertCurrentView(t, r, "a")
}

// TestEvictionCleanUpCalledOnNavigationTarget verifies that when pushToStack
// evicts a view matching the navigation target, CleanUp is still called on
// the newly registered instance before Init is called.
func TestEvictionCleanUpCalledOnNavigationTarget(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")
	c := newMock("c")

	r.SetInitialView(a)
	r.RegisterView(b)
	r.RegisterView(c)

	// a -> b -> a -> c
	r.NavigateTo("b") // back: [a]
	r.RegisterView(newMock("a"))
	r.NavigateTo("a") // back: [a, b]
	r.NavigateTo("c") // back: [a, b, a]

	// Register new "a" — this is the instance that will be in r.views["a"]
	newA := newMock("a")
	r.RegisterView(newA)

	// NavigateTo("a") evicts oldest "a" from backStack, calling CleanUp on
	// whatever r.views["a"] points to — which is newA (the fresh instance)
	r.NavigateTo("a")

	if newA.cleanCalled == 0 {
		t.Fatal("CleanUp should be called on the view during eviction, even when it's the navigation target")
	}
	if newA.initCalled == 0 {
		t.Fatal("Init should be called on the navigation target after eviction")
	}
	assertCurrentView(t, r, "a")
}

// Cross-stack eviction must preserve a view needed by a later Back call.
func TestBackPopsDanglingRefAfterCrossStackEviction(t *testing.T) {
	r := NewRouter()
	r.RegisterView(newMock("b"))
	r.RegisterView(newMock("x"))
	r.RegisterView(newMock("y"))
	r.RegisterView(newMock("z"))
	r.RegisterView(newMock("current"))

	// "b" appears in both backStack (bottom) and forwardStack (oldest).
	// forwardStack is full (maxHistorySize=3).
	r.currentView = "current"
	r.backStack = newStackSetFrom("b", "z")
	r.forwardStack = newStackSetFrom("b", "x", "y")

	// Evict "b" from forward history; it must remain available in back history.
	r.Back()
	assertCurrentView(t, r, "z")

	r.Back()

	assertCurrentView(t, r, "b")
}

// Cross-stack eviction must preserve a view needed by a later Forward call.
func TestForwardPopsDanglingRefAfterCrossStackEviction(t *testing.T) {
	r := NewRouter()
	r.RegisterView(newMock("b"))
	r.RegisterView(newMock("x"))
	r.RegisterView(newMock("y"))
	r.RegisterView(newMock("z"))
	r.RegisterView(newMock("current"))

	// "b" appears in both forwardStack (bottom) and backStack (oldest).
	// backStack is full (maxHistorySize=3).
	r.currentView = "current"
	r.forwardStack = newStackSetFrom("b", "z")
	r.backStack = newStackSetFrom("b", "x", "y")

	// Evict "b" from back history; it must remain available in forward history.
	r.Forward()
	assertCurrentView(t, r, "z")

	r.Forward()

	assertCurrentView(t, r, "b")
}

func TestInitCalledOnNavigation(t *testing.T) {
	r := NewRouter()
	a := newMock("a")
	b := newMock("b")
	r.SetInitialView(a)
	r.RegisterView(b)

	b.initCalled = 0
	r.NavigateTo("b")

	if b.initCalled == 0 {
		t.Fatal("Init should be called on the view being navigated to")
	}
	assertCurrentView(t, r, "b")
}
