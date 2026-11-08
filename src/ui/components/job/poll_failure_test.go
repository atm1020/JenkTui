package job

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/poll"
)

func TestPersistentPollFailureShowsFeedback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "wfapi/runs") {
			http.Error(w, "permission denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"name":"demo","url":"http://%s/job/demo/","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob"}`, r.Host)
	}))
	defer server.Close()
	api := gojenkins.CreateJenkins(nil, server.URL)
	manager := poll.NewManager(api, nil, poll.Config{PollInterval: time.Hour, RetryBase: time.Hour, RetryMax: time.Hour})
	manager.Start()
	defer manager.Stop()
	model := NewModelFromUrl(&appcontext.ProgramContext{Config: config.GetDefaultConfig(), Jenkins: api, PollManager: manager}, 160, 45, server.URL+"/job/demo/")
	defer func() { _ = model.CleanUp() }()
	init := model.Init()().(tea.BatchMsg)
	for _, cmd := range init {
		if cmd != nil {
			if msg, ok := cmd().(JobLoadedMsg); ok {
				model.Update(msg)
			}
		}
	}
	select {
	case delivery := <-manager.Results():
		if len(delivery.Batch.Failures) != 1 {
			t.Fatalf("delivery=%+v", delivery.Batch)
		}
		model.ApplyPollFailure(delivery.Batch.Failures[0])
	case <-time.After(time.Second):
		t.Fatal("no failure delivered")
	}
	view := model.View()
	if model.loading || strings.Contains(view, "Loading") ||
		!strings.Contains(view, "Could not load pipeline runs: demo") ||
		!strings.Contains(view, "403") || !strings.Contains(view, "Back returns") {
		t.Fatalf("failure did not replace spinner with actionable feedback:\n%s", view)
	}
}
