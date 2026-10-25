package joblog

import (
	stdctx "context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

func TestModelFromURLDownloadsStoredRunLog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/job/folder/job/demo/42/consoleText/" {
			http.NotFound(w, request)
			return
		}
		_, _ = w.Write([]byte("line one\nline two\n"))
	}))
	defer server.Close()

	cfg := config.GetDefaultConfig()
	ctx := &appcontext.ProgramContext{
		Config: cfg, CacheDir: t.TempDir(), Jenkins: gojenkins.CreateJenkins(nil, server.URL),
	}
	model := NewModelFromURL(ctx, 100, 30, server.URL+"/job/folder/job/demo/", "42")
	t.Cleanup(func() { _ = model.CleanUp() })

	complete, ok := model.downloadLog()().(DownloadCompleteMsg)
	if !ok {
		t.Fatal("stored log download did not return DownloadCompleteMsg")
	}
	if complete.Error != nil {
		t.Fatalf("stored log download error = %v", complete.Error)
	}
	if complete.LineCount != 2 {
		t.Fatalf("stored log line count = %d, want 2", complete.LineCount)
	}
}

func TestDownloadLogIsCancelledDuringCleanup(t *testing.T) {
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
	run := testPipelineRun(server.URL)
	model := NewModel(&appcontext.ProgramContext{Config: cfg, CacheDir: t.TempDir()}, 100, 30, run)

	result := make(chan tea.Msg, 1)
	go func() { result <- model.downloadLog()() }()
	<-requestStarted
	_ = model.CleanUp()

	select {
	case <-requestCancelled:
	case <-time.After(time.Second):
		t.Fatal("cleanup did not cancel the log download")
	}
	select {
	case msg := <-result:
		complete, ok := msg.(DownloadCompleteMsg)
		if !ok || !errors.Is(complete.Error, stdctx.Canceled) {
			t.Fatalf("download result = %#v, want cancelled DownloadCompleteMsg", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("cancelled log download did not return")
	}
}

func TestDownloaderHonorsCancelledContext(t *testing.T) {
	requestReachedServer := make(chan struct{}, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestReachedServer <- struct{}{}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := stdctx.WithCancel(stdctx.Background())
	cancel()
	_, _, err := NewDownloader(testPipelineRun(server.URL), t.TempDir()).Download(ctx)
	if !errors.Is(err, stdctx.Canceled) {
		t.Fatalf("Download error = %v, want context.Canceled", err)
	}
	select {
	case <-requestReachedServer:
		t.Fatal("cancelled request reached the server")
	default:
	}
}

func TestJobLogDropsStaleDownloadGeneration(t *testing.T) {
	model := &Model{downloadGeneration: 2, downloading: true}
	_, cmd := model.Update(DownloadCompleteMsg{Generation: 1, FilePath: "stale"})
	if cmd != nil {
		t.Fatal("stale result returned a command")
	}
	if model.viewer != nil || !model.downloading {
		t.Fatal("stale result mutated the current page")
	}
}

func testPipelineRun(serverURL string) *gojenkins.PipelineRun {
	jenkins := gojenkins.CreateJenkins(nil, serverURL)
	job := &gojenkins.Job{Jenkins: jenkins, Base: "/job/demo", Raw: &gojenkins.JobResponse{URL: serverURL + "/job/demo/"}}
	return &gojenkins.PipelineRun{ID: "42", Base: "/job/demo/42", Job: job}
}
