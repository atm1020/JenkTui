package build

import (
	stdctx "context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/animation"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

const fixtureJobPath = "/job/team/job/demo"

var fixtureLoadPaths = []string{
	fixtureJobPath + "/api/json",
	fixtureJobPath + "/42/wfapi/describe/api/json",
	// The pinned gojenkins GetBuild appends a slash to Job.Raw.URL.
	fixtureJobPath + "//42/api/json",
	fixtureJobPath + "/42/execution/node/7/wfapi/describe/api/json",
}

func serveBuildLoadFixture(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case fixtureLoadPaths[0]:
		_, _ = fmt.Fprintf(w, `{"name":"demo","url":"http://%s%s/"}`, r.Host, fixtureJobPath)
	case fixtureLoadPaths[1]:
		// Deliberately omit wfapi self links: API paths must still be relative
		// to the client, not the absolute URL used for navigation identity.
		_, _ = fmt.Fprint(w, `{"id":"42","name":"#42","status":"SUCCESS","stages":[{"id":"7","name":"Build","status":"SUCCESS"}]}`)
	case fixtureLoadPaths[2]:
		_, _ = fmt.Fprint(w, `{"number":42,"result":"SUCCESS","actions":[{"causes":[{"userName":"Tester","shortDescription":"Started by Tester"}]}]}`)
	case fixtureLoadPaths[3]:
		_, _ = fmt.Fprint(w, `{"id":"7","name":"Build","status":"SUCCESS","execNode":"linux-1"}`)
	default:
		http.NotFound(w, r)
	}
}

type deadlineTransport struct {
	base http.RoundTripper
}

func (t deadlineTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if _, ok := r.Context().Deadline(); !ok {
		return nil, fmt.Errorf("build load request has no deadline")
	}
	return t.base.RoundTrip(r)
}

func buildLoadContext(server *httptest.Server) *appcontext.ProgramContext {
	cfg := config.GetDefaultConfig()
	cfg.JenkinsRequestTimeoutSeconds = 60
	client := server.Client()
	client.Transport = deadlineTransport{base: client.Transport}
	return &appcontext.ProgramContext{Config: cfg, Jenkins: gojenkins.CreateJenkins(client, server.URL)}
}

func TestBuildLoadResolvesOnlyMissingData(t *testing.T) {
	for _, source := range []string{"stored reference", "known run", "build list entry", "no stages"} {
		t.Run(source, func(t *testing.T) {
			var mu sync.Mutex
			var requests []string
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, r.URL.Path)
				mu.Unlock()
				serveBuildLoadFixture(w, r)
			}))
			defer server.Close()
			ctx := buildLoadContext(server)
			jobURL := server.URL + fixtureJobPath + "/"
			knownJob := &gojenkins.Job{
				Jenkins: ctx.Jenkins, Base: fixtureJobPath,
				Raw: &gojenkins.JobResponse{Name: "demo", URL: jobURL},
			}
			knownRun := gojenkins.PipelineRun{
				ID: "42", Name: "#42", Job: knownJob,
				Base: fixtureJobPath + "/42", Status: "SUCCESS",
				Stages: []gojenkins.PipelineNode{{ID: "7", Name: "Build", Status: "SUCCESS"}},
			}
			var model *Model
			var wantRequests []string
			switch source {
			case "stored reference":
				model = NewModelFromURL(ctx, jobURL, "42", 100, 30)
				wantRequests = fixtureLoadPaths
			case "known run":
				model = NewModel(ctx, knownRun, 100, 30)
				wantRequests = fixtureLoadPaths[2:]
			case "build list entry":
				run := knownRun
				run.Status, run.Stages = "", nil
				model = NewModel(ctx, run, 100, 30)
				wantRequests = fixtureLoadPaths[1:]
			case "no stages":
				run := knownRun
				run.Stages = nil
				model = NewModel(ctx, run, 100, 30)
				wantRequests = fixtureLoadPaths[2:3]
			}
			defer func() { _ = model.CleanUp() }()
			initialID := model.ID()
			if initialID != "build:"+jobURL+"42" {
				t.Fatalf("initial ID = %q", initialID)
			}
			if len(requests) != 0 || !strings.Contains(model.View(), "Loading") {
				t.Fatal("constructor must show loading without fetching")
			}
			result := model.fetchBuild()()
			fetched := result.(BuildFetchedMsg)
			if fetched.Error != nil {
				t.Fatal(fetched.Error)
			}
			mu.Lock()
			gotRequests := append([]string(nil), requests...)
			mu.Unlock()
			if !reflect.DeepEqual(gotRequests, wantRequests) {
				t.Fatalf("requests = %v, want %v", gotRequests, wantRequests)
			}
			_, _ = model.Update(result)
			if model.loading || model.build == nil || model.ID() != initialID {
				t.Fatal("load did not finish with stable navigation identity")
			}
			if model.startedBy != "Tester" || model.startedByDetails != "Started by Tester" {
				t.Fatalf("cause = %q / %q", model.startedBy, model.startedByDetails)
			}
			if strings.Contains(model.View(), "Loading") {
				t.Fatal("loaded build still displays the loader")
			}
			if source != "no stages" && model.stageDetails.pipelineNode.ExecNode != "linux-1" {
				t.Fatal("initial stage details are missing")
			}
			if knownRun.Stages[0].ExecNode != "" {
				t.Fatal("loading mutated the source Job page's run")
			}
			if cmd := model.Init(); cmd != nil {
				t.Fatal("returning to a loaded terminal build should not reload it")
			}
		})
	}
}

func TestBuildKeepsLoadingUntilInitialNodeArrives(t *testing.T) {
	nodeStarted, releaseNode := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == fixtureLoadPaths[3] {
			close(nodeStarted)
			select {
			case <-releaseNode:
			case <-r.Context().Done():
				return
			}
		}
		serveBuildLoadFixture(w, r)
	}))
	defer server.Close()
	model := NewModelFromURL(buildLoadContext(server), server.URL+fixtureJobPath+"/", "42", 100, 30)
	defer func() { _ = model.CleanUp() }()
	batch := model.Init()().(tea.BatchMsg)
	result := make(chan tea.Msg, 1)
	go func() { result <- batch[1]() }()
	select {
	case <-nodeStarted:
	case <-time.After(time.Second):
		t.Fatal("initial node request did not start")
	}
	if !model.loading || model.build != nil || !strings.Contains(model.View(), "Loading") {
		t.Fatal("build rendered progressively before node details arrived")
	}
	before := model.spinnerModel.View()
	frame := batch[0]().(animation.FrameMsg)
	if _, cmd := model.Update(frame); cmd == nil || before == model.spinnerModel.View() {
		t.Fatal("loading spinner did not animate")
	}
	close(releaseNode)
	select {
	case msg := <-result:
		_, _ = model.Update(msg)
	case <-time.After(time.Second):
		t.Fatal("initial load did not finish")
	}
	if model.loading || model.stageDetails.pipelineNode == nil {
		t.Fatal("build was not ready after node details arrived")
	}
	if _, cmd := model.Update(frame); cmd != nil || model.clock.Running() {
		t.Fatal("terminal build kept the loading animation running")
	}
}

func TestBuildLoadingSpinnerWorksForTerminalBuildAndResizes(t *testing.T) {
	model := NewModel(&appcontext.ProgramContext{Config: config.GetDefaultConfig()},
		gojenkins.PipelineRun{ID: "42", Status: "SUCCESS"}, 100, 30)
	before := model.spinnerModel.View()
	model.entered = true
	_ = model.ensureAnimation()
	_, cmd := model.Update(model.clock.Frame())
	if cmd == nil || before == model.spinnerModel.View() {
		t.Fatal("terminal run's loading spinner did not animate")
	}
	if _, cmd := model.Update(tea.KeyPressMsg{Code: 'l', Text: "l"}); cmd != nil {
		t.Fatal("build action was accepted while loading")
	}
	_, _ = model.Update(tea.WindowSizeMsg{Width: 12, Height: 5})
	view := ansi.Strip(model.View())
	if lipgloss.Width(view) > 12 || lipgloss.Height(view) != 5 {
		t.Fatalf("loader dimensions = %dx%d, want 12x5", lipgloss.Width(view), lipgloss.Height(view))
	}
}

func TestBuildCleanupCancelsEveryInitialFetchStep(t *testing.T) {
	for _, blockedPath := range fixtureLoadPaths {
		t.Run(blockedPath, func(t *testing.T) {
			started := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == blockedPath {
					close(started)
					<-r.Context().Done()
					return
				}
				serveBuildLoadFixture(w, r)
			}))
			defer server.Close()
			model := NewModelFromURL(buildLoadContext(server), server.URL+fixtureJobPath+"/", "42", 100, 30)
			defer func() { _ = model.CleanUp() }()
			cmd := model.fetchBuild()
			result := make(chan tea.Msg, 1)
			go func() { result <- cmd() }()
			select {
			case <-started:
			case <-time.After(time.Second):
				t.Fatal("request did not start")
			}
			_ = model.CleanUp()
			select {
			case msg := <-result:
				if !errors.Is(msg.(BuildFetchedMsg).Error, stdctx.Canceled) {
					t.Fatalf("result = %#v, want cancellation", msg)
				}
				if _, cmd := model.Update(msg); cmd != nil {
					t.Fatal("cancelled load emitted an error/navigation command")
				}
			case <-time.After(time.Second):
				t.Fatal("cleanup did not cancel the request")
			}
		})
	}
}

func TestBuildLoadFailuresAreScopedToCurrentAttempt(t *testing.T) {
	titles := []string{"Failed to get job", "Failed to get build #42", "Failed to fetch build #42", "Could not get 'Build' Build stage details"}
	for i, failedPath := range fixtureLoadPaths {
		t.Run(failedPath, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == failedPath {
					http.NotFound(w, r)
					return
				}
				serveBuildLoadFixture(w, r)
			}))
			defer server.Close()
			model := NewModelFromURL(buildLoadContext(server), server.URL+fixtureJobPath+"/", "42", 100, 30)
			defer func() { _ = model.CleanUp() }()
			_, cmd := model.Update(model.fetchBuild()())
			if cmd == nil {
				t.Fatal("failed load returned no error command")
			}
			failure, ok := cmd().(LoadFailedMsg)
			if !ok || failure.Error == nil || failure.ErrorTitle != titles[i] || !model.MatchesLoadFailure(failure) {
				t.Fatalf("failure = %#v, want scoped navigation error", failure)
			}
			_ = model.CleanUp()
			_ = model.Init()
			if model.MatchesLoadFailure(failure) {
				t.Fatal("failure from abandoned load matched a new attempt")
			}
		})
	}
}

func TestBuildRejectsLateResultsFromOtherPagesAndAttempts(t *testing.T) {
	ctx := &appcontext.ProgramContext{Config: config.GetDefaultConfig()}
	model := NewModel(ctx, gojenkins.PipelineRun{ID: "42"}, 100, 30)
	other := NewModel(ctx, gojenkins.PipelineRun{ID: "43"}, 100, 30)
	for _, msg := range []tea.Msg{
		BuildFetchedMsg{model: other},
		BuildFetchedMsg{model: other, Error: errors.New("old failure")},
		NodeDownloadedMsg{model: other, PipelineNode: gojenkins.PipelineNode{ID: "7"}},
		NodeDownloadedMsg{model: other, Error: errors.New("old node failure")},
	} {
		if _, cmd := model.Update(msg); cmd != nil {
			t.Fatalf("other page's %T emitted a command", msg)
		}
	}
	_ = model.CleanUp()
	for _, msg := range []tea.Msg{
		BuildFetchedMsg{model: model},
		NodeDownloadedMsg{model: model, PipelineNode: gojenkins.PipelineNode{ID: "7"}},
	} {
		if _, cmd := model.Update(msg); cmd != nil {
			t.Fatalf("abandoned attempt's %T emitted a command", msg)
		}
	}
	if !model.loading || model.build != nil || model.stageDetails.pipelineNode != nil {
		t.Fatal("stale response mutated the current page")
	}
}

func TestBuildCleanupCancelsStageRefresh(t *testing.T) {
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
	}))
	defer server.Close()
	ctx := buildLoadContext(server)
	model := NewModel(ctx, gojenkins.PipelineRun{
		ID: "42", Base: fixtureJobPath + "/42", Status: "IN_PROGRESS",
		Job: &gojenkins.Job{
			Jenkins: ctx.Jenkins, Base: fixtureJobPath,
			Raw: &gojenkins.JobResponse{URL: server.URL + fixtureJobPath + "/"},
		},
		Stages: []gojenkins.PipelineNode{{ID: "7", Name: "Build"}},
	}, 100, 30)
	defer func() { _ = model.CleanUp() }()
	model.loading = false
	cmd := model.getNode()
	result := make(chan tea.Msg, 1)
	go func() { result <- cmd() }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("stage refresh did not start")
	}
	_ = model.CleanUp()
	select {
	case msg := <-result:
		if !errors.Is(msg.(NodeDownloadedMsg).Error, stdctx.Canceled) {
			t.Fatalf("stage refresh result = %#v, want cancellation", msg)
		}
		if _, cmd := model.Update(msg); cmd != nil {
			t.Fatal("cancelled stage refresh emitted an error")
		}
	case <-time.After(time.Second):
		t.Fatal("cleanup did not cancel stage refresh")
	}
}

func TestBuildRetriesAfterCancelledLoad(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		serveBuildLoadFixture(w, r)
	}))
	defer server.Close()
	model := NewModelFromURL(buildLoadContext(server), server.URL+fixtureJobPath+"/", "42", 100, 30)
	defer func() { _ = model.CleanUp() }()
	oldCmd := model.fetchBuild()
	_ = model.CleanUp()
	batch := model.Init()().(tea.BatchMsg)
	if _, cmd := model.Update(oldCmd()); cmd != nil {
		t.Fatal("old request affected the retried page")
	}
	_, _ = model.Update(batch[1]())
	if model.loading || model.build == nil || requests.Load() != 4 {
		t.Fatalf("retry did not load the build: loading=%v, requests=%d", model.loading, requests.Load())
	}
}
