package buildaction

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/confirm"
	"github.com/atm1020/jenktui/src/ui/util"

	"github.com/atm1020/gojenkins"
)

// abortServer serves a build that reports itself running, and records whether
// the stop endpoint was reached.
func abortServer(t *testing.T, running bool, stopStatus int) (*httptest.Server, *bool) {
	t.Helper()
	stopped := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		// A real Jenkins job URL ends in a slash, which gojenkins joins into a
		// doubled separator ("/job/demo//42"); collapse it so the routing below
		// reads as the paths Jenkins actually exposes.
		path := strings.ReplaceAll(request.URL.Path, "//", "/")
		switch {
		case path == "/job/demo/api/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "demo", "url": "http://" + request.Host + "/job/demo/"})
		case path == "/job/demo/42/api/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"number": 42, "building": running})
		case strings.HasPrefix(path, "/crumbIssuer/api/json"):
			_ = json.NewEncoder(w).Encode(map[string]string{})
		case request.Method == http.MethodPost && path == "/job/demo/42/stop":
			stopped = true
			w.WriteHeader(stopStatus)
		default:
			http.NotFound(w, request)
		}
	}))
	t.Cleanup(server.Close)
	return server, &stopped
}

func abortContext(server *httptest.Server) (*context.ProgramContext, *gojenkins.Job) {
	jenkins := gojenkins.CreateJenkins(nil, server.URL)
	job := &gojenkins.Job{
		Jenkins: jenkins,
		Base:    "/job/demo",
		Raw:     &gojenkins.JobResponse{Name: "demo", URL: server.URL + "/job/demo/"},
	}
	return &context.ProgramContext{Config: config.GetDefaultConfig(), Jenkins: jenkins}, job
}

func TestAbortStopsRunningBuild(t *testing.T) {
	server, stopped := abortServer(t, true, http.StatusOK)
	ctx, job := abortContext(server)

	msg := Abort(ctx, job, "42")()

	noti, ok := msg.(util.FooterNotiMsg)
	if !ok {
		t.Fatalf("abort result = %#v, want util.FooterNotiMsg", msg)
	}
	if !strings.Contains(noti.Message, "#42") {
		t.Fatalf("abort message = %q, want it to name build #42", noti.Message)
	}
	if !*stopped {
		t.Fatal("abort never reached the Jenkins stop endpoint")
	}
}

// Jenkins reports a refused stop as a plain false, which must not be reported
// to the user as a successful cancellation.
func TestAbortReportsRefusedStop(t *testing.T) {
	server, _ := abortServer(t, true, http.StatusInternalServerError)
	ctx, job := abortContext(server)

	msg := Abort(ctx, job, "42")()

	errMsg, ok := msg.(util.ErrorMsg)
	if !ok {
		t.Fatalf("abort result = %#v, want util.ErrorMsg", msg)
	}
	if errMsg.Title != "Cancel failed" {
		t.Fatalf("error title = %q, want \"Cancel failed\"", errMsg.Title)
	}
}

func TestAbortRejectsUnparsableBuildID(t *testing.T) {
	server, stopped := abortServer(t, true, http.StatusOK)
	ctx, job := abortContext(server)

	msg := Abort(ctx, job, "not-a-number")()

	errMsg, ok := msg.(util.ErrorMsg)
	if !ok {
		t.Fatalf("abort result = %#v, want util.ErrorMsg", msg)
	}
	if errMsg.Title != "Failed to parse build ID" {
		t.Fatalf("error title = %q, want the parse failure title", errMsg.Title)
	}
	if *stopped {
		t.Fatal("abort posted a stop for an unparsable build ID")
	}
}

func TestAbortRequiresJob(t *testing.T) {
	msg := Abort(&context.ProgramContext{Config: config.GetDefaultConfig()}, nil, "42")()

	errMsg, ok := msg.(util.ErrorMsg)
	if !ok {
		t.Fatalf("abort result = %#v, want util.ErrorMsg", msg)
	}
	if errMsg.Error == nil || errMsg.Error.Error() != "job is not available" {
		t.Fatalf("abort error = %#v, want unavailable job", errMsg)
	}
}

// My Runs rows name a job by URL only, so the job is resolved on confirmation.
func TestAbortByURLResolvesJob(t *testing.T) {
	server, stopped := abortServer(t, true, http.StatusOK)
	ctx, _ := abortContext(server)

	msg := AbortByURL(ctx, server.URL+"/job/demo/", "42")()

	if _, ok := msg.(util.FooterNotiMsg); !ok {
		t.Fatalf("abort result = %#v, want util.FooterNotiMsg", msg)
	}
	if !*stopped {
		t.Fatal("abort never reached the Jenkins stop endpoint")
	}
}

func TestAbortByURLReportsUnknownJob(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	ctx := &context.ProgramContext{
		Config:  config.GetDefaultConfig(),
		Jenkins: gojenkins.CreateJenkins(nil, server.URL),
	}

	msg := AbortByURL(ctx, server.URL+"/job/missing/", "42")()

	errMsg, ok := msg.(util.ErrorMsg)
	if !ok {
		t.Fatalf("abort result = %#v, want util.ErrorMsg", msg)
	}
	if errMsg.Title != "Failed to get job" {
		t.Fatalf("error title = %q, want the job lookup title", errMsg.Title)
	}
}

// Aborting is destructive, so it must always go through a confirmation rather
// than stopping the build on the keypress itself.
func TestConfirmAbortAsksBeforeStopping(t *testing.T) {
	server, stopped := abortServer(t, true, http.StatusOK)
	ctx, job := abortContext(server)

	msg := ConfirmAbort(ctx, job, "42", false)()

	open, ok := msg.(util.OpenDialogMsg)
	if !ok {
		t.Fatalf("confirm result = %#v, want util.OpenDialogMsg", msg)
	}
	if _, ok := open.Dialog.(*confirm.ConfirmDialog); !ok {
		t.Fatalf("opened dialog = %T, want a confirm dialog", open.Dialog)
	}
	if *stopped {
		t.Fatal("opening the confirmation already stopped the build")
	}
}

func TestConfirmAbortWarnsForFinishedBuild(t *testing.T) {
	server, stopped := abortServer(t, false, http.StatusOK)
	ctx, job := abortContext(server)

	msg := ConfirmAbort(ctx, job, "42", true)()

	noti, ok := msg.(util.FooterNotiMsg)
	if !ok {
		t.Fatalf("finished abort result = %#v, want util.FooterNotiMsg", msg)
	}
	if noti.Type != util.NotiWarning || !strings.Contains(noti.Message, "#42") {
		t.Fatalf("finished abort warning = %#v, want warning naming build #42", noti)
	}
	if *stopped {
		t.Fatal("finished build reached the Jenkins stop endpoint")
	}
}
