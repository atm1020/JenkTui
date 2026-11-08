package job_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/ui"
	"github.com/atm1020/jenktui/src/ui/buildaction"
	"github.com/atm1020/jenktui/src/ui/components/job"
	"github.com/atm1020/jenktui/src/ui/router"
	"github.com/atm1020/jenktui/src/ui/util"
)

func openUnknownTarget(t *testing.T, app *ui.App, target jobref.Target, fromMyRuns bool) tea.Cmd {
	t.Helper()
	msg := util.OpenJobTargetMsg{Target: target}
	if fromMyRuns {
		msg = buildaction.OpenJobByURL(target.URL, target.Name)().(util.OpenJobTargetMsg)
	}
	_, classify := app.Update(msg)
	_, open := app.Update(classify())
	nav, ok := open().(router.NavigateMsg)
	if !ok {
		t.Fatal("unknown target did not immediately navigate")
	}
	if _, ok := nav.Model.(*job.Model); !ok {
		t.Fatalf("destination = %T", nav.Model)
	}
	_, init := app.Update(nav)
	if view := app.View().Content; !strings.Contains(view, "Loading job...") {
		t.Fatalf("first paint is not loading:\n%s", view)
	}
	return init
}

// Like navigationApp's known-kind tests, this finite shell test excludes
// continuous polling. The latency measurements separately wire a real manager.
func TestUnknownPipelineNavigationFromMyRunsAndSearch(t *testing.T) {
	for _, fromMyRuns := range []bool{true, false} {
		t.Run(fmt.Sprintf("myruns=%v", fromMyRuns), func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				if strings.Contains(r.URL.Path, "wfapi/runs") {
					fmt.Fprint(w, `[]`)
					return
				}
				fmt.Fprintf(w, `{"name":"pipe","url":"http://%s/job/team/job/pipe/","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","builds":[]}`, r.Host)
			}))
			defer server.Close()
			app := navigationApp(t, server.URL)
			init := openUnknownTarget(t, app, jobref.Target{URL: server.URL + "/job/team/job/pipe/", Name: "pipe"}, fromMyRuns)
			if requests.Load() != 0 {
				t.Fatal("request before loading paint")
			}
			deliverNavigation(t, app, init)
			if requests.Load() != 2 {
				t.Fatalf("requests excluding polling = %d, want 2", requests.Load())
			}
			if view := app.View().Content; strings.Contains(view, "Loading job") || strings.Contains(view, "not supported") {
				t.Fatalf("pipeline did not load:\n%s", view)
			}
		})
	}
}

func TestUnknownContainerRedirectReplacesHistory(t *testing.T) {
	for _, class := range []string{
		"com.cloudbees.hudson.plugins.folder.Folder",
		"jenkins.branch.OrganizationFolder",
		"org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject",
	} {
		t.Run(class, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"name":"team","url":"/job/team/","_class":%q,"jobs":[]}`, class)
			}))
			defer server.Close()
			app := navigationApp(t, server.URL)
			init := openUnknownTarget(t, app, jobref.Target{URL: server.URL + "/job/team/"}, false)
			if requests.Load() != 0 {
				t.Fatal("request before loading paint")
			}
			deliverNavigation(t, app, init)
			if view := app.View().Content; !strings.Contains(view, "0 items") {
				t.Fatalf("did not redirect:\n%s", view)
			}
			for i := 0; i < 3; i++ {
				_, cmd := app.Update(router.BackMsg{})
				deliverNavigation(t, app, cmd)
				if view := app.View().Content; !strings.Contains(view, "Server") || strings.Contains(view, "0 items") || strings.Contains(view, "Loading job") {
					t.Fatalf("Back %d resurrected a throwaway page:\n%s", i, view)
				}
			}
			// Execute Forward's Init, not only its first frame: a throwaway loading
			// page must not reappear or re-classify on either trip through history.
			for i := 0; i < 2; i++ {
				before := requests.Load()
				_, cmd := app.Update(router.ForwardMsg{})
				deliverNavigation(t, app, cmd)
				if view := app.View().Content; !strings.Contains(view, "0 items") || strings.Contains(view, "Loading job") {
					t.Fatalf("Forward %d did not reach container directly:\n%s", i, view)
				}
				if delta := requests.Load() - before; delta != 1 {
					t.Fatalf("Forward fetched %d requests, want only folder children", delta)
				}
				_, cmd = app.Update(router.BackMsg{})
				deliverNavigation(t, app, cmd)
			}
		})
	}
}

func TestUnknownUnsupportedKindStaysInPage(t *testing.T) {
	for _, tt := range []struct{ name, caller, resolved, url, label string }{
		{"caller_name", "quick-health-check", "resolved", "/job/team/job/target/", "quick-health-check"},
		{"resolved_name", "  ", "target", "/job/team/job/target/", "target"},
		{"url_fallback", "", "", "/job/team/job/target/", jobref.ToDisplayName("/job/team/job/target/")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int64
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Content-Type", "application/json")
				fmt.Fprintf(w, `{"name":%q,"url":%q,"_class":"hudson.model.FreeStyleProject"}`, tt.resolved, tt.url)
			}))
			defer server.Close()
			app := navigationApp(t, server.URL)
			init := openUnknownTarget(t, app, jobref.Target{URL: server.URL + tt.url, Name: tt.caller}, true)
			if requests.Load() != 0 {
				t.Fatal("request before loading paint")
			}
			deliverNavigation(t, app, init)
			view := app.View().Content
			want := fmt.Sprintf("Navigation to %q is not supported", tt.label)
			if !strings.Contains(view, want) || !strings.Contains(view, "b: Back returns") || strings.Contains(view, "Loading job") {
				t.Fatalf("missing unsupported message and escape hint:\n%s", view)
			}
			if requests.Load() != 1 {
				t.Fatalf("unsupported page made %d requests, want only classification (no wfapi)", requests.Load())
			}
			// Job actions must be inert here, but global history keys still work.
			_, cmd := app.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
			deliverNavigation(t, app, cmd)
			_, cmd = app.Update(tea.KeyPressMsg{Code: 'b'})
			deliverNavigation(t, app, cmd)
			if view := app.View().Content; !strings.Contains(view, "Server") || strings.Contains(view, "not supported") {
				t.Fatalf("Back did not return home:\n%s", view)
			}
			_, cmd = app.Update(tea.KeyPressMsg{Code: ']'})
			deliverNavigation(t, app, cmd)
			if view := app.View().Content; !strings.Contains(view, want) {
				t.Fatalf("Forward did not return to unsupported page:\n%s", view)
			}
		})
	}
}
