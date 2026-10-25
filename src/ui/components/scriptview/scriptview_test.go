package scriptview

import (
	stdctx "context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

func TestReplayEditorUsesPrivateUniqueTemporaryFiles(t *testing.T) {
	model := &Model{ctx: &appcontext.ProgramContext{CacheDir: t.TempDir()}, content: "pipeline {}"}
	_, first, err := model.writeScriptAndEdit()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(first)
	_, second, err := model.writeScriptAndEdit()
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(second)
	if first == second {
		t.Fatal("temporary replay script paths collided")
	}
	info, err := os.Stat(first)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("temporary replay script mode = %o, want 600", info.Mode().Perm())
	}
}

func TestFetchScriptIsCancelledDuringCleanup(t *testing.T) {
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
	job := &gojenkins.Job{Jenkins: jenkins, Base: "/job/demo", Raw: &gojenkins.JobResponse{URL: server.URL + "/job/demo/"}}
	run := &gojenkins.PipelineRun{ID: "42", Base: "/job/demo/42", Job: job}
	model := &Model{ctx: &appcontext.ProgramContext{Config: cfg}, pipelineRun: run, job: job, loading: true}

	result := make(chan tea.Msg, 1)
	go func() { result <- model.fetchScript()() }()
	<-requestStarted
	_ = model.CleanUp()

	select {
	case <-requestCancelled:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not cancel the replay-script request")
	}
	select {
	case msg := <-result:
		fetched, ok := msg.(ScriptFetchedMsg)
		if !ok || !errors.Is(fetched.Error, stdctx.Canceled) {
			t.Fatalf("fetch result = %#v, want cancelled ScriptFetchedMsg", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled replay-script request did not return")
	}
}

func TestScriptViewDropsStaleLoadGeneration(t *testing.T) {
	model := &Model{requestGeneration: 2, loading: true}
	_, cmd := model.Update(ScriptFetchedMsg{Generation: 1, Content: "stale"})
	if cmd != nil {
		t.Fatal("stale result returned a command")
	}
	if model.content != "" || !model.loading {
		t.Fatal("stale result mutated the current page")
	}
}
