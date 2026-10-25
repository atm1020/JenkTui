package buildaction

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/buildparams"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/confirm"
	"github.com/atm1020/jenktui/src/ui/util"

	"github.com/atm1020/gojenkins"
)

func demoJob(jenkins *gojenkins.Jenkins, serverURL string) *gojenkins.Job {
	return &gojenkins.Job{
		Jenkins: jenkins,
		Base:    "/job/demo",
		Raw:     &gojenkins.JobResponse{Name: "demo", URL: serverURL + "/job/demo/"},
	}
}

func TestTriggerRequiresPollManager(t *testing.T) {
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins")

	msg := Trigger(&context.ProgramContext{}, demoJob(jenkins, "http://jenkins"), nil)()

	errMsg, ok := msg.(util.ErrorMsg)
	if !ok {
		t.Fatalf("trigger result = %T, want util.ErrorMsg", msg)
	}
	if errMsg.Title != "Failed to trigger build" || errMsg.Error == nil || errMsg.Error.Error() != "poll manager is not available" {
		t.Fatalf("trigger error = %#v, want unavailable poll manager", errMsg)
	}
}

func TestTriggerRequiresJob(t *testing.T) {
	msg := Trigger(&context.ProgramContext{PollManager: poll.NewManager(nil, nil, poll.Config{})}, nil, nil)()

	errMsg, ok := msg.(util.ErrorMsg)
	if !ok {
		t.Fatalf("trigger result = %T, want util.ErrorMsg", msg)
	}
	if errMsg.Error == nil || errMsg.Error.Error() != "job is not available" {
		t.Fatalf("trigger error = %#v, want unavailable job", errMsg)
	}
}

// A trigger must reach Jenkins with a queue receipt and come back as a tracked
// RunTriggeredMsg, which is what makes the app shell start following the run.
func TestTriggerReturnsTrackedRunTriggered(t *testing.T) {
	var postedDelay string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch {
		case request.URL.Path == "/job/demo/api/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "demo", "url": "http://" + request.Host + "/job/demo/"})
		case strings.HasPrefix(request.URL.Path, "/crumbIssuer/api/json"):
			_ = json.NewEncoder(w).Encode(map[string]string{})
		case request.Method == http.MethodPost && request.URL.Path == "/job/demo/build":
			_ = request.ParseForm()
			postedDelay = request.PostFormValue("delay")
			w.Header().Set("Location", "http://"+request.Host+"/queue/item/123/")
			w.WriteHeader(http.StatusCreated)
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	runStore := store.New(t.TempDir())
	defer runStore.Close()
	jenkins := gojenkins.CreateJenkins(nil, server.URL)
	manager := poll.NewManager(jenkins, runStore, poll.Config{})
	defer manager.Stop()
	ctx := &context.ProgramContext{Config: config.GetDefaultConfig(), PollManager: manager}

	msg := Trigger(ctx, demoJob(jenkins, server.URL), nil)()

	triggered, ok := msg.(util.RunTriggeredMsg)
	if !ok {
		t.Fatalf("trigger result = %#v, want util.RunTriggeredMsg", msg)
	}
	if !triggered.Tracked || triggered.IntentID == "" {
		t.Fatalf("trigger result = %#v, want a tracked intent", triggered)
	}
	if triggered.QueueID == nil || *triggered.QueueID != 123 {
		t.Fatalf("queue ID = %v, want 123", triggered.QueueID)
	}
	// An unparameterised trigger still has to carry the delay Jenkins expects.
	if postedDelay != "0sec" {
		t.Fatalf("posted delay = %q, want 0sec", postedDelay)
	}
}

func TestOpenTriggerDialogSurfacesFetchError(t *testing.T) {
	ctx := &context.ProgramContext{Config: config.GetDefaultConfig()}

	msg := OpenTriggerDialog(ctx, 80, ParamsFetchedMsg{Error: http.ErrNotSupported})()

	errMsg, ok := msg.(util.ErrorMsg)
	if !ok {
		t.Fatalf("dialog result = %T, want util.ErrorMsg", msg)
	}
	if errMsg.Title != "Failed to fetch build parameters" {
		t.Fatalf("error title = %q, want the parameter fetch title", errMsg.Title)
	}
}

// An unparameterised job gets a plain confirmation; a parameterised one gets
// the form, so the user never triggers a build without seeing its inputs.
func TestOpenTriggerDialogPicksDialogByParameterCount(t *testing.T) {
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins")
	ctx := &context.ProgramContext{Config: config.GetDefaultConfig()}
	job := demoJob(jenkins, "http://jenkins")

	for _, tt := range []struct {
		name   string
		params []gojenkins.ParameterDefinition
		want   string
	}{
		{name: "no parameters", want: "*confirm.ConfirmDialog"},
		{
			name:   "with parameters",
			params: []gojenkins.ParameterDefinition{{Name: "BRANCH", Type: "StringParameterDefinition"}},
			want:   "*buildparams.BuildParamsDialog",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			msg := OpenTriggerDialog(ctx, 80, ParamsFetchedMsg{Job: job, Params: tt.params})()

			open, ok := msg.(util.OpenDialogMsg)
			if !ok {
				t.Fatalf("dialog result = %T, want util.OpenDialogMsg", msg)
			}
			switch open.Dialog.(type) {
			case *confirm.ConfirmDialog:
				if tt.want != "*confirm.ConfirmDialog" {
					t.Fatalf("opened confirm dialog, want %s", tt.want)
				}
			case *buildparams.BuildParamsDialog:
				if tt.want != "*buildparams.BuildParamsDialog" {
					t.Fatalf("opened params dialog, want %s", tt.want)
				}
			default:
				t.Fatalf("opened %T, want %s", open.Dialog, tt.want)
			}
		})
	}
}

// My Runs rows name a job by URL only, so the job has to be resolved before
// its parameters can be read — and a failure there must surface as an error.
func TestFetchParamsByURLResolvesJob(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/job/demo/api/json":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "demo",
				"url":  "http://" + request.Host + "/job/demo/",
				"property": []map[string]any{{
					"parameterDefinitions": []map[string]any{{"name": "BRANCH", "type": "ChoiceParameterDefinition", "choices": []string{"main", "release"}}},
				}},
			})
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	ctx := &context.ProgramContext{
		Config:  config.GetDefaultConfig(),
		Jenkins: gojenkins.CreateJenkins(nil, server.URL),
	}

	var request ParamsRequest
	cmd := request.FetchByURL(ctx, server.URL+"/job/demo/")
	msg, ok := cmd().(ParamsFetchedMsg)
	if !ok {
		t.Fatalf("fetch result is not a ParamsFetchedMsg")
	}
	if msg.Error != nil {
		t.Fatalf("fetch error = %v, want nil", msg.Error)
	}
	if msg.requestID != request.id {
		t.Fatalf("request ID = %d, want %d", msg.requestID, request.id)
	}
	if msg.Job == nil || msg.Job.GetName() != "demo" {
		t.Fatalf("resolved job = %#v, want demo", msg.Job)
	}
	if len(msg.Params) != 1 || msg.Params[0].Name != "BRANCH" {
		t.Fatalf("fetched params = %#v, want a single BRANCH parameter", msg.Params)
	}
	if len(msg.Params[0].Choices) != 2 || msg.Params[0].Choices[1] != "release" {
		t.Fatalf("fetched choices = %#v, want main and release", msg.Params[0].Choices)
	}
}

func TestFetchParamsByURLReportsUnknownJob(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	ctx := &context.ProgramContext{
		Config:  config.GetDefaultConfig(),
		Jenkins: gojenkins.CreateJenkins(nil, server.URL),
	}

	var request ParamsRequest
	cmd := request.FetchByURL(ctx, server.URL+"/job/missing/")
	msg, ok := cmd().(ParamsFetchedMsg)
	if !ok {
		t.Fatalf("fetch result is not a ParamsFetchedMsg")
	}
	if msg.Error == nil {
		t.Fatal("fetching an unknown job returned no error")
	}
	if msg.requestID != request.id {
		t.Fatalf("request ID = %d, want %d", msg.requestID, request.id)
	}
	if msg.Job != nil {
		t.Fatalf("resolved job = %#v, want nil for a missing job", msg.Job)
	}
}

func TestFetchParamsRequiresJob(t *testing.T) {
	var request ParamsRequest
	cmd := request.Fetch(&context.ProgramContext{Config: config.GetDefaultConfig()}, nil)
	msg, ok := cmd().(ParamsFetchedMsg)
	if !ok {
		t.Fatalf("fetch result is not a ParamsFetchedMsg")
	}
	if msg.Error == nil || !strings.Contains(msg.Error.Error(), "no job") {
		t.Fatalf("fetch error = %v, want a missing-job error", msg.Error)
	}
	if msg.requestID != request.id {
		t.Fatalf("request ID = %d, want %d", msg.requestID, request.id)
	}
}

func TestParamsRequestRejectsSupersededAndInvalidatedResults(t *testing.T) {
	ctx := &context.ProgramContext{Config: config.GetDefaultConfig()}
	var request ParamsRequest

	oldCmd := request.Fetch(ctx, nil)
	currentCmd := request.Fetch(ctx, nil)
	oldMsg := oldCmd().(ParamsFetchedMsg)
	currentMsg := currentCmd().(ParamsFetchedMsg)

	if cmd := request.Handle(ctx, 80, oldMsg); cmd != nil {
		t.Fatal("superseded parameter result returned a command")
	}
	if cmd := request.Handle(ctx, 80, currentMsg); cmd == nil {
		t.Fatal("current parameter result returned no command")
	}

	invalidatedCmd := request.Fetch(ctx, nil)
	invalidatedMsg := invalidatedCmd().(ParamsFetchedMsg)
	request.Invalidate()
	if cmd := request.Handle(ctx, 80, invalidatedMsg); cmd != nil {
		t.Fatal("parameter result survived request invalidation")
	}
}
