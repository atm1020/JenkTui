package buildaction

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/util"

	"github.com/atm1020/gojenkins"
)

func TestAPITokenTriggerCrumbPreflight(t *testing.T) {
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
				case r.URL.Path == "/job/demo/api/json":
					_ = json.NewEncoder(w).Encode(map[string]any{"name": "demo", "url": "http://" + r.Host + "/job/demo/"})
				case r.URL.Path == "/crumbIssuer/api/json":
					crumbs.Add(1)
					if crumbStatus == http.StatusNotFound {
						w.Header().Set("Content-Type", "text/html")
						w.WriteHeader(crumbStatus)
						_, _ = w.Write([]byte("<html><title>Not Found</title></html>"))
						return
					}
					_ = json.NewEncoder(w).Encode(map[string]string{"crumbRequestField": "Jenkins-Crumb", "crumb": "fixture-crumb"})
				case r.Method == http.MethodPost && r.URL.Path == "/job/demo/build":
					posts.Add(1)
					if crumbStatus == http.StatusOK && r.Header.Get("Jenkins-Crumb") != "fixture-crumb" {
						t.Error("issued crumb missing from trigger")
					}
					w.Header().Set("Location", "http://"+r.Host+"/queue/item/123/")
					w.WriteHeader(http.StatusCreated)
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
			ctx := &context.ProgramContext{Config: config.GetDefaultConfig(), PollManager: manager}

			msg := Trigger(ctx, demoJob(jenkins, server.URL), nil)()
			triggered, ok := msg.(util.RunTriggeredMsg)
			if !ok {
				t.Fatalf("trigger = %#v, want RunTriggeredMsg", msg)
			}
			if !triggered.Tracked || triggered.IntentID == "" || triggered.QueueID == nil || *triggered.QueueID != 123 {
				t.Fatalf("trigger = %#v, want tracked queue 123", triggered)
			}
			if crumbs.Load() != 1 || posts.Load() != 1 {
				t.Fatalf("crumbs=%d, posts=%d; want one each", crumbs.Load(), posts.Load())
			}
		})
	}
}
