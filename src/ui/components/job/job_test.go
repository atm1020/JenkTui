package job

import (
	stdctx "context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/buildaction"
	"github.com/atm1020/jenktui/src/ui/components/build"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

func TestFetchJobIsCancelledDuringCleanup(t *testing.T) {
	requestStarted := make(chan struct{})
	requestCancelled := make(chan struct{})
	var startOnce, cancelOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		startOnce.Do(func() { close(requestStarted) })
		<-request.Context().Done()
		cancelOnce.Do(func() { close(requestCancelled) })
	}))
	defer server.Close()

	cfg := config.GetDefaultConfig()
	cfg.JenkinsRequestTimeoutSeconds = 60
	runStore := store.New(t.TempDir())
	defer runStore.Close()
	model := NewModelFromUrl(&context.ProgramContext{
		Config: cfg, Jenkins: gojenkins.CreateJenkins(nil, server.URL), RunStore: runStore,
	}, 100, 30, server.URL+"/job/demo/")

	result := make(chan tea.Msg, 1)
	go func() { result <- model.fetchJob()() }()
	<-requestStarted
	_ = model.CleanUp()

	select {
	case <-requestCancelled:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not cancel the job request")
	}
	select {
	case msg := <-result:
		loaded, ok := msg.(JobLoadedMsg)
		if !ok || !errors.Is(loaded.Error, stdctx.Canceled) {
			t.Fatalf("fetch result = %#v, want cancelled JobLoadedMsg", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled job request did not return")
	}
}

func TestJobDropsStaleLoadGeneration(t *testing.T) {
	model := &Model{requestGeneration: 2, loading: true}
	if cmd := model.handleJobLoadedMsg(JobLoadedMsg{Generation: 1, Job: &gojenkins.Job{}}); cmd != nil {
		t.Fatal("stale result returned a command")
	}
	if model.job != nil || !model.loading {
		t.Fatal("stale result mutated the current page")
	}
}

func TestKnownJobShowsAnimatedLoadersInBothPanels(t *testing.T) {
	cfg := config.GetDefaultConfig()
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins")
	loadedJob := &gojenkins.Job{
		Jenkins: jenkins,
		Raw:     &gojenkins.JobResponse{Name: "demo", URL: "http://jenkins/job/demo/"},
	}
	model := NewModel(&context.ProgramContext{Config: cfg, Jenkins: jenkins}, 100, 30, loadedJob)

	view := model.View()
	if got := strings.Count(view, "Loading..."); got != 2 {
		t.Fatalf("loading placeholders = %d, want one in each panel:\n%s", got, view)
	}
	before := model.loadingSpinner.View()
	model.entered = true
	start := model.ensureAnimation()
	if start == nil {
		t.Fatal("loading page did not start its frame clock")
	}
	if _, cmd := model.Update(start()); cmd == nil {
		t.Fatal("loading frame did not schedule the next frame")
	}
	if after := model.loadingSpinner.View(); after == before {
		t.Fatalf("loading spinner frame did not advance: %q", after)
	}

	_ = model.handleJobLoadedMsg(JobLoadedMsg{Job: loadedJob})
	if model.loading {
		t.Fatal("job should no longer be loading after JobLoadedMsg")
	}
	if view := model.View(); strings.Contains(view, "Loading...") {
		t.Fatalf("loaded panels still show loading placeholders:\n%s", view)
	}
}

func TestCleanupInvalidatesTriggerParamsRequest(t *testing.T) {
	model := &Model{ctx: &context.ProgramContext{Config: config.GetDefaultConfig()}}
	fetch := model.paramsRequest.Fetch(model.ctx, nil)

	_ = model.CleanUp()
	_, cmd := model.Update(fetch())
	if cmd != nil {
		t.Fatal("parameter result survived page cleanup")
	}
}

func TestCancelBuildGuardsUnavailableAndFinishedRuns(t *testing.T) {
	cfg := config.GetDefaultConfig()
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins")
	job := &gojenkins.Job{
		Jenkins: jenkins,
		Base:    "/job/demo",
		Raw:     &gojenkins.JobResponse{Name: "demo", URL: "http://jenkins/job/demo/"},
	}

	for _, tt := range []struct {
		name string
		run  *gojenkins.PipelineRun
		want string
	}{
		{name: "no build warns", want: "warn"},
		{name: "running build confirms", run: &gojenkins.PipelineRun{ID: "42", Status: "IN_PROGRESS", Job: job}, want: "confirm"},
		{name: "finished build warns", run: &gojenkins.PipelineRun{ID: "42", Status: "SUCCESS", Job: job}, want: "warn"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			model := NewModel(&context.ProgramContext{Config: cfg, Jenkins: jenkins}, 100, 30, job)
			if tt.run != nil {
				model.stageView.SetPipelineRun([]gojenkins.PipelineRun{*tt.run})
			}

			cmd := model.handleCancelBuild()
			if cmd == nil {
				t.Fatal("cancel returned no command")
			}
			switch msg := cmd().(type) {
			case util.OpenDialogMsg:
				if tt.want != "confirm" {
					t.Fatalf("cancel opened a confirmation, want %s", tt.want)
				}
			case util.FooterNotiMsg:
				if tt.want != "warn" || msg.Type != util.NotiWarning {
					t.Fatalf("cancel warning = %#v, want %s", msg, tt.want)
				}
			default:
				t.Fatalf("cancel produced %T, want confirmation or warning", msg)
			}
		})
	}
}

func TestCleanupDoesNotCancelInFlightTrigger(t *testing.T) {
	postStarted := make(chan struct{})
	postContext := make(chan stdctx.Context, 1)
	releasePost := make(chan struct{})
	var postOnce sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/job/demo/api/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "demo", "url": "http://" + request.Host + "/job/demo/"})
		case strings.HasPrefix(request.URL.Path, "/crumbIssuer/api/json"):
			_ = json.NewEncoder(w).Encode(map[string]string{})
		case request.Method == http.MethodPost && request.URL.Path == "/job/demo/build":
			postOnce.Do(func() {
				postContext <- request.Context()
				close(postStarted)
			})
			<-releasePost
			w.Header().Set("Location", "http://"+request.Host+"/queue/item/123/")
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	cfg := config.GetDefaultConfig()
	runStore := store.New(t.TempDir())
	defer runStore.Close()
	jenkins := gojenkins.CreateJenkins(nil, server.URL)
	manager := poll.NewManager(jenkins, runStore, poll.Config{})
	defer manager.Stop()
	job := &gojenkins.Job{
		Jenkins: jenkins,
		Base:    "/job/demo",
		Raw:     &gojenkins.JobResponse{Name: "demo", URL: server.URL + "/job/demo/"},
	}
	model := &Model{ctx: &context.ProgramContext{Config: cfg, PollManager: manager}, job: job}

	result := make(chan tea.Msg, 1)
	go func() { result <- buildaction.Trigger(model.ctx, model.job, nil)() }()
	<-postStarted
	requestContext := <-postContext
	_ = model.CleanUp()
	select {
	case <-requestContext.Done():
		t.Fatalf("cleanup cancelled trigger context: %v", requestContext.Err())
	default:
	}
	close(releasePost)
	select {
	case msg := <-result:
		if _, ok := msg.(util.RunTriggeredMsg); !ok {
			t.Fatalf("trigger result = %#v, want RunTriggeredMsg", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("trigger did not finish after Jenkins responded")
	}
}

func TestJobWindowWatchIsReleasedDuringCleanup(t *testing.T) {
	var runRequests atomic.Int32
	firstRequest := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/demo/api/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "demo", "url": serverURL(r) + "/job/demo/"})
		case "/job/demo/wfapi/runs/api/json":
			runRequests.Add(1)
			select {
			case firstRequest <- struct{}{}:
			default:
			}
			_, _ = w.Write([]byte(`[]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	manager := poll.NewManager(gojenkins.CreateJenkins(nil, server.URL), nil, poll.Config{PollInterval: time.Second})
	manager.Start()
	defer manager.Stop()
	time.Sleep(20 * time.Millisecond)

	model := &Model{
		ctx:    &context.ProgramContext{PollManager: manager},
		jobUrl: server.URL + "/job/demo/",
	}
	_ = model.watchJobWindow()
	select {
	case <-firstRequest:
	case <-time.After(time.Second):
		t.Fatal("job watch did not start polling")
	}

	_ = model.CleanUp()
	time.Sleep(1100 * time.Millisecond)
	if got := runRequests.Load(); got != 1 {
		t.Fatalf("run requests after cleanup = %d, want 1", got)
	}
}

func serverURL(r *http.Request) string {
	return "http://" + r.Host
}

func TestPollingSnapshotReplacesLiveState(t *testing.T) {
	cfg := config.GetDefaultConfig()
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins")
	job := &gojenkins.Job{
		Jenkins: jenkins,
		Base:    "/job/demo",
		Raw:     &gojenkins.JobResponse{Name: "demo", URL: "http://jenkins/job/demo/"},
	}
	model := NewModel(&context.ProgramContext{Config: cfg, Jenkins: jenkins}, 100, 30, job)
	model.stageView.SetPipelineRun([]gojenkins.PipelineRun{{
		ID: "42", Job: job, Base: "/job/demo/42", Status: "IN_PROGRESS",
		Stages: []gojenkins.PipelineNode{{
			ID: "7", Name: "Build", Status: "IN_PROGRESS",
			StageFlowNodes: []gojenkins.PipelineNode{{ID: "step-1", Name: "Compile"}},
		}},
	}})

	snapshots := []poll.RunSnapshot{{
		BuildNumber: 42, Status: "SUCCESS", Activity: buildstatus.ActivityFinished,
		Stages: []poll.StageSnapshot{{ID: "7", Name: "Build", Status: "SUCCESS", ExecNode: "linux-1", DurationMillis: 250}},
	}}
	_ = model.applySnapshotData(model.pipelineRunsFromSnapshots(snapshots), false)

	if len(model.stageView.pipelineRuns) != 1 {
		t.Fatalf("pipeline run count = %d, want 1", len(model.stageView.pipelineRuns))
	}
	run := model.stageView.pipelineRuns[0]
	if run.Status != "SUCCESS" || run.Stages[0].Duration != 250 || run.Stages[0].ExecNode != "linux-1" {
		t.Fatalf("updated run = %#v, want successful stage duration 250 on linux-1", run)
	}
	if len(run.Stages[0].StageFlowNodes) != 0 {
		t.Fatal("polling update retained stage details outside the job snapshot contract")
	}
	if run.Job != job || run.Base != "/job/demo/42" {
		t.Fatalf("run identity = (%p, %q), want job and build URL", run.Job, run.Base)
	}
	if model.isBuildInProgress {
		t.Fatal("finished aggregate snapshot left build progress active")
	}
}

func TestRunHistoryQuery_FiltersByCurrentJobName(t *testing.T) {
	m := &Model{
		job: &gojenkins.Job{Raw: &gojenkins.JobResponse{URL: "http://a/job/team/job/deploy/"}},
	}

	query := m.RunHistoryQuery()

	if query.JobName != jobref.ToDisplayName("http://a/job/team/job/deploy/") {
		t.Errorf("JobName = %q, want current job display name", query.JobName)
	}
	if query.SortBy != store.RunSortDate || query.Direction != store.SortDescending {
		t.Errorf("sort = (%q, %q), want date descending", query.SortBy, query.Direction)
	}
}

func TestEnterOnBuildListOpensSelectedBuild(t *testing.T) {
	cfg := config.GetDefaultConfig()
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins")
	builds := []gojenkins.JobBuild{
		{Number: 42, URL: "http://jenkins/job/demo/42/"},
		{Number: 41, URL: "http://jenkins/job/demo/41/"},
	}
	loadedJob := &gojenkins.Job{
		Jenkins: jenkins,
		Base:    "/job/demo",
		Raw: &gojenkins.JobResponse{
			Name: "demo", URL: "http://jenkins/job/demo/", Builds: builds,
		},
	}
	model := NewModel(&context.ProgramContext{Config: cfg, Jenkins: jenkins}, 100, 30, loadedJob)
	model.loading = false
	model.activeView = JobListView
	model.stageView.SetPipelineRun([]gojenkins.PipelineRun{
		{ID: "42", Job: loadedJob, Base: "/job/demo/42"},
		{ID: "41", Job: loadedJob, Base: "/job/demo/41"},
	})
	model.jobList.setItems(builds)

	assertOpensBuild := func(wantID string) {
		t.Helper()
		cmd := model.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
		if cmd == nil {
			t.Fatal("enter on selected build returned no command")
		}
		result := cmd()
		msg, ok := result.(build.OpenBuildMsg)
		if !ok {
			t.Fatalf("enter command emitted %T, want build.OpenBuildMsg", result)
		}
		if msg.PipelineRun.ID != wantID {
			t.Fatalf("opened build ID = %q, want selected build %s", msg.PipelineRun.ID, wantID)
		}
	}

	_ = model.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyDown})
	assertOpensBuild("41")

	model.jobList.list.SetFilterText("41")
	assertOpensBuild("41")
}

func TestBuildListSearchUsesConfiguredKeyAndCapturesQuery(t *testing.T) {
	cfg := config.GetDefaultConfig()
	cfg.KeyMapConfig.Job.Search = context.KeyBinding{"s"}
	model := NewModel(
		&context.ProgramContext{Config: cfg, Jenkins: gojenkins.CreateJenkins(nil, "http://jenkins")},
		100,
		30,
		&gojenkins.Job{Raw: &gojenkins.JobResponse{URL: "http://jenkins/job/demo/"}},
	)
	model.loading = false
	model.activeView = JobListView
	model.jobList.setItems([]gojenkins.JobBuild{{Number: 42}, {Number: 41}})

	_ = model.handleKeyPressMsg(tea.KeyPressMsg{Code: 's', Text: "s"})
	if !model.jobList.IsFiltering() {
		t.Fatal("configured search key did not start filtering")
	}

	for _, r := range "41" {
		_ = model.handleKeyPressMsg(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if got := model.jobList.list.FilterInput.Value(); got != "41" {
		t.Fatalf("filter input value = %q, want %q", got, "41")
	}
}

func TestJobIsCapturingInputWhileBuildListFilters(t *testing.T) {
	jl := &BuildListModel{list: buildListModel()}
	jl.setItems([]gojenkins.JobBuild{{Number: 14}, {Number: 13}})
	m := &Model{jobList: jl, activeView: JobListView}

	if m.IsCapturingInput() {
		t.Fatal("should not capture input before filtering starts")
	}

	// Start filtering the build list with its filter key.
	ll, _ := jl.list.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	jl.list = &ll
	if jl.list.FilterState() != list.Filtering {
		t.Fatal("precondition: build list should be filtering after '/'")
	}

	if !m.IsCapturingInput() {
		t.Fatal("should capture input while the build list filters")
	}

	// The gate only applies while the build list is the active pane.
	m.activeView = StageView
	if m.IsCapturingInput() {
		t.Fatal("stage view is active; build-list filter should not capture global keys")
	}
}

func TestHasInProgressRun(t *testing.T) {
	m := Model{}

	tests := []struct {
		name     string
		runs     []gojenkins.PipelineRun
		expected bool
	}{
		{
			name:     "empty runs returns false",
			runs:     []gojenkins.PipelineRun{},
			expected: false,
		},
		{
			name: "nil stages returns false",
			runs: []gojenkins.PipelineRun{
				{ID: "1", Status: "SUCCESS"},
			},
			expected: false,
		},
		{
			name: "all stages SUCCESS returns false",
			runs: []gojenkins.PipelineRun{
				{
					ID:     "1",
					Status: "SUCCESS",
					Stages: []gojenkins.PipelineNode{
						{ID: "1", Name: "Build", Status: "SUCCESS"},
						{ID: "2", Name: "Test", Status: "SUCCESS"},
					},
				},
			},
			expected: false,
		},
		{
			name: "stage with IN_PROGRESS returns true",
			runs: []gojenkins.PipelineRun{
				{
					ID:     "1",
					Status: "IN_PROGRESS",
					Stages: []gojenkins.PipelineNode{
						{ID: "1", Name: "Build", Status: "SUCCESS"},
						{ID: "2", Name: "Test", Status: "IN_PROGRESS"},
					},
				},
			},
			expected: true,
		},
		{
			name: "stage with RUNNING returns true",
			runs: []gojenkins.PipelineRun{
				{
					ID:     "1",
					Status: "RUNNING",
					Stages: []gojenkins.PipelineNode{
						{ID: "1", Name: "Build", Status: "RUNNING"},
					},
				},
			},
			expected: true,
		},
		{
			name: "mixed runs second has in-progress stage",
			runs: []gojenkins.PipelineRun{
				{
					ID:     "1",
					Status: "SUCCESS",
					Stages: []gojenkins.PipelineNode{
						{ID: "1", Name: "Build", Status: "SUCCESS"},
					},
				},
				{
					ID:     "2",
					Status: "IN_PROGRESS",
					Stages: []gojenkins.PipelineNode{
						{ID: "1", Name: "Build", Status: "IN_PROGRESS"},
					},
				},
			},
			expected: true,
		},
		{
			name: "FAILED stages return false",
			runs: []gojenkins.PipelineRun{
				{
					ID:     "1",
					Status: "FAILED",
					Stages: []gojenkins.PipelineNode{
						{ID: "1", Name: "Build", Status: "FAILED"},
					},
				},
			},
			expected: false,
		},
		{
			name: "PENDING stages remain active",
			runs: []gojenkins.PipelineRun{
				{
					ID:     "1",
					Status: "PENDING",
					Stages: []gojenkins.PipelineNode{
						{ID: "1", Name: "Build", Status: "PENDING"},
					},
				},
			},
			expected: true,
		},
		{
			name: "paused input run returns true",
			runs: []gojenkins.PipelineRun{
				{
					ID:     "1",
					Status: "PAUSED_PENDING_INPUT",
					Stages: []gojenkins.PipelineNode{
						{ID: "1", Name: "Approve", Status: "PAUSED_PENDING_INPUT"},
					},
				},
			},
			expected: true,
		},
		{
			name: "ABORTED stages return false",
			runs: []gojenkins.PipelineRun{
				{
					ID:     "1",
					Status: "ABORTED",
					Stages: []gojenkins.PipelineNode{
						{ID: "1", Name: "Build", Status: "ABORTED"},
					},
				},
			},
			expected: false,
		},
		{
			name: "queued run with IN_PROGRESS status but no stages returns true",
			runs: []gojenkins.PipelineRun{
				{
					ID:     "1",
					Status: "IN_PROGRESS",
					Stages: []gojenkins.PipelineNode{},
				},
			},
			expected: true,
		},
		{
			name: "run with RUNNING status but no stages returns true",
			runs: []gojenkins.PipelineRun{
				{
					ID:     "1",
					Status: "RUNNING",
				},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.hasInProgressRun(tt.runs)
			if result != tt.expected {
				t.Errorf("hasInProgressRun() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestApplySnapshotData_UpdatesInProgressState(t *testing.T) {
	tests := []struct {
		name              string
		initialInProgress bool
		msgInProgress     bool
		expectedChanged   bool
	}{
		{
			name:              "no change when both false",
			initialInProgress: false,
			msgInProgress:     false,
		},
		{
			name:              "no change when both true",
			initialInProgress: true,
			msgInProgress:     true,
		},
		{
			name:              "transitions from false to true",
			initialInProgress: false,
			msgInProgress:     true,
			expectedChanged:   true,
		},
		{
			name:              "transitions from true to false",
			initialInProgress: true,
			msgInProgress:     false,
			expectedChanged:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				ctx:               &context.ProgramContext{},
				isBuildInProgress: tt.initialInProgress,
				stageView: &StageViewModel{
					hasInProgress: tt.initialInProgress,
					pulseBar:      animation.New(animation.Settings{Width: 32, FPS: 10}),
				},
			}

			m.applySnapshotData([]gojenkins.PipelineRun{}, tt.msgInProgress)

			if m.isBuildInProgress != tt.msgInProgress {
				t.Errorf("isBuildInProgress = %v, want %v", m.isBuildInProgress, tt.msgInProgress)
			}
			if m.stageView.hasInProgress != tt.msgInProgress {
				t.Errorf("stageView.hasInProgress = %v, want %v", m.stageView.hasInProgress, tt.msgInProgress)
			}
		})
	}
}

func TestApplySnapshotData_UpdatesJobList(t *testing.T) {
	builds := []gojenkins.JobBuild{
		{Number: 1, URL: "http://jenkins/job/test/1/"},
		{Number: 2, URL: "http://jenkins/job/test/2/"},
	}

	m := &Model{
		ctx: &context.ProgramContext{},
		job: &gojenkins.Job{
			Raw: &gojenkins.JobResponse{
				Builds: builds,
			},
		},
		stageView: &StageViewModel{pulseBar: animation.New(animation.Settings{Width: 32, FPS: 10})},
		jobList:   &BuildListModel{list: buildListModel()},
	}

	m.applySnapshotData([]gojenkins.PipelineRun{
		{ID: "1", Status: "SUCCESS"},
	}, false)

	if len(m.jobList.list.Items()) != len(builds) {
		t.Errorf("jobList items = %d, want %d", len(m.jobList.list.Items()), len(builds))
	}
}

func TestApplySnapshotData_NilJobSkipsJobListUpdate(t *testing.T) {
	m := &Model{
		ctx:       &context.ProgramContext{},
		job:       nil,
		stageView: &StageViewModel{pulseBar: animation.New(animation.Settings{Width: 32, FPS: 10})},
		jobList:   &BuildListModel{list: buildListModel()},
	}

	m.applySnapshotData([]gojenkins.PipelineRun{
		{ID: "1", Status: "SUCCESS"},
	}, false)

	if len(m.jobList.list.Items()) != 0 {
		t.Errorf("jobList items = %d, want 0 when job is nil", len(m.jobList.list.Items()))
	}
}

func newTestStageViewModel() *StageViewModel {
	return &StageViewModel{
		pulseBar: animation.New(animation.Settings{Width: 32, FPS: 10}),
		seenRuns: make(map[string]struct{}),
	}
}

func TestSetPipelineRun_InitialSet(t *testing.T) {
	svm := newTestStageViewModel()

	runs := []gojenkins.PipelineRun{
		{ID: "1", Status: "SUCCESS"},
		{ID: "2", Status: "IN_PROGRESS"},
	}

	svm.SetPipelineRun(runs)

	if svm.pipelineRuns == nil {
		t.Fatal("pipelineRuns should not be nil")
	}
	if len(svm.pipelineRuns) != 2 {
		t.Errorf("pipelineRuns length = %d, want 2", len(svm.pipelineRuns))
	}
	if svm.pipelineRuns[0].ID != "1" {
		t.Errorf("first run ID = %s, want '1'", svm.pipelineRuns[0].ID)
	}
	if svm.pipelineRuns[1].ID != "2" {
		t.Errorf("second run ID = %s, want '2'", svm.pipelineRuns[1].ID)
	}
}

func TestSetPipelineRun_UpdatesExistingRuns(t *testing.T) {
	svm := newTestStageViewModel()

	initial := []gojenkins.PipelineRun{
		{ID: "1", Status: "IN_PROGRESS"},
		{ID: "2", Status: "PENDING"},
	}
	svm.SetPipelineRun(initial)

	updated := []gojenkins.PipelineRun{
		{ID: "1", Status: "SUCCESS"},
		{ID: "2", Status: "FAILED"},
		{ID: "3", Status: "PENDING"},
	}
	svm.SetPipelineRun(updated)

	if len(svm.pipelineRuns) != 3 {
		t.Errorf("pipelineRuns length = %d, want 3", len(svm.pipelineRuns))
	}
	if svm.pipelineRuns[0].Status != "SUCCESS" {
		t.Errorf("run 1 status = %s, want SUCCESS", svm.pipelineRuns[0].Status)
	}
	if svm.pipelineRuns[1].Status != "FAILED" {
		t.Errorf("run 2 status = %s, want FAILED", svm.pipelineRuns[1].Status)
	}
}

func TestSetPipelineRun_WithEmpty(t *testing.T) {
	svm := newTestStageViewModel()

	initial := []gojenkins.PipelineRun{
		{ID: "1", Status: "SUCCESS"},
		{ID: "2", Status: "FAILED"},
	}
	svm.SetPipelineRun(initial)

	dupes := []gojenkins.PipelineRun{}
	svm.SetPipelineRun(dupes)

	if len(svm.pipelineRuns) != 2 {
		t.Errorf("pipelineRuns length = %d, want 2 (no duplicates)", len(svm.pipelineRuns))
	}
}

func TestSetPipelineRun_MixedNewAndExisting(t *testing.T) {
	svm := newTestStageViewModel()

	initial := []gojenkins.PipelineRun{
		{ID: "1", Status: "SUCCESS"},
	}
	svm.SetPipelineRun(initial)

	mixed := []gojenkins.PipelineRun{
		{ID: "1", Status: "SUCCESS"},
		{ID: "2", Status: "RUNNING"},
	}
	svm.SetPipelineRun(mixed)

	// Current implementation only updates existing runs, does not append new ones.
	// This test documents current behavior. Update expected length when adding append support.
	if len(svm.pipelineRuns) != 2 {
		t.Errorf("pipelineRuns length = %d, want 2 (current impl does not append new runs)", len(svm.pipelineRuns))
	}
}

func TestSetPipelineRun_EmptySlice(t *testing.T) {
	svm := newTestStageViewModel()

	empty := []gojenkins.PipelineRun{}
	svm.SetPipelineRun(empty)

	// Current impl uses append in a loop, so empty input leaves pipelineRuns nil
	if len(svm.pipelineRuns) != 0 {
		t.Errorf("pipelineRuns length = %d, want 0", len(svm.pipelineRuns))
	}
}

func TestSetPipelineRun_PartialPayloadKeepsExistingRuns(t *testing.T) {
	svm := newTestStageViewModel()

	initial := []gojenkins.PipelineRun{
		{ID: "5", Status: "IN_PROGRESS"},
		{ID: "4", Status: "SUCCESS"},
		{ID: "3", Status: "SUCCESS"},
	}
	svm.SetPipelineRun(initial)

	partial := []gojenkins.PipelineRun{
		{ID: "5", Status: "SUCCESS"},
		{ID: "4", Status: "SUCCESS"},
	}
	svm.SetPipelineRun(partial)

	if len(svm.pipelineRuns) != 3 {
		t.Fatalf("pipelineRuns length = %d, want 3", len(svm.pipelineRuns))
	}
	if svm.pipelineRuns[0].ID != "5" || svm.pipelineRuns[0].Status != "SUCCESS" {
		t.Fatalf("run[0] = (%s,%s), want (5,SUCCESS)", svm.pipelineRuns[0].ID, svm.pipelineRuns[0].Status)
	}
	if svm.pipelineRuns[1].ID != "4" {
		t.Fatalf("run[1] id = %s, want 4", svm.pipelineRuns[1].ID)
	}
	if svm.pipelineRuns[2].ID != "3" {
		t.Fatalf("run[2] id = %s, want 3 (retained from previous snapshot)", svm.pipelineRuns[2].ID)
	}
}

func TestSetPipelineRun_DropsQueuedShadowWhenNewRunAppears(t *testing.T) {
	svm := newTestStageViewModel()

	initial := []gojenkins.PipelineRun{
		{ID: "queued", Status: "QUEUED"},
		{ID: "7", Status: "IN_PROGRESS"},
	}
	svm.SetPipelineRun(initial)

	incoming := []gojenkins.PipelineRun{
		{ID: "8", Status: "RUNNING"},
		{ID: "7", Status: "SUCCESS"},
	}
	svm.SetPipelineRun(incoming)

	if len(svm.pipelineRuns) != 2 {
		t.Fatalf("pipelineRuns length = %d, want 2", len(svm.pipelineRuns))
	}
	if svm.pipelineRuns[0].ID != "8" {
		t.Fatalf("run[0] id = %s, want 8", svm.pipelineRuns[0].ID)
	}
	if svm.pipelineRuns[1].ID != "7" {
		t.Fatalf("run[1] id = %s, want 7", svm.pipelineRuns[1].ID)
	}
	for i := range svm.pipelineRuns {
		if svm.pipelineRuns[i].ID == "queued" {
			t.Fatal("queued shadow run should be removed when new real run appears")
		}
	}
}

func TestSetPipelineRun_DropsQueuedShadowOnAnyIncomingPayload(t *testing.T) {
	svm := newTestStageViewModel()

	initial := []gojenkins.PipelineRun{
		{ID: "queued", Status: "QUEUED"},
		{ID: "7", Status: "IN_PROGRESS"},
	}
	svm.SetPipelineRun(initial)

	incoming := []gojenkins.PipelineRun{
		{ID: "7", Status: "IN_PROGRESS"},
	}
	svm.SetPipelineRun(incoming)

	if len(svm.pipelineRuns) != 1 {
		t.Fatalf("pipelineRuns length = %d, want 1", len(svm.pipelineRuns))
	}
	if svm.pipelineRuns[0].ID != "7" {
		t.Fatalf("run[0] id = %s, want 7", svm.pipelineRuns[0].ID)
	}
}

func TestToggleFavorite_TogglesIsFavoriteAndPersists(t *testing.T) {
	runStore := store.New(t.TempDir())
	m := &Model{
		ctx:    &context.ProgramContext{RunStore: runStore},
		job:    &gojenkins.Job{Raw: &gojenkins.JobResponse{URL: "http://a/job/x/"}},
		jobUrl: "http://a/job/x/",
	}

	cmd := m.toggleFavorite()
	msg := cmd()
	toggled, ok := msg.(FavoriteToggledMsg)
	if !ok || !toggled.Favorited {
		t.Fatalf("first toggle = %+v, want FavoriteToggledMsg{Favorited: true}", msg)
	}
	is, err := runStore.IsFavorite("http://a/job/x/")
	if err != nil || !is {
		t.Fatalf("IsFavorite after toggle = (%v, %v), want (true, nil)", is, err)
	}

	m.isFavorite = true // Update's FavoriteToggledMsg handler would normally set this
	cmd = m.toggleFavorite()
	msg = cmd()
	toggled, ok = msg.(FavoriteToggledMsg)
	if !ok || toggled.Favorited {
		t.Fatalf("second toggle = %+v, want FavoriteToggledMsg{Favorited: false}", msg)
	}
	is, err = runStore.IsFavorite("http://a/job/x/")
	if err != nil || is {
		t.Fatalf("IsFavorite after second toggle = (%v, %v), want (false, nil)", is, err)
	}
}
