package util

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"

	pocApi "github.com/atm1020/gojenkins"
)

// classServer answers every job's api/json with the given class, counting
// the requests it served so tests can assert on how much discovery a
// navigation actually costs.
func classServer(t *testing.T, class string) (*pocApi.Jenkins, *atomic.Int64) {
	t.Helper()
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.URL.Path != "/job/target/api/json" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"name":"target","_class":"` + class + `"}`))
	}))
	t.Cleanup(srv.Close)
	return pocApi.CreateJenkins(nil, srv.URL), &requests
}

func testProgram(api *pocApi.Jenkins) *appcontext.ProgramContext {
	return &appcontext.ProgramContext{Jenkins: api}
}

// Unknown URLs from My Runs and search must paint before classification, so
// the class the server would report cannot matter here. The destination then
// classifies and, for a container, replaces itself without leaving a throwaway
// page on Back; TestUnknownContainerRedirectReplacesHistory in
// job/unknown_navigation_test.go guards that through the real shell.
func TestHandleJobTarget_UnknownOpensBeforeClassification(t *testing.T) {
	t.Parallel()

	api, requests := classServer(t, "com.cloudbees.hudson.plugins.folder.Folder")
	target := jobref.Target{URL: api.Server + "/job/target/", Name: "caller label"}
	msg := HandleJobTarget(target, testProgram(api))()
	open, ok := msg.(OpenJobComponentMsg)
	if !ok || open.Job != nil || open.URL != target.URL || open.Name != target.Name {
		t.Fatalf("message = %#v, want URL-only destination preserving caller name", msg)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("requests before destination = %d, want 0", got)
	}
}

// TestHandleJobTarget_KnownContainerSkipsDiscovery is the point of carrying a
// typed target at all: a row that already told us it is a Folder or a
// MultiBranch Pipeline must not pay for a shallow api/json poll just to
// re-learn its own class before its page can open.
func TestHandleJobTarget_KnownContainerSkipsDiscovery(t *testing.T) {
	t.Parallel()

	for _, kind := range []jobref.Kind{jobref.KindFolder, jobref.KindMultiBranch} {
		t.Run(kind.FavoriteKind(), func(t *testing.T) {
			t.Parallel()

			api, requests := classServer(t, "should.not.be.requested")
			target := jobref.Target{
				URL:  api.Server + "/job/team/job/target/",
				Name: "target",
				Kind: kind,
			}

			msg := HandleJobTarget(target, testProgram(api))()

			m, ok := msg.(OpenJobFolderMsg)
			if !ok {
				t.Fatalf("got %T, want OpenJobFolderMsg", msg)
			}
			if m.Target != target {
				t.Errorf("Target = %+v, want %+v", m.Target, target)
			}
			if m.Job == nil {
				t.Fatal("OpenJobFolderMsg.Job is nil — the page has nothing to load children with")
			}
			if got, want := m.Job.Base, "/job/team/job/target"; got != want {
				t.Errorf("Job.Base = %q, want %q", got, want)
			}
			if got := requests.Load(); got != 0 {
				t.Errorf("known container cost %d requests, want 0 before its page opens", got)
			}
		})
	}
}

// Known pipelines open without discovery because the job page fetches its
// own job and runs from Init; polling here would repeat its GetJob request.
func TestHandleJobTarget_KnownPipelineSkipsDiscovery(t *testing.T) {
	t.Parallel()

	class := "org.jenkinsci.plugins.workflow.job.WorkflowJob"
	api, requests := classServer(t, class)
	target := jobref.Target{URL: api.Server + "/job/target/", Name: "target", Kind: jobref.KindPipeline}

	msg := HandleJobTarget(target, testProgram(api))()

	m, ok := msg.(OpenJobComponentMsg)
	if !ok {
		t.Fatalf("got %T, want OpenJobComponentMsg", msg)
	}
	if m.Job != nil || m.URL != target.URL {
		t.Errorf("message = %+v, want URL-only target %q", m, target.URL)
	}
	if got := requests.Load(); got != 0 {
		t.Errorf("known pipeline cost %d requests, want 0 before paint", got)
	}
}

func TestHandleJobTarget_NonJobURLWarnsWithoutDiscovery(t *testing.T) {
	t.Parallel()

	api, requests := classServer(t, "should.not.be.requested")
	tests := []struct {
		name string
		url  string
		item string
		want string
	}{
		{
			name: "node",
			url:  api.Server + "/computer/(built-in)/",
			item: "Built-In Node",
			want: `Navigation to "Built-In Node" is not supported`,
		},
		{
			name: "view",
			url:  api.Server + "/view/all/",
			item: "All",
			want: `Navigation to "All" is not supported`,
		},
		{
			name: "empty_name_falls_back_to_display",
			url:  "/manage",
			want: `Navigation to "manage" is not supported`,
		},
		{
			name: "empty_target_falls_back_to_generic",
			want: "Navigation to this item is not supported",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			msg := HandleJobTarget(jobref.Target{URL: tt.url, Name: tt.item}, testProgram(api))()
			got, ok := msg.(FooterNotiMsg)
			if !ok {
				t.Fatalf("got %T, want FooterNotiMsg", msg)
			}
			if got.Type != NotiWarning {
				t.Errorf("Type = %v, want NotiWarning", got.Type)
			}
			if got.Message != tt.want {
				t.Errorf("Message = %q, want %q", got.Message, tt.want)
			}
			if n := requests.Load(); n != 0 {
				t.Errorf("non-job URL cost %d requests, want 0", n)
			}
		})
	}
}

func TestUnsupportedNavigationMessage_PreservesCallerName(t *testing.T) {
	t.Parallel()

	got := UnsupportedNavigationMessage(jobref.Target{URL: "/job/target/", Name: "quick-health-check"})
	if want := `Navigation to "quick-health-check" is not supported`; got != want {
		t.Errorf("Message = %q, want %q", got, want)
	}
}

func TestUnsupportedNavigationMessage_Fallbacks(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		target jobref.Target
		want   string
	}{
		{jobref.Target{URL: "/job/team/job/target/", Name: "  "}, `Navigation to "team \ueab6 target" is not supported`},
		{jobref.Target{}, "Navigation to this item is not supported"},
	} {
		if got := UnsupportedNavigationMessage(tt.target); got != tt.want {
			t.Errorf("Message = %q, want %q", got, tt.want)
		}
	}
}

func TestTargetFromJob_CarriesURLNameAndKind(t *testing.T) {
	t.Parallel()

	api, _ := classServer(t, "com.cloudbees.hudson.plugins.folder.Folder")
	job, err := api.GetJob(t.Context(), "target")
	if err != nil {
		t.Fatalf("GetJob: %v", err)
	}

	got := TargetFromJob(api, job)
	if got.Kind != jobref.KindFolder {
		t.Errorf("Kind = %v, want KindFolder", got.Kind)
	}
	if got.Name != "target" {
		t.Errorf("Name = %q, want %q", got.Name, "target")
	}
}
