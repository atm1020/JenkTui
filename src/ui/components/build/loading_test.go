package build

import (
	stdctx "context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
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
	fixtureJobPath + "/42/api/json",
	fixtureJobPath + "/42/execution/node/7/wfapi/describe/api/json",
}

func serveBuildLoadFixture(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case fixtureLoadPaths[0]:
		_, _ = fmt.Fprintf(w,
			`{"_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob","name":"demo","url":"http://%s%s/","color":"blue","healthReport":[{"description":"Build stability: No recent builds failed.","score":100}],"lastBuild":{"number":42},"nextBuildNumber":43,"property":[{"parameterDefinitions":[{"name":"BRANCH","type":"StringParameterDefinition","defaultParameterValue":{"name":"BRANCH","value":"main"}}]}]}`,
			r.Host, fixtureJobPath,
		)
	case fixtureLoadPaths[1]:
		// Deliberately omit wfapi self links: API paths must still be relative
		// to the client, not the absolute URL used for navigation identity.
		_, _ = fmt.Fprint(w, `{"id":"42","name":"#42","status":"SUCCESS","stages":[{"id":"7","name":"Build","status":"SUCCESS"}]}`)
	case fixtureLoadPaths[2]:
		_, _ = fmt.Fprint(w, `{"number":42,"actions":[{"causes":[{"userName":"Tester","shortDescription":"Started by Tester"}]}],"artifacts":[{"fileName":"report.txt","relativePath":"out/report.txt"}]}`)
	case fixtureLoadPaths[3]:
		_, _ = fmt.Fprint(w, `{"id":"7","name":"Build","status":"SUCCESS","execNode":"linux-1"}`)
	case fixtureJobPath + "/42/parameters/":
		_, _ = fmt.Fprint(w, `<div class="jenkins-form-item"><div class="jenkins-form-label">BRANCH</div><button class="copy-button" text="main"></button></div>`)
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
			type recordedRequest struct {
				path  string
				tree  string
				depth string
			}
			var requests []recordedRequest
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				mu.Lock()
				requests = append(requests, recordedRequest{
					path:  r.URL.Path,
					tree:  r.URL.Query().Get("tree"),
					depth: r.URL.Query().Get("depth"),
				})
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
			var wantPaths []string
			switch source {
			case "stored reference":
				model = NewModelFromURL(ctx, jobURL, "42", 100, 30)
				wantPaths = fixtureLoadPaths
			case "known run":
				model = NewModel(ctx, knownRun, 100, 30)
				wantPaths = fixtureLoadPaths[2:]
			case "build list entry":
				run := knownRun
				run.Status, run.Stages = "", nil
				model = NewModel(ctx, run, 100, 30)
				wantPaths = fixtureLoadPaths[1:]
			case "no stages":
				run := knownRun
				run.Stages = nil
				model = NewModel(ctx, run, 100, 30)
				wantPaths = fixtureLoadPaths[2:3]
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
			gotRequests := append([]recordedRequest(nil), requests...)
			mu.Unlock()

			gotPaths := make([]string, len(gotRequests))
			for i, req := range gotRequests {
				gotPaths[i] = req.path
				switch req.path {
				case fixtureLoadPaths[0]:
					if req.tree != jobDetailsTree {
						t.Fatalf("job tree = %q, want %q", req.tree, jobDetailsTree)
					}
					if strings.Contains(req.tree, "builds[") {
						t.Fatalf("job tree must not fetch builds list: %q", req.tree)
					}
				case fixtureLoadPaths[2]:
					if req.tree != buildDetailsTree {
						t.Fatalf("build tree = %q, want %q", req.tree, buildDetailsTree)
					}
					if req.depth != "" {
						t.Fatalf("build request unexpectedly passed depth=%q", req.depth)
					}
				}
			}
			sortedGot := slices.Clone(gotPaths)
			sortedWant := slices.Clone(wantPaths)
			slices.Sort(sortedGot)
			slices.Sort(sortedWant)
			if !reflect.DeepEqual(sortedGot, sortedWant) {
				t.Fatalf("requests = %v, want %v", gotPaths, wantPaths)
			}
			if source == "stored reference" || source == "build list entry" {
				if gotPaths[len(gotPaths)-1] != fixtureLoadPaths[3] {
					t.Fatalf("expected stage node fetch last after discovery, got order %v", gotPaths)
				}
			}

			_, _ = model.Update(result)
			if model.loading || model.build == nil || model.ID() != initialID {
				t.Fatal("load did not finish with stable navigation identity")
			}
			if model.build.Base != fixtureJobPath+"/42" {
				t.Fatalf("build.Base = %q, want %q", model.build.Base, fixtureJobPath+"/42")
			}
			if model.build.GetBuildNumber() != 42 {
				t.Fatalf("build number = %d, want 42", model.build.GetBuildNumber())
			}
			if artifacts := model.build.GetArtifacts(); len(artifacts) != 1 || artifacts[0].FileName != "report.txt" || artifacts[0].Path != fixtureJobPath+"/42/artifact/out/report.txt" {
				t.Fatalf("artifacts = %#v, want report.txt with clean path", artifacts)
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
			if source == "stored reference" {
				jobRaw := model.pipelineRun.Job.GetDetails()
				if jobRaw == nil || jobRaw.Class != "org.jenkinsci.plugins.workflow.job.WorkflowJob" || jobRaw.URL != jobURL {
					t.Fatalf("resolved job metadata = %#v, want pipeline class and URL", jobRaw)
				}
				if len(jobRaw.HealthReport) != 1 || jobRaw.HealthReport[0].Score != 100 {
					t.Fatalf("resolved job healthReport = %#v, want score 100", jobRaw.HealthReport)
				}
				if jobRaw.LastBuild.Number != 42 || jobRaw.NextBuildNumber != 43 {
					t.Fatalf("resolved job build baseline = last:%d next:%d, want 42/43", jobRaw.LastBuild.Number, jobRaw.NextBuildNumber)
				}
				if len(jobRaw.Property) != 1 || len(jobRaw.Property[0].ParameterDefinitions) != 1 || jobRaw.Property[0].ParameterDefinitions[0].Name != "BRANCH" {
					t.Fatalf("resolved job parameters = %#v, want BRANCH", jobRaw.Property)
				}
			}
			// Verify on-demand build parameters (p) work with the projected build handle.
			paramsMsg := model.fetchBuildParams()().(BuildParamsFetchedMsg)
			if paramsMsg.Error != nil || len(paramsMsg.Params) != 1 || paramsMsg.Params[0].Name != "BRANCH" || paramsMsg.Params[0].Value != "main" {
				t.Fatalf("build params = %#v, want BRANCH=main", paramsMsg)
			}
			if cmd := model.Init(); cmd != nil {
				t.Fatal("returning to a loaded terminal build should not reload it")
			}
		})
	}
}

// Exercise concurrent build+stage loading from a known Job page run with fixed
// latency, pinning both request count and 1-RTT parallelism.
func TestBuildLoadFromJobParallelizesBuildAndStageFetch(t *testing.T) {
	const latency = 150 * time.Millisecond
	var mu sync.Mutex
	type hit struct {
		path       string
		start, end time.Duration
	}
	var hits []hit
	var inFlight, peakInFlight atomic.Int32
	press := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		for {
			prev := peakInFlight.Load()
			if cur <= prev || peakInFlight.CompareAndSwap(prev, cur) {
				break
			}
		}
		defer inFlight.Add(-1)

		start := time.Since(press)
		time.Sleep(latency)
		mu.Lock()
		hits = append(hits, hit{path: r.URL.RequestURI(), start: start, end: time.Since(press)})
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

	press = time.Now()
	model := NewModel(ctx, knownRun, 160, 45)
	defer func() { _ = model.CleanUp() }()

	fetched := model.fetchBuild()().(BuildFetchedMsg)
	if fetched.Error != nil {
		t.Fatal(fetched.Error)
	}
	_, _ = model.Update(fetched)
	ready := time.Since(press)

	t.Logf("job->build ready %.3fms (peak in-flight %d)", float64(ready.Microseconds())/1000, peakInFlight.Load())
	mu.Lock()
	defer mu.Unlock()
	for _, h := range hits {
		t.Logf("%.3f -> %.3fms %s", float64(h.start.Microseconds())/1000, float64(h.end.Microseconds())/1000, h.path)
	}
	if len(hits) != 2 {
		t.Fatalf("requests = %d, want 2", len(hits))
	}
	if peakInFlight.Load() != 2 {
		t.Fatalf("peak concurrent requests = %d, want 2 (build and stage node must run in parallel)", peakInFlight.Load())
	}
	if model.loading || model.stageDetails.pipelineNode == nil || model.stageDetails.pipelineNode.ExecNode != "linux-1" {
		t.Fatal("build page was not ready with stage details")
	}
}

// Exercise concurrent discovery on the URL-only / My Runs open path with fixed
// latency: projected job details, GetPipelineRun, and projected build details
// run concurrently (1 RTT) before GetNode (2nd RTT).
func TestBuildLoadFromURLParallelizesDiscoveryBeforeStageFetch(t *testing.T) {
	const latency = 150 * time.Millisecond
	var mu sync.Mutex
	type hit struct {
		path       string
		rawPath    string
		start, end time.Duration
	}
	var hits []hit
	var inFlight, peakInFlight atomic.Int32
	press := time.Now()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cur := inFlight.Add(1)
		for {
			prev := peakInFlight.Load()
			if cur <= prev || peakInFlight.CompareAndSwap(prev, cur) {
				break
			}
		}
		defer inFlight.Add(-1)

		start := time.Since(press)
		time.Sleep(latency)
		mu.Lock()
		hits = append(hits, hit{path: r.URL.RequestURI(), rawPath: r.URL.Path, start: start, end: time.Since(press)})
		mu.Unlock()
		serveBuildLoadFixture(w, r)
	}))
	defer server.Close()

	ctx := buildLoadContext(server)
	jobURL := server.URL + fixtureJobPath + "/"

	press = time.Now()
	model := NewModelFromURL(ctx, jobURL, "42", 160, 45)
	defer func() { _ = model.CleanUp() }()

	fetched := model.fetchBuild()().(BuildFetchedMsg)
	if fetched.Error != nil {
		t.Fatal(fetched.Error)
	}
	_, _ = model.Update(fetched)
	ready := time.Since(press)

	t.Logf("myruns->build ready %.3fms (peak in-flight %d)", float64(ready.Microseconds())/1000, peakInFlight.Load())
	mu.Lock()
	defer mu.Unlock()
	var describeEnd, nodeStart time.Duration
	for _, h := range hits {
		t.Logf("%.3f -> %.3fms %s", float64(h.start.Microseconds())/1000, float64(h.end.Microseconds())/1000, h.path)
		if h.rawPath == fixtureLoadPaths[1] {
			describeEnd = h.end
		}
		if h.rawPath == fixtureLoadPaths[3] {
			nodeStart = h.start
		}
	}
	if len(hits) != 4 {
		t.Fatalf("requests = %d, want 4", len(hits))
	}
	if peakInFlight.Load() != 3 {
		t.Fatalf("peak concurrent requests = %d, want 3 (job, pipeline run, and build must run in parallel)", peakInFlight.Load())
	}
	if nodeStart < describeEnd {
		t.Fatalf("stage node request started at %v before wfapi/describe finished at %v", nodeStart, describeEnd)
	}
	if model.loading || model.stageDetails.pipelineNode == nil || model.stageDetails.pipelineNode.ExecNode != "linux-1" {
		t.Fatal("build page was not ready with stage details")
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
				t.Fatalf("failure = %#v, want scoped navigation error %q", failure, titles[i])
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
