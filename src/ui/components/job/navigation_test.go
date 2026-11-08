package job_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/ui"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/components/job"
	"github.com/atm1020/jenktui/src/ui/router"
	"github.com/atm1020/jenktui/src/ui/util"
)

// Exercise the real shell routing and Init commands, without starting the
// unrelated app-wide Jenkins initialization or continuous polling loops.
func navigationApp(t *testing.T, serverURL string) *ui.App {
	t.Helper()
	home := t.TempDir()
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA"} {
		t.Setenv(key, home)
	}
	configDir, err := config.GetConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := config.SaveServerConfig(configDir, &context.ServerConfig{
		ID: "test", Name: "Test", JenkinsURL: serverURL, Username: "fake", APIToken: "fake",
	}); err != nil {
		t.Fatal(err)
	}
	dataDir, err := config.GetDataDir()
	if err != nil {
		t.Fatal(err)
	}
	dirs, err := config.InitializeSubdirectories(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	app, err := ui.InitAppModel(dirs)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = app.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	t.Cleanup(func() { _, _ = app.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}) })
	return app
}

// Run finite command trees explicitly; animation delivery is deliberately
// withheld so this test cannot start an endless frame chain.
func deliverNavigation(t *testing.T, app *ui.App, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	msg := cmd()
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, child := range msg {
			deliverNavigation(t, app, child)
		}
	case animation.FrameMsg, spinner.TickMsg, nil:
		return
	case util.ErrorMsg:
		t.Fatalf("navigation failed: %s: %v", msg.Title, msg.Error)
	default:
		_, next := app.Update(msg)
		deliverNavigation(t, app, next)
	}
}

func openKnownPipeline(t *testing.T, app *ui.App, url string) (*job.Model, tea.Cmd) {
	t.Helper()
	_, route := app.Update(util.OpenJobTargetMsg{Target: jobref.Target{URL: url, Kind: jobref.KindPipeline}})
	_, open := app.Update(route())
	nav, ok := open().(router.NavigateMsg)
	if !ok {
		t.Fatal("known pipeline did not immediately emit navigation")
	}
	page, ok := nav.Model.(*job.Model)
	if !ok {
		t.Fatalf("destination = %T, want job page", nav.Model)
	}
	_, init := app.Update(nav)
	if view := app.View().Content; !strings.Contains(view, "Loading job...") {
		t.Fatalf("first paint is not the centered loading page:\n%s", view)
	}
	return page, init
}

func TestKnownPipelineNavigationCostsTwoRequests(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/job/team/job/pipe/api/json":
			_, _ = fmt.Fprintf(w, `{"name":"pipe","url":"http://%s/job/team/job/pipe/","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","builds":[{"number":42}]}`, r.Host)
		case "/job/team/job/pipe/wfapi/runs/api/json":
			_, _ = fmt.Fprint(w, `[{"id":"42","name":"#42","status":"SUCCESS","stages":[]}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	app := navigationApp(t, server.URL)
	page, init := openKnownPipeline(t, app, server.URL+"/job/team/job/pipe/")
	if len(paths) != 0 {
		t.Fatalf("requests before paint = %v, want none", paths)
	}
	t.Log("requests before loading paint: 0")
	// URL-only and loaded construction must agree even when one URL has a
	// trailing slash and the other does not; the router will not duplicate it.
	ctx := &context.ProgramContext{Config: config.GetDefaultConfig(), Jenkins: gojenkins.CreateJenkins(nil, server.URL)}
	loaded := job.NewModel(ctx, 100, 30, &gojenkins.Job{Raw: &gojenkins.JobResponse{URL: server.URL + "/job/team/job/pipe"}})
	if page.ID() != loaded.ID() {
		t.Fatalf("URL-only ID %q differs from loaded ID %q", page.ID(), loaded.ID())
	}
	deliverNavigation(t, app, init)
	want := "/job/team/job/pipe/api/json,/job/team/job/pipe/wfapi/runs/api/json"
	if got := strings.Join(paths, ","); got != want {
		t.Fatalf("navigation requests = %s, want %s", got, want)
	}
	if strings.Contains(page.View(), "Loading job...") {
		t.Fatal("page remained loading after fetching job and runs")
	}
	t.Logf("full navigation requests: %d (%s)", len(paths), strings.Join(paths, ", "))
}

func TestMisclassifiedPipelineRedirectReplacesHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"name":"team","url":"/job/team/","_class":"com.cloudbees.hudson.plugins.folder.Folder","jobs":[]}`)
	}))
	defer server.Close()
	app := navigationApp(t, server.URL)
	_, init := openKnownPipeline(t, app, server.URL+"/job/team/")
	deliverNavigation(t, app, init)
	if view := app.View().Content; strings.Contains(view, "Loading job...") || !strings.Contains(view, "0 items") {
		t.Fatalf("misclassified pipeline did not redirect to folder:\n%s", view)
	}
	_, _ = app.Update(router.BackMsg{})
	if view := app.View().Content; !strings.Contains(view, "Server") || strings.Contains(view, "Loading job...") || strings.Contains(view, "0 items") {
		t.Fatalf("Back did not return directly to home:\n%s", view)
	}
	_, _ = app.Update(router.ForwardMsg{})
	if view := app.View().Content; strings.Contains(view, "Loading job...") {
		t.Fatalf("Forward resurrected a throwaway job page:\n%s", view)
	}
}
