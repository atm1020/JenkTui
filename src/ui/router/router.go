package router

import (
	"fmt"
	"strings"

	"github.com/atm1020/jenktui/src/ui/util"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
)

const maxHistorySize = 3

// Router manages view switching and maintains navigation state
type Router struct {
	currentView  string
	backStack    stackSet
	forwardStack stackSet
	views        map[string]*util.JenkTuiModel
}

// NewRouter creates a new router with registered views
func NewRouter() *Router {
	return &Router{
		backStack:    newStackSet(),
		forwardStack: newStackSet(),
		views:        make(map[string]*util.JenkTuiModel),
	}
}

// RegisterView adds a view to the router using the model's ID
func (r *Router) RegisterView(model util.JenkTuiModel) {
	if model == nil {
		log.Error("RegisterView called with nil model")
		return
	}
	id := model.ID()
	log.Debug("registering view", "id", id)
	r.views[id] = &model
}

// SetInitialView sets the initial view without pushing to history
func (r *Router) SetInitialView(model util.JenkTuiModel) {
	r.RegisterView(model)
	r.currentView = model.ID()
}

// pushToStack pushes an id onto a stack, evicting the oldest entry if over max size
func (r *Router) pushToStack(stack *stackSet, id string, nexID string) {
	stack.push(id)
	if stack.len() > maxHistorySize {
		evicted := stack.evict()
		if evicted == r.currentView {
			log.Error("router bug: evicting the current view from history", "evicted", evicted)
		}
		if r.backStack.contains(evicted) {
			log.Error("router bug: evicting view that is still in backStack", "evicted", evicted)
		}
		if r.forwardStack.contains(evicted) {
			log.Error("router bug: evicting view that is still in forwardStack", "evicted", evicted)
		}
		log.Error("deregistering view (stack eviction)", "id", evicted)
		if ref, ok := r.views[evicted]; ok && ref != nil {
			(*ref).CleanUp()
		}
		if nexID != evicted && !r.backStack.contains(evicted) && !r.forwardStack.contains(evicted) {
			delete(r.views, evicted)
		}
	}
}

// logStackState logs the current router state for debugging
func (r *Router) logStackState(action string) {
	registeredViews := make([]string, 0, len(r.views))
	for k := range r.views {
		registeredViews = append(registeredViews, k)
	}
	log.Debug(fmt.Sprintf("router [%s]", action),
		"current", r.currentView,
		"backStack", fmt.Sprintf("[%s]", strings.Join(r.backStack.items(), ", ")),
		"forwardStack", fmt.Sprintf("[%s]", strings.Join(r.forwardStack.items(), ", ")),
		"registeredViews", fmt.Sprintf("[%s]", strings.Join(registeredViews, ", ")),
	)
}

// NavigateTo switches to a new view and adds current view to history
func (r *Router) NavigateTo(id string) tea.Cmd {
	r.logStackState(fmt.Sprintf("NavigateTo %s - before", id))

	if _, exists := r.views[id]; !exists {
		log.Warn("NavigateTo: view not registered", "id", id)
		return nil
	}

	var cmds []tea.Cmd

	if currentModel := r.CurrentView(); currentModel != nil {
		if cmd := currentModel.CleanUp(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if r.currentView != "" && r.currentView != id {
		log.Debug("pushStack", "newView", id, "currentView", r.currentView)
		r.pushToStack(&r.backStack, r.currentView, id)
	} else if r.currentView == id {
		log.Debug("NavigateTo: same view, skipping back stack push", "id", id)
		cmds = append(cmds, util.CmdHandler(util.FooterNotiMsg{
			Message: "Already on this page",
			Type:    util.NotiWarning,
		}))
	}

	r.forwardStack.clear()
	r.currentView = id

	if ref, ok := r.views[id]; ok && ref != nil {
		cmds = append(cmds, (*ref).UpdateFooter(), (*ref).Init())
	}
	r.logStackState(fmt.Sprintf("NavigateTo %s - after", id))
	return tea.Batch(cmds...)
}

// Back navigates to previous view in history
func (r *Router) Back() tea.Cmd {
	r.logStackState("Back - before")

	if r.backStack.len() == 0 {
		log.Debug("Back: backStack empty, no-op")
		return nil
	}

	var cmds []tea.Cmd

	if currentModel := r.CurrentView(); currentModel != nil {
		if cmd := currentModel.CleanUp(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	previousView := r.backStack.peek()

	r.pushToStack(&r.forwardStack, r.currentView, previousView)

	r.backStack.pop()
	r.currentView = previousView

	view := r.CurrentView()
	if view == nil {
		log.Error("Back: CurrentView returned nil after popping back stack",
			"previousView", previousView,
			"viewRegistered", r.views[previousView] != nil,
			"viewRefNil", r.views[previousView] == nil || *r.views[previousView] == nil,
		)
		r.logStackState("Back - nil view")
		return tea.Batch(cmds...)
	}
	cmds = append(cmds,
		view.Init(),
		view.UpdateFooter(),
	)
	r.logStackState("Back - after")
	return tea.Batch(cmds...)
}

// Forward navigates to the next view in forward history
func (r *Router) Forward() tea.Cmd {
	r.logStackState("Forward - before")

	if r.forwardStack.len() == 0 {
		log.Debug("Forward: forwardStack empty, no-op")
		return nil
	}

	var cmds []tea.Cmd

	if currentModel := r.CurrentView(); currentModel != nil {
		if cmd := currentModel.CleanUp(); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	nextView := r.forwardStack.peek()

	r.pushToStack(&r.backStack, r.currentView, nextView)

	r.forwardStack.pop()
	r.currentView = nextView

	view := r.CurrentView()
	if view == nil {
		log.Error("Forward: CurrentView returned nil after popping forward stack",
			"nextView", nextView,
			"viewRegistered", r.views[nextView] != nil,
			"viewRefNil", r.views[nextView] == nil || *r.views[nextView] == nil,
		)
		r.logStackState("Forward - nil view")
		return tea.Batch(cmds...)
	}
	cmds = append(cmds,
		view.Init(),
		view.UpdateFooter(),
	)
	r.logStackState("Forward - after")
	return tea.Batch(cmds...)
}

// CurrentView returns the currently active view model
func (r *Router) CurrentView() util.JenkTuiModel {
	if ref, ok := r.views[r.currentView]; ok && ref != nil {
		return *ref
	}

	return nil
}

// Update delegates update to current view and handles navigation messages
func (r *Router) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case NavigateMsg:
		r.RegisterView(msg.Model)
		return r.NavigateTo(msg.Model.ID())
	case BackMsg:
		return r.Back()
	case ForwardMsg:
		return r.Forward()
	}

	if ref, ok := r.views[r.currentView]; ok && ref != nil {
		if *ref == nil {
			log.Error("Update: view ref points to nil interface", "currentView", r.currentView)
			return nil
		}
		var updated util.JenkTuiModel
		updated, cmd = (*ref).Update(msg)
		if updated == nil {
			log.Error("Update: view.Update() returned nil model", "currentView", r.currentView, "msgType", fmt.Sprintf("%T", msg))
			return cmd
		}
		*ref = updated
		return cmd
	}

	log.Warn("Update: no view found for currentView", "currentView", r.currentView)
	return cmd
}

// View renders the current view
func (r *Router) View() string {
	if currentView := r.CurrentView(); currentView != nil {
		return currentView.View()
	}
	log.Warn("View: no current view to render", "currentView", r.currentView)
	return "No view available"
}

// CleanUp releases every registered view, including history entries that are
// not currently visible. Cleanup is idempotent at the view layer.
func (r *Router) CleanUp() {
	for id, ref := range r.views {
		if ref == nil || *ref == nil {
			continue
		}
		(*ref).CleanUp()
		delete(r.views, id)
	}
}
