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

// TestHandleJobTarget_ClassifiesUnknownBeforeOpening covers the bug where
// opening a job by URL alone (global search, My Runs — neither of which
// carries a resolved class) must classify the target before deciding which
// view to open. An earlier version of this flow in ui.go skipped
// classification and always opened the pipeline job view, relying on that
// view's Init() to notice the mismatch and self-redirect — which left a
// throwaway entry in the router's back stack that reappeared (and
// re-redirected) on every later Back press, making Back look like it just
// reloaded the current page instead of going back.
func TestHandleJobTarget_ClassifiesUnknownBeforeOpening(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		class   string
		wantMsg string // "folder" or "component"
	}{
		{
			name:    "multibranch_pipeline_opens_as_folder",
			class:   "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject",
			wantMsg: "folder",
		},
		{
			name:    "plain_folder_opens_as_folder",
			class:   "com.cloudbees.hudson.plugins.folder.Folder",
			wantMsg: "folder",
		},
		{
			name:    "organization_folder_opens_as_folder",
			class:   "jenkins.branch.OrganizationFolder",
			wantMsg: "folder",
		},
		{
			name:    "pipeline_job_opens_as_component",
			class:   "org.jenkinsci.plugins.workflow.job.WorkflowJob",
			wantMsg: "component",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			api, requests := classServer(t, tt.class)
			cmd := HandleJobTarget(jobref.Target{URL: api.Server + "/job/target/"}, testProgram(api))
			if cmd == nil {
				t.Fatal("HandleJobTarget returned a nil cmd")
			}

			msg := cmd()
			switch m := msg.(type) {
			case OpenJobFolderMsg:
				if tt.wantMsg != "folder" {
					t.Fatalf("got OpenJobFolderMsg, want OpenJobComponentMsg (class %q)", tt.class)
				}
				if m.Job.GetDetails().Class != tt.class {
					t.Errorf("Job.Class = %q, want %q", m.Job.GetDetails().Class, tt.class)
				}
				if m.Target.Kind == jobref.KindUnknown {
					t.Error("Target.Kind should be resolved once the class is known")
				}
			case OpenJobComponentMsg:
				if tt.wantMsg != "component" {
					t.Fatalf("got OpenJobComponentMsg, want OpenJobFolderMsg (class %q)", tt.class)
				}
				if m.Job.GetDetails().Class != tt.class {
					t.Errorf("Job.Class = %q, want %q", m.Job.GetDetails().Class, tt.class)
				}
			case ErrorMsg:
				t.Fatalf("HandleJobTarget returned ErrorMsg: %v", m.Error)
			default:
				t.Fatalf("HandleJobTarget returned unexpected msg type %T", msg)
			}

			if got := requests.Load(); got != 1 {
				t.Errorf("unknown target cost %d requests, want 1 discovery request", got)
			}
		})
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

// TestHandleJobTarget_KnownPipelineLoadsJob keeps known pipelines on the
// ordinary path: they still need their polled job to open the run list.
func TestHandleJobTarget_KnownPipelineLoadsJob(t *testing.T) {
	t.Parallel()

	class := "org.jenkinsci.plugins.workflow.job.WorkflowJob"
	api, requests := classServer(t, class)
	target := jobref.Target{URL: api.Server + "/job/target/", Name: "target", Kind: jobref.KindPipeline}

	msg := HandleJobTarget(target, testProgram(api))()

	m, ok := msg.(OpenJobComponentMsg)
	if !ok {
		t.Fatalf("got %T, want OpenJobComponentMsg", msg)
	}
	if m.Job.GetDetails().Class != class {
		t.Errorf("Job.Class = %q, want %q", m.Job.GetDetails().Class, class)
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("known pipeline cost %d requests, want 1", got)
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

func TestHandleJobTarget_UnsupportedJobKindWarns(t *testing.T) {
	t.Parallel()

	api, requests := classServer(t, "hudson.model.FreeStyleProject")
	msg := HandleJobTarget(jobref.Target{
		URL:  api.Server + "/job/target/",
		Name: "quick-health-check",
	}, testProgram(api))()

	got, ok := msg.(FooterNotiMsg)
	if !ok {
		t.Fatalf("got %T, want FooterNotiMsg", msg)
	}
	if got.Type != NotiWarning {
		t.Errorf("Type = %v, want NotiWarning", got.Type)
	}
	if want := `Navigation to "quick-health-check" is not supported`; got.Message != want {
		t.Errorf("Message = %q, want %q", got.Message, want)
	}
	if n := requests.Load(); n != 1 {
		t.Errorf("unsupported job cost %d requests, want 1 discovery request", n)
	}
}

func TestHandleJobTarget_UnsupportedJobKindUsesResolvedName(t *testing.T) {
	t.Parallel()

	api, _ := classServer(t, "hudson.model.FreeStyleProject")
	msg := HandleJobTarget(jobref.Target{URL: api.Server + "/job/target/"}, testProgram(api))()

	got, ok := msg.(FooterNotiMsg)
	if !ok {
		t.Fatalf("got %T, want FooterNotiMsg", msg)
	}
	if want := `Navigation to "target" is not supported`; got.Message != want {
		t.Errorf("Message = %q, want %q", got.Message, want)
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
