package build

import (
	stdctx "context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/ui/buildaction"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/artifactselector"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

func TestFetchBuildIsCancelledDuringCleanup(t *testing.T) {
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
	jenkins := gojenkins.CreateJenkins(nil, server.URL)
	job := &gojenkins.Job{
		Jenkins: jenkins,
		Base:    "/job/demo",
		Raw:     &gojenkins.JobResponse{Name: "demo", URL: server.URL + "/job/demo/"},
	}
	model := &Model{
		ctx:         &appcontext.ProgramContext{Config: cfg},
		pipelineRun: gojenkins.PipelineRun{ID: "42", Job: job, Status: "SUCCESS"},
	}

	result := make(chan tea.Msg, 1)
	cmd := model.fetchBuild()
	go func() { result <- cmd() }()
	<-requestStarted
	_ = model.CleanUp()

	select {
	case <-requestCancelled:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not cancel the build request")
	}
	select {
	case msg := <-result:
		fetched, ok := msg.(BuildFetchedMsg)
		if !ok || !errors.Is(fetched.Error, stdctx.Canceled) {
			t.Fatalf("fetch result = %#v, want cancelled BuildFetchedMsg", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled build request did not return")
	}
}

func TestBuildDropsStaleLoadGeneration(t *testing.T) {
	model := &Model{fetchGeneration: 2}
	if cmd := model.handleBuildFetchMsg(BuildFetchedMsg{model: model, Generation: 1}); cmd != nil {
		t.Fatal("stale result returned a command")
	}
	if model.build != nil {
		t.Fatal("stale result mutated the current page")
	}
}

func TestExactRunWatchIsReleasedDuringCleanup(t *testing.T) {
	var exactRequests atomic.Int32
	firstRequest := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/demo/api/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "demo", "url": "http://" + r.Host + "/job/demo/"})
		case "/job/demo/42/wfapi/describe/api/json":
			exactRequests.Add(1)
			select {
			case firstRequest <- struct{}{}:
			default:
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "42", "status": "IN_PROGRESS", "stages": []any{},
				"_links": map[string]any{"self": map[string]string{"href": "/job/demo/42/wfapi/describe"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := gojenkins.CreateJenkins(nil, server.URL)
	manager := poll.NewManager(client, nil, poll.Config{PollInterval: time.Second})
	manager.Start()
	defer manager.Stop()
	time.Sleep(20 * time.Millisecond)
	job := &gojenkins.Job{Jenkins: client, Base: "/job/demo", Raw: &gojenkins.JobResponse{URL: server.URL + "/job/demo/"}}
	model := &Model{
		ctx:         &appcontext.ProgramContext{PollManager: manager},
		pipelineRun: gojenkins.PipelineRun{ID: "42", Job: job, Base: "/job/demo/42", Status: "IN_PROGRESS"},
	}

	_ = model.watchExactRun()
	select {
	case <-firstRequest:
	case <-time.After(time.Second):
		t.Fatal("exact-run watch did not start polling")
	}
	_ = model.CleanUp()
	time.Sleep(1100 * time.Millisecond)
	if got := exactRequests.Load(); got != 1 {
		t.Fatalf("exact requests after cleanup = %d, want 1", got)
	}
}

func TestApplyPollSnapshotUpdatesExactRunAndPreservesStageDetails(t *testing.T) {
	job := &gojenkins.Job{Base: "/job/demo", Raw: &gojenkins.JobResponse{URL: "http://jenkins/job/demo/"}}
	model := &Model{pipelineRun: gojenkins.PipelineRun{
		ID: "42", Job: job, Base: "/job/demo/42", Status: "IN_PROGRESS",
		Stages: []gojenkins.PipelineNode{{
			ID: "7", Name: "Build", Status: "IN_PROGRESS", ExecNode: "old-node",
			StageFlowNodes: []gojenkins.PipelineNode{{ID: "step-1", Name: "Compile"}},
		}},
	}}

	_ = model.ApplyPollSnapshot(poll.Snapshot{
		Target: poll.Target{Kind: poll.TargetExactRun, JobURL: job.Raw.URL, BuildNumber: 42},
		Runs: []poll.RunSnapshot{{
			BuildNumber: 42, Status: buildstatus.StatusSuccess, Activity: buildstatus.ActivityFinished,
			Stages: []poll.StageSnapshot{{ID: "7", Name: "Build", Status: buildstatus.StatusSuccess, ExecNode: "linux-1", DurationMillis: 250}},
		}},
	})

	if model.pipelineRun.Status != "SUCCESS" || model.pipelineRun.Stages[0].Duration != 250 || model.pipelineRun.Stages[0].ExecNode != "linux-1" {
		t.Fatalf("updated run = %#v, want successful stage duration 250 on linux-1", model.pipelineRun)
	}
	if len(model.pipelineRun.Stages[0].StageFlowNodes) != 1 || model.pipelineRun.Stages[0].StageFlowNodes[0].ID != "step-1" {
		t.Fatal("typed exact-run update discarded existing stage detail metadata")
	}
}

// TestIsTerminal_QueuedBuildIsNotTerminal covers a freshly-triggered build
// that's still waiting in the Jenkins queue: wfapi/poll.isActiveBuildStatus
// treats QUEUED as an active status, but build.Model.isTerminal only
// excluded RUNNING/IN_PROGRESS/PENDING, so a QUEUED build with no stages yet
// was misclassified as terminal and flashed "No stage data" until the next
// poll tick corrected it.
func TestIsTerminal_QueuedBuildIsNotTerminal(t *testing.T) {
	tests := []struct {
		status  string
		endTime int64
		want    bool
	}{
		{"QUEUED", 0, false},
		{"PENDING", 0, false},
		{"IN_PROGRESS", 0, false},
		{"RUNNING", 0, false},
		{"PAUSED_PENDING_INPUT", 0, false},
		{"SUCCESS", 1000, true},
		{"FAILED", 1000, true},
		{"ABORTED", 1000, true},
		{"NOT_EXECUTED", 0, false},
		{"NOT_EXECUTED", 1000, true},
	}
	for _, tt := range tests {
		m := &Model{pipelineRun: gojenkins.PipelineRun{Status: tt.status, EndTime: tt.endTime}}
		if got := m.isTerminal(); got != tt.want {
			t.Errorf("isTerminal() with status %q = %v, want %v", tt.status, got, tt.want)
		}
	}
}

func TestBuildInfoRendersNodeSummary(t *testing.T) {
	tests := []struct {
		name   string
		stages []gojenkins.PipelineNode
		want   string
	}{
		{name: "unavailable", want: "Node: —"},
		{name: "built-in", stages: []gojenkins.PipelineNode{{Name: "Build"}}, want: "Node: built-in"},
		{name: "named", stages: []gojenkins.PipelineNode{{Name: "Build", ExecNode: "linux-1"}}, want: "Node: linux-1"},
		{name: "multiple", stages: []gojenkins.PipelineNode{{Name: "Build", ExecNode: "linux-1"}, {Name: "Test", ExecNode: "linux-2"}}, want: "Node: multiple (2)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{pipelineRun: gojenkins.PipelineRun{Stages: tt.stages}}
			if got := ansi.Strip(m.renderBuildInfo(m.spinnerModel.View())); !strings.Contains(got, tt.want) {
				t.Fatalf("build info = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildInfoUsesFriendlyStatusLabel(t *testing.T) {
	m := &Model{pipelineRun: gojenkins.PipelineRun{
		Status: "IN_PROGRESS",
		Stages: []gojenkins.PipelineNode{{Status: "IN_PROGRESS"}},
	}}

	got := ansi.Strip(m.renderBuildInfo(m.spinnerModel.View()))
	if !strings.Contains(got, "Status: In progress") {
		t.Errorf("build info does not contain friendly status label: %q", got)
	}
	if strings.Contains(got, "IN_PROGRESS") {
		t.Errorf("build info leaks raw Jenkins status: %q", got)
	}
}

func TestPausedPendingInputRendersWaitingForApproval(t *testing.T) {
	m := &Model{
		wWidth: 120,
		pipelineRun: gojenkins.PipelineRun{
			ID:     "42",
			Name:   "#42",
			Status: "PAUSED_PENDING_INPUT",
			Stages: []gojenkins.PipelineNode{{
				ID:     "10",
				Name:   "Deploy",
				Status: "PAUSED_PENDING_INPUT",
			}},
		},
	}

	theme := styles.GetTheme()
	orangeApprovalText := theme.S().Base.Foreground(theme.Warning).Render("Waiting for approval")
	if got := m.renderBuildInfo(m.spinnerModel.View()); !strings.Contains(got, orangeApprovalText) {
		t.Fatalf("build info does not render approval status in warning color: %q", got)
	}
	if got := m.renderStages(); !strings.Contains(got, orangeApprovalText) {
		t.Fatalf("stage does not render approval status in warning color: %q", got)
	}
}

func TestModelHandleWindowSizeMsgPassesAvailableHeightToStageDetails(t *testing.T) {
	stageView := viewport.New()
	stageView.SetHeight(10)

	m := &Model{
		keyMap:       &KeyMap{},
		stageDetails: NewStageDetails(80, 50, &KeyMap{}),
		stageView:    &stageView,
	}

	msg := tea.WindowSizeMsg{Width: 80, Height: 50}
	m.stageView.SetWidth(msg.Width)
	m.updateStageViewLayout()
	expectedHeight := msg.Height - lipgloss.Height(lipgloss.JoinHorizontal(
		lipgloss.Center,
		m.renderBuildInfo(m.spinnerModel.View()),
		m.renderArtifacts(),
	)) - lipgloss.Height(m.stageView.View())

	m.handleWindowSizeMsg(msg)

	if got := m.stageDetails.wHeight; got != expectedHeight {
		t.Fatalf("stage details height = %d, want available height %d", got, expectedHeight)
	}
}

func TestDownloadArtifactsOpensSelectorWhenBuildHasMultipleArtifacts(t *testing.T) {
	cfg := config.GetDefaultConfig()
	m := &Model{
		ctx:     &appcontext.ProgramContext{Config: cfg},
		build:   buildWithArtifacts(t, "one.zip", "two.zip"),
		keyMap:  UserKeyMap(cfg.KeyMapConfig.Build),
		wWidth:  80,
		wHeight: 24,
	}

	cmd := m.handleKeyPressMsgStageView(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if cmd == nil {
		t.Fatal("artifact chooser key returned no command")
	}
	result := cmd()
	msg, ok := result.(util.OpenDialogMsg)
	if !ok {
		t.Fatalf("artifact chooser command produced %T, want OpenDialogMsg", result)
	}
	if _, ok := msg.Dialog.(*artifactselector.Dialog); !ok {
		t.Fatalf("opened dialog = %T, want artifact selector", msg.Dialog)
	}
}

func TestDownloadArtifactsSkipsSelectorForSingleArtifact(t *testing.T) {
	cfg := config.GetDefaultConfig()
	m := &Model{
		ctx:    &appcontext.ProgramContext{Config: cfg, DataArtifactsDir: t.TempDir()},
		build:  buildWithArtifacts(t, "one.zip"),
		keyMap: UserKeyMap(cfg.KeyMapConfig.Build),
		pipelineRun: gojenkins.PipelineRun{
			ID:  "42",
			Job: &gojenkins.Job{Raw: &gojenkins.JobResponse{Name: "demo"}},
		},
	}

	cmd := m.handleKeyPressMsgStageView(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if cmd == nil {
		t.Fatal("single-artifact download returned no command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("single-artifact download produced no message")
	} else if _, ok := msg.(util.OpenDialogMsg); ok {
		t.Fatal("single-artifact download opened the artifact selector")
	}
}

func TestDownloadAllArtifactsShortcutBypassesSelector(t *testing.T) {
	cfg := config.GetDefaultConfig()
	m := &Model{
		ctx:    &appcontext.ProgramContext{Config: cfg, DataArtifactsDir: t.TempDir()},
		build:  buildWithArtifacts(t, "one.zip", "two.zip"),
		keyMap: UserKeyMap(cfg.KeyMapConfig.Build),
		pipelineRun: gojenkins.PipelineRun{
			ID:  "42",
			Job: &gojenkins.Job{Raw: &gojenkins.JobResponse{Name: "demo"}},
		},
	}

	cmd := m.handleKeyPressMsgStageView(tea.KeyPressMsg{Code: 'D', Text: "D"})
	if cmd == nil {
		t.Fatal("download-all key returned no command")
	}
	if msg := cmd(); msg == nil {
		t.Fatal("download-all command produced no message")
	} else if _, ok := msg.(util.OpenDialogMsg); ok {
		t.Fatal("download-all shortcut opened the artifact selector")
	}
}

func TestArtifactDownloadFailureBecomesVisibleError(t *testing.T) {
	m := &Model{}
	wantErr := os.ErrPermission

	_, cmd := m.Update(ArtifactsDownloadFailedMsg{Error: wantErr})
	if cmd == nil {
		t.Fatal("artifact download failure returned no command")
	}
	result := cmd()
	msg, ok := result.(util.ErrorMsg)
	if !ok {
		t.Fatalf("failure command produced %T, want ErrorMsg", result)
	}
	if msg.Error != wantErr || msg.Title != "Failed to download artifacts" {
		t.Fatalf("error message = %#v", msg)
	}
}

func TestDownloadArtifactsUsesRequestedSubset(t *testing.T) {
	var selectedRequests atomic.Int32
	var skippedRequests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/artifact/selected.zip/":
			selectedRequests.Add(1)
			_, _ = w.Write([]byte("selected"))
		case r.URL.Path == "/artifact/skipped.zip/":
			skippedRequests.Add(1)
			_, _ = w.Write([]byte("skipped"))
		case strings.HasPrefix(r.URL.Path, "/fingerprint/"):
			hash := md5.Sum([]byte("selected"))
			_ = json.NewEncoder(w).Encode(map[string]string{"hash": hex.EncodeToString(hash[:])})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := gojenkins.CreateJenkins(nil, server.URL)
	job := &gojenkins.Job{Raw: &gojenkins.JobResponse{Name: "demo"}}
	dataDir := t.TempDir()
	build := buildWithArtifacts(t, "selected.zip", "skipped.zip")
	build.Jenkins = client
	build.Job = job
	m := &Model{
		ctx:         &appcontext.ProgramContext{DataArtifactsDir: dataDir},
		pipelineRun: gojenkins.PipelineRun{ID: "42", Job: job},
		build:       build,
	}
	artifacts := m.build.GetArtifacts()

	msg := m.downloadArtifacts(artifacts[:1])()
	completed, ok := msg.(ArtifactsDownloadedMsg)
	if !ok {
		t.Fatalf("download result = %#v, want ArtifactsDownloadedMsg", msg)
	}
	if completed.Count != 1 {
		t.Fatalf("downloaded count = %d, want 1", completed.Count)
	}
	if selectedRequests.Load() != 1 || skippedRequests.Load() != 0 {
		t.Fatalf("requests selected=%d skipped=%d, want 1 and 0", selectedRequests.Load(), skippedRequests.Load())
	}
	artifactPath := filepath.Join(dataDir, "jenkins", "demo", "42", "selected.zip")
	if _, err := os.Stat(artifactPath); err != nil {
		t.Fatalf("selected artifact was not saved: %v", err)
	}
	info, err := os.Stat(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("artifact mode = %o, want 0600", info.Mode().Perm())
	}
}

func TestDownloadArtifactsReportsUnsuccessfulSave(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	client := gojenkins.CreateJenkins(nil, server.URL)
	job := &gojenkins.Job{Raw: &gojenkins.JobResponse{Name: "demo"}}
	build := buildWithArtifacts(t, "secret.zip")
	build.Jenkins = client
	build.Job = job
	m := &Model{
		ctx:         &appcontext.ProgramContext{DataArtifactsDir: t.TempDir()},
		pipelineRun: gojenkins.PipelineRun{ID: "42", Job: job},
		build:       build,
	}

	msg := m.downloadArtifacts(m.build.GetArtifacts())()
	if _, ok := msg.(ArtifactsDownloadFailedMsg); !ok {
		t.Fatalf("download result = %#v, want ArtifactsDownloadFailedMsg", msg)
	}
}

func TestDownloadArtifactsReportsWriteFailure(t *testing.T) {
	dataDir := t.TempDir()
	job := &gojenkins.Job{Raw: &gojenkins.JobResponse{Name: "demo"}}
	artifactPath := filepath.Join(dataDir, "jenkins", "demo", "42", "output.zip")
	if err := os.MkdirAll(artifactPath, 0o700); err != nil {
		t.Fatal(err)
	}
	m := &Model{
		ctx:         &appcontext.ProgramContext{DataArtifactsDir: dataDir},
		pipelineRun: gojenkins.PipelineRun{ID: "42", Job: job},
	}

	msg := m.downloadArtifacts([]gojenkins.Artifact{{FileName: "output.zip"}})()
	if _, ok := msg.(ArtifactsDownloadFailedMsg); !ok {
		t.Fatalf("download result = %#v, want ArtifactsDownloadFailedMsg", msg)
	}
}

func TestDownloadArtifactsReportsFingerprintFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/artifact/output.zip/" {
			_, _ = w.Write([]byte("artifact"))
			return
		}
		http.Error(w, "fingerprint unavailable", http.StatusInternalServerError)
	}))
	defer server.Close()

	client := gojenkins.CreateJenkins(nil, server.URL)
	job := &gojenkins.Job{Raw: &gojenkins.JobResponse{Name: "demo"}}
	build := buildWithArtifacts(t, "output.zip")
	build.Jenkins = client
	build.Job = job
	m := &Model{
		ctx:         &appcontext.ProgramContext{DataArtifactsDir: t.TempDir()},
		pipelineRun: gojenkins.PipelineRun{ID: "42", Job: job},
		build:       build,
	}

	msg := m.downloadArtifacts(m.build.GetArtifacts())()
	if _, ok := msg.(ArtifactsDownloadFailedMsg); !ok {
		t.Fatalf("download result = %#v, want ArtifactsDownloadFailedMsg", msg)
	}
}

func TestDownloadArtifactsRejectsUnsafeFilename(t *testing.T) {
	job := &gojenkins.Job{Raw: &gojenkins.JobResponse{Name: "demo"}}
	m := &Model{
		ctx:         &appcontext.ProgramContext{DataArtifactsDir: t.TempDir()},
		pipelineRun: gojenkins.PipelineRun{ID: "42", Job: job},
	}
	msg := m.downloadArtifacts([]gojenkins.Artifact{{FileName: "../outside"}})()
	if _, ok := msg.(ArtifactsDownloadFailedMsg); !ok {
		t.Fatalf("download result = %#v, want ArtifactsDownloadFailedMsg", msg)
	}
}

func buildWithArtifacts(t *testing.T, names ...string) *gojenkins.Build {
	t.Helper()
	items := make([]map[string]string, len(names))
	for i, name := range names {
		items[i] = map[string]string{
			"fileName":     name,
			"relativePath": name,
		}
	}
	data, err := json.Marshal(map[string]any{"artifacts": items})
	if err != nil {
		t.Fatal(err)
	}
	var raw gojenkins.BuildResponse
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	return &gojenkins.Build{Raw: &raw}
}

// The build view triggers a new run of the job it belongs to, starting the
// same parameter flow the job view uses.
func TestTriggerBuildKeyFetchesJobParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/job/demo/api/json" {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "demo",
				"url":  "http://" + request.Host + "/job/demo/",
				"property": []map[string]any{{
					"parameterDefinitions": []map[string]any{{"name": "BRANCH", "type": "StringParameterDefinition"}},
				}},
			})
			return
		}
		http.NotFound(w, request)
	}))
	defer server.Close()

	cfg := config.GetDefaultConfig()
	jenkins := gojenkins.CreateJenkins(nil, server.URL)
	m := &Model{
		ctx:    &appcontext.ProgramContext{Config: cfg, Jenkins: jenkins},
		build:  &gojenkins.Build{},
		keyMap: UserKeyMap(cfg.KeyMapConfig.Build),
		pipelineRun: gojenkins.PipelineRun{
			ID: "42",
			Job: &gojenkins.Job{
				Jenkins: jenkins,
				Base:    "/job/demo",
				Raw:     &gojenkins.JobResponse{Name: "demo", URL: server.URL + "/job/demo/"},
			},
		},
	}

	cmd := m.handleKeyPressMsgStageView(tea.KeyPressMsg{Code: 'b', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("trigger build key returned no command")
	}
	msg, ok := cmd().(buildaction.ParamsFetchedMsg)
	if !ok {
		t.Fatalf("trigger command produced %T, want buildaction.ParamsFetchedMsg", cmd())
	}
	if msg.Error != nil {
		t.Fatalf("params fetch error = %v, want nil", msg.Error)
	}
	if len(msg.Params) != 1 || msg.Params[0].Name != "BRANCH" {
		t.Fatalf("fetched params = %#v, want a single BRANCH parameter", msg.Params)
	}
}

func TestCleanupInvalidatesTriggerParamsRequest(t *testing.T) {
	m := &Model{ctx: &appcontext.ProgramContext{Config: config.GetDefaultConfig()}}
	fetch := m.paramsRequest.Fetch(m.ctx, nil)

	_ = m.CleanUp()
	_, cmd := m.Update(fetch())
	if cmd != nil {
		t.Fatal("parameter result survived page cleanup")
	}
}

func TestRunTriggeredNavigatesToJob(t *testing.T) {
	job := &gojenkins.Job{Raw: &gojenkins.JobResponse{Name: "demo", URL: "http://jenkins/job/demo/"}}
	m := &Model{
		pipelineRun: gojenkins.PipelineRun{ID: "42", Job: job},
		jobURL:      job.Raw.URL,
	}
	queueID := int64(123)

	for _, msg := range []util.RunTriggeredMsg{
		{Job: job, SourceViewID: m.ID()},
		{Job: job, QueueID: &queueID, Tracked: true, SourceViewID: m.ID()},
	} {
		_, cmd := m.Update(msg)
		if cmd == nil {
			t.Fatal("triggered run returned no navigation command")
		}
		result := cmd()
		open, ok := result.(util.OpenJobComponentMsg)
		if !ok {
			t.Fatalf("trigger navigation = %T, want OpenJobComponentMsg", result)
		}
		if open.Job != job {
			t.Fatalf("job destination = %p, want %p", open.Job, job)
		}
	}
}

func TestRunTriggeredFromAnotherViewDoesNotNavigate(t *testing.T) {
	m := &Model{buildURL: "http://jenkins/job/demo/42"}
	_, cmd := m.Update(util.RunTriggeredMsg{SourceViewID: "build:http://jenkins/job/other/7"})
	if cmd != nil {
		t.Fatal("trigger from another view returned a navigation command")
	}
}

// Aborting must confirm first for a live build, and say plainly that there is
// nothing to stop for one that already finished.
func TestAbortBuildGuardsFinishedRuns(t *testing.T) {
	cfg := config.GetDefaultConfig()
	job := &gojenkins.Job{Raw: &gojenkins.JobResponse{Name: "demo"}}

	for _, tt := range []struct {
		name   string
		status string
		want   string
	}{
		{name: "running build asks for confirmation", status: "IN_PROGRESS", want: "confirm"},
		{name: "finished build warns instead", status: "SUCCESS", want: "warn"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := &Model{
				ctx:         &appcontext.ProgramContext{Config: cfg},
				build:       &gojenkins.Build{},
				keyMap:      UserKeyMap(cfg.KeyMapConfig.Build),
				pipelineRun: gojenkins.PipelineRun{ID: "42", Status: tt.status, Job: job},
			}

			cmd := m.handleKeyPressMsgStageView(tea.KeyPressMsg{Code: 'x', Text: "x"})
			if cmd == nil {
				t.Fatal("cancel build key returned no command")
			}

			switch msg := cmd().(type) {
			case util.OpenDialogMsg:
				if tt.want != "confirm" {
					t.Fatalf("opened a confirmation for a %s build, want a warning", tt.status)
				}
			case util.FooterNotiMsg:
				if tt.want != "warn" {
					t.Fatalf("warned instead of confirming for a %s build", tt.status)
				}
				if msg.Type != util.NotiWarning || !strings.Contains(msg.Message, "#42") {
					t.Fatalf("warning = %#v, want a warning naming build #42", msg)
				}
			default:
				t.Fatalf("cancel produced %T, want a dialog or a footer warning", msg)
			}
		})
	}
}
