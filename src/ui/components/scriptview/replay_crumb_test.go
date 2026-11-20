package scriptview

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/util"

	"github.com/atm1020/gojenkins"
)

func TestAPITokenReplayCrumbPreflight(t *testing.T) {
	for _, crumbStatus := range []int{http.StatusOK, http.StatusNotFound} {
		t.Run(http.StatusText(crumbStatus), func(t *testing.T) {
			var posts, crumbs atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				username, token, ok := r.BasicAuth()
				if !ok || username != "fixture-user" || token != "fixture-api-token" {
					t.Error("missing API-token basic auth")
					http.Error(w, "unauthorized", http.StatusUnauthorized)
					return
				}
				switch {
				case r.URL.Path == "/crumbIssuer/api/json":
					crumbs.Add(1)
					if crumbStatus == http.StatusNotFound {
						w.Header().Set("Content-Type", "text/html")
						w.WriteHeader(crumbStatus)
						_, _ = w.Write([]byte("<html><title>Not Found</title></html>"))
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"crumbRequestField": "Jenkins-Crumb", "crumb": "fixture-crumb"})
				case r.Method == http.MethodPost && r.URL.Path == "/job/demo/42/replay/run":
					posts.Add(1)
					if crumbStatus == http.StatusOK && r.Header.Get("Jenkins-Crumb") != "fixture-crumb" {
						t.Error("issued crumb missing from replay")
					}
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte("<html><title>Build started</title></html>"))
				default:
					t.Errorf("unexpected request: %s %s", r.Method, r.URL.Path)
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			runStore := store.New(t.TempDir())
			defer runStore.Close()
			jenkins := gojenkins.CreateJenkins(nil, server.URL, "fixture-user", "fixture-api-token")
			manager := poll.NewManager(jenkins, runStore, poll.Config{})
			defer manager.Stop()
			job := &gojenkins.Job{Jenkins: jenkins, Base: "/job/demo", Raw: &gojenkins.JobResponse{Name: "demo", URL: server.URL + "/job/demo/"}}
			model := &Model{
				ctx:         &appcontext.ProgramContext{Config: config.GetDefaultConfig(), PollManager: manager},
				job:         job,
				pipelineRun: &gojenkins.PipelineRun{Job: job, Base: "/job/demo/42", ID: "42"},
				content:     "echo 'ok'",
			}

			msg := model.runScript()()
			triggered, ok := msg.(util.RunTriggeredMsg)
			if !ok {
				t.Fatalf("replay = %#v, want RunTriggeredMsg", msg)
			}
			if !triggered.Tracked || triggered.IntentID == "" || triggered.QueueID != nil {
				t.Fatalf("replay = %#v, want tracked queue-less intent", triggered)
			}
			if crumbs.Load() != 1 || posts.Load() != 1 {
				t.Fatalf("crumbs=%d, posts=%d; want one each", crumbs.Load(), posts.Load())
			}
		})
	}
}
