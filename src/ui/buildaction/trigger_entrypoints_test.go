package buildaction_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/components/build"
	"github.com/atm1020/jenktui/src/ui/components/footer"
	"github.com/atm1020/jenktui/src/ui/components/job"
	"github.com/atm1020/jenktui/src/ui/components/myruns"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

// triggerHarness runs one page against a real poll manager rather than silently
// excluding its initial demand, so request counts are the ones a user pays.
type triggerHarness struct {
	t        *testing.T
	page     util.JenkTuiModel
	status   *footer.Model
	requests *atomic.Int32
	dialogs  []util.JenkTuiDialogModel
}

// walk runs a finite command tree the way the runtime would, withholding
// animation frames so no endless chain starts.
func (h *triggerHarness) walk(cmd tea.Cmd) {
	h.t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	switch msg.(type) {
	case nil, animation.FrameMsg, spinner.TickMsg:
		return
	}
	if value := reflect.ValueOf(msg); value.Kind() == reflect.Slice {
		for i := range value.Len() {
			h.walk(value.Index(i).Interface().(tea.Cmd))
		}
		return
	}
	switch msg := msg.(type) {
	case util.ErrorMsg:
		h.t.Fatalf("%s: %v", msg.Title, msg.Error)
	case util.FooterNotiMsg:
		if strings.Contains(msg.Message, "Loading build parameters") {
			h.t.Fatal("the parameter wait was pushed into the footer")
		}
		return
	case util.OpenDialogMsg:
		h.dialogs = append(h.dialogs, msg.Dialog)
		return
	}
	_, next := h.page.Update(msg)
	h.walk(next)
}

func (h *triggerHarness) view() string { return h.page.View() }

func newTriggerHarness(t *testing.T, entry string) *triggerHarness {
	t.Helper()
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "wfapi/runs"):
			_, _ = fmt.Fprint(w, `[{"id":"42","name":"#42","status":"SUCCESS","stages":[]}]`)
		case strings.Contains(r.URL.Path, "wfapi/describe"):
			_, _ = fmt.Fprint(w, `{"id":"42","name":"#42","status":"SUCCESS","stages":[]}`)
		case strings.Contains(r.URL.Path, "42/api/json"):
			_, _ = fmt.Fprint(w, `{"number":42,"result":"SUCCESS","actions":[]}`)
		default:
			_, _ = fmt.Fprintf(w, `{"name":"demo","url":"http://%s/job/demo/","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","builds":[{"number":42}],"property":[{"parameterDefinitions":[{"name":"BRANCH","type":"StringParameterDefinition"}]}]}`, r.Host)
		}
	}))
	t.Cleanup(server.Close)

	db := store.New(t.TempDir())
	t.Cleanup(func() { db.Close() })
	api := gojenkins.CreateJenkins(nil, server.URL)
	manager := poll.NewManager(api, db, poll.Config{PollInterval: time.Hour})
	manager.Start()
	t.Cleanup(manager.Stop)

	ctx := &context.ProgramContext{Config: config.GetDefaultConfig(), Jenkins: api, RunStore: db, PollManager: manager}
	status := footer.NewModel(ctx, "test")
	_, _ = status.Update(tea.WindowSizeMsg{Width: 160, Height: 45})

	h := &triggerHarness{t: t, status: status, requests: &requests}
	switch entry {
	case "job":
		h.page = job.NewModelFromUrl(ctx, 160, 45, server.URL+"/job/demo/")
	case "build":
		h.page = build.NewModelFromURL(ctx, server.URL+"/job/demo/", "42", 160, 45)
	case "myruns":
		page := myruns.NewModel(ctx, 160, 45)
		_, _ = page.Update(myruns.RunsLoadedMsg{Runs: []store.RunRecord{{
			ID:      server.URL + "/job/demo/#42",
			JobURL:  server.URL + "/job/demo/",
			JobName: "demo", BuildID: "42", Status: "SUCCESS",
		}}})
		h.page = page
	}
	t.Cleanup(func() { _ = h.page.CleanUp() })

	// Settle the page's own loading before measuring the trigger.
	if entry != "myruns" {
		h.walk(h.page.Init())
	}
	if entry == "job" {
		select {
		case delivery := <-manager.Results():
			for _, snapshot := range delivery.Batch.Snapshots {
				h.walk(h.page.(interface {
					ApplyPollSnapshot(poll.Snapshot) tea.Cmd
				}).ApplyPollSnapshot(snapshot))
			}
		case <-time.After(5 * time.Second):
			t.Fatal("initial poll did not complete")
		}
	}
	requests.Store(0)
	return h
}

// A page that already holds its job opens the trigger dialog on the keypress
// itself, with no request and nothing in between.
func TestResolvedPagesTriggerWithoutAnyWait(t *testing.T) {
	for _, entry := range []string{"job", "build"} {
		t.Run(entry, func(t *testing.T) {
			h := newTriggerHarness(t, entry)

			_, cmd := h.page.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
			h.walk(cmd)

			if len(h.dialogs) != 1 {
				t.Fatalf("opened %d dialogs, want exactly the trigger dialog", len(h.dialogs))
			}
			if view := h.dialogs[0].View(); !strings.Contains(view, "Are you sure") && !strings.Contains(view, "BRANCH") {
				t.Fatalf("dialog is not the trigger chooser:\n%s", view)
			}
			if got := h.requests.Load(); got != 0 {
				t.Fatalf("trigger made %d requests, want 0 — the job is already loaded", got)
			}
		})
	}
}

// My Runs knows a job by URL only, so it has to ask. The wait belongs on the
// row the user acted on: no dialog may open until the answer is in, so the
// trigger dialog is never seen at anything but its finished size.
func TestMyRunsShowsTheWaitOnItsRowThenOpensTheDialog(t *testing.T) {
	h := newTriggerHarness(t, "myruns")

	_, cmd := h.page.Update(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})

	waiting := h.view()
	if !strings.Contains(waiting, "Triggering") {
		t.Fatalf("the triggered row does not show its wait:\n%s", waiting)
	}
	if len(h.dialogs) != 0 {
		t.Fatal("a dialog opened before the parameters were known")
	}

	h.walk(cmd)

	if len(h.dialogs) != 1 {
		t.Fatalf("opened %d dialogs, want exactly the trigger dialog", len(h.dialogs))
	}
	if view := h.dialogs[0].View(); !strings.Contains(view, "Are you sure") && !strings.Contains(view, "BRANCH") {
		t.Fatalf("dialog is not the trigger chooser:\n%s", view)
	}
	if settled := h.view(); strings.Contains(settled, "Triggering") {
		t.Fatalf("the row kept animating after the parameters arrived:\n%s", settled)
	}
	if got := h.requests.Load(); got != 1 {
		t.Fatalf("trigger made %d requests, want 1 job resolve", got)
	}
	if strings.Contains(h.status.View(), "Loading build parameters") {
		t.Fatal("the parameter wait was pushed into the footer")
	}
}
