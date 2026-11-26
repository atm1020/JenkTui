package poll

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/atm1020/gojenkins"
	"github.com/atm1020/jenktui/src/buildstatus"
)

func TestJenkinsSourceFetchesTypedWindowWithCursor(t *testing.T) {
	var since string
	var fullStages string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/demo/api/json":
			t.Errorf("unexpected job discovery request %s", r.URL.RequestURI())
			http.NotFound(w, r)
		case "/job/demo/wfapi/runs/api/json":
			since = r.URL.Query().Get("since")
			fullStages = r.URL.Query().Get("fullStages")
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": "42", "name": "demo #42", "status": "PAUSED_PENDING_INPUT",
				"startTimeMillis": int64(1000), "durationMillis": int64(500),
				"stages": []map[string]any{{"id": "7", "name": "Approval", "status": "PAUSED_PENDING_INPUT", "execNode": "linux-1"}},
			}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := jenkinsSource{jenkins: gojenkins.CreateJenkins(nil, server.URL)}
	observation, err := source.fetchWindow(context.Background(), server.URL+"/job/demo/", 41)
	if err != nil {
		t.Fatal(err)
	}
	if since != "#41" {
		t.Fatalf("since = %q, want #41", since)
	}
	if fullStages != "" {
		t.Fatalf("fullStages = %q, want omitted for classic stage view", fullStages)
	}
	if len(observation.Runs) != 1 || observation.Runs[0].BuildNumber != 42 || observation.Runs[0].Activity != buildstatus.ActivityPaused {
		t.Fatalf("observation = %#v", observation)
	}
	if got := observation.Runs[0].Stages[0].ExecNode; got != "linux-1" {
		t.Fatalf("stage executor = %q, want linux-1", got)
	}
}

func TestJenkinsSourceConfirmCompletionProjectsBuildingField(t *testing.T) {
	var tree string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/demo/wfapi/runs/api/json":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id": "42", "name": "demo #42", "status": "NOT_EXECUTED",
				"stages": []map[string]any{},
			}})
		case "/job/demo/42/api/json":
			tree = r.URL.Query().Get("tree")
			_, _ = w.Write([]byte(`{"building":false}`))
		default:
			t.Errorf("unexpected request %s", r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	source := jenkinsSource{jenkins: gojenkins.CreateJenkins(nil, server.URL)}
	observation, err := source.fetchWindow(context.Background(), server.URL+"/job/demo/", 0)
	if err != nil {
		t.Fatal(err)
	}
	if tree != "building" {
		t.Fatalf("confirmCompletion tree = %q, want building", tree)
	}
	if len(observation.Runs) != 1 || observation.Runs[0].Activity != buildstatus.ActivityFinished {
		t.Fatalf("observation = %#v, want finished activity", observation)
	}
}

func TestJenkinsSourceRejectsUnsupportedBuildIDAsMalformed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/job/demo/wfapi/runs/api/json":
			_, _ = w.Write([]byte(`[{"id":"branch-main","status":"SUCCESS","stages":[]}]`))
		default:
			t.Errorf("unexpected request %s", r.URL.RequestURI())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	_, err := (jenkinsSource{jenkins: gojenkins.CreateJenkins(nil, server.URL)}).fetchWindow(context.Background(), server.URL+"/job/demo/", 0)
	var sourceErr *SourceError
	if !errors.As(err, &sourceErr) || sourceErr.Kind != SourceErrorMalformed {
		t.Fatalf("error = %T %v, want malformed SourceError", err, err)
	}
}

func TestJenkinsSourceUsesOneCanceledContextForExactRequest(t *testing.T) {
	canceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/job/demo/api/json" {
			t.Errorf("unexpected job discovery request %s", r.URL.RequestURI())
			http.NotFound(w, r)
			return
		}
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (jenkinsSource{jenkins: gojenkins.CreateJenkins(nil, server.URL)}).fetchExact(ctx, Target{Kind: TargetExactRun, JobURL: server.URL + "/job/demo/", BuildNumber: 42})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
}
