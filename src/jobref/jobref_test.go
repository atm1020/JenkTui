package jobref_test

import (
	"reflect"
	"testing"

	"github.com/atm1020/jenktui/src/jobref"
)

func TestToAbsolute(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		server string
		url    string
		want   string
	}{
		{
			name:   "relative_path_gets_server_prefix",
			server: "http://jenkins:8080",
			url:    "/job/foo/",
			want:   "http://jenkins:8080/job/foo/",
		},
		{
			name:   "absolute_http_passes_through",
			server: "http://jenkins:8080",
			url:    "http://other-host/job/foo/",
			want:   "http://other-host/job/foo/",
		},
		{
			name:   "absolute_https_passes_through",
			server: "http://jenkins:8080",
			url:    "https://secure-jenkins/job/foo/",
			want:   "https://secure-jenkins/job/foo/",
		},
		{
			name:   "empty_server_returns_url_unchanged",
			server: "",
			url:    "/job/foo/",
			want:   "/job/foo/",
		},
		{
			name:   "both_empty",
			server: "",
			url:    "",
			want:   "",
		},
		{
			name:   "deep_folder_path",
			server: "http://jenkins:8080",
			url:    "/job/team/job/svc/job/build/",
			want:   "http://jenkins:8080/job/team/job/svc/job/build/",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := jobref.ToAbsolute(tt.server, tt.url)
			if got != tt.want {
				t.Errorf("ToAbsolute(%q, %q) = %q, want %q", tt.server, tt.url, got, tt.want)
			}
		})
	}
}

func TestBuildURL(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		jobURL  string
		buildID string
		want    string
	}{
		{name: "trailing slash", jobURL: "http://jenkins/job/demo/", buildID: "42", want: "http://jenkins/job/demo/42"},
		{name: "no trailing slash", jobURL: "http://jenkins/job/demo", buildID: "42", want: "http://jenkins/job/demo/42"},
		{name: "leading build slash", jobURL: "http://jenkins/job/demo/", buildID: "/42", want: "http://jenkins/job/demo/42"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := jobref.BuildURL(tt.jobURL, tt.buildID); got != tt.want {
				t.Errorf("BuildURL(%q, %q) = %q, want %q", tt.jobURL, tt.buildID, got, tt.want)
			}
		})
	}
}

func TestToDisplayName(t *testing.T) {
	t.Parallel()
	// Nerd Font glyph rendered as a chevron in the TUI; mirrors the
	// jobNameSep constant in jobref.go so a typo there fails this test.
	const sep = "  "

	tests := []struct {
		name string
		url  string
		want string
	}{
		{
			name: "absolute_single_job",
			url:  "http://jenkins:8080/job/foo/",
			want: "foo",
		},
		{
			name: "absolute_one_folder",
			url:  "http://jenkins/job/folder/job/leaf/",
			want: "folder" + sep + "leaf",
		},
		{
			name: "absolute_deep_folders",
			url:  "http://jenkins/job/Ops/job/Monitoring/job/run-script/",
			want: "Ops" + sep + "Monitoring" + sep + "run-script",
		},
		{
			name: "https_scheme_handled",
			url:  "https://jenkins/job/foo/",
			want: "foo",
		},
		{
			name: "relative_path",
			url:  "/job/foo/",
			want: "foo",
		},
		{
			name: "no_trailing_slash",
			url:  "http://jenkins/job/folder/job/leaf",
			want: "folder" + sep + "leaf",
		},
		{
			name: "empty_url",
			url:  "",
			want: "",
		},
		{
			// Regression: a multibranch pipeline branch named "merge-queue/PR-58"
			// is reported by Jenkins as the single percent-encoded path segment
			// "merge-queue%2FPR-58" so it survives as one job under the multibranch
			// project. The display name must decode it back to the readable slash.
			name: "encoded_slash_in_branch_name",
			url:  "http://jenkins/job/merge-queue-test/job/merge-queue%2FPR-58/",
			want: "merge-queue-test" + sep + "merge-queue/PR-58",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := jobref.ToDisplayName(tt.url)
			if got != tt.want {
				t.Errorf("ToDisplayName(%q) = %q, want %q", tt.url, got, tt.want)
			}
		})
	}
}

// TestDecodeName covers a multibranch pipeline branch whose name contains
// "/" (e.g. a merge-queue PR branch), which Jenkins reports as a single
// percent-encoded job name so it doesn't collide with the URL path separator.
func TestDecodeName(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"encoded_slash", "merge-queue%2FPR-48", "merge-queue/PR-48"},
		{"plain_name_unchanged", "master", "master"},
		{"invalid_encoding_falls_back", "100%done", "100%done"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := jobref.DecodeName(tt.in); got != tt.want {
				t.Errorf("DecodeName(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestIsJobURL(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		url  string
		want bool
	}{
		{name: "relative_job", url: "/job/pipeline/", want: true},
		{name: "nested_folder_job", url: "/job/team/job/pipeline/", want: true},
		{name: "absolute_with_context_path", url: "https://jenkins.example/jenkins/job/team/", want: true},
		{name: "encoded_branch_name", url: "/job/repo/job/feature%2Fsearch/", want: true},
		{name: "node", url: "/computer/(built-in)/", want: false},
		{name: "view", url: "/view/all/", want: false},
		{name: "user", url: "/user/admin", want: false},
		{name: "job_action", url: "/job/pipeline/configure", want: false},
		{name: "malformed", url: "%", want: false},
		{name: "empty", url: "", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := jobref.IsJobURL(tt.url); got != tt.want {
				t.Errorf("IsJobURL(%q) = %v, want %v", tt.url, got, tt.want)
			}
		})
	}
}

func TestToIDAndParents(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name        string
		url         string
		wantID      string
		wantParents []string
	}{
		{
			name:   "absolute_url_single_job",
			url:    "http://jenkins:8080/job/foo/",
			wantID: "foo",
		},
		{
			name:        "absolute_url_with_folder",
			url:         "http://jenkins:8080/job/folder/job/leaf/",
			wantID:      "leaf",
			wantParents: []string{"folder"},
		},
		{
			name:        "absolute_url_deep_folders",
			url:         "http://jenkins:8080/job/a/job/b/job/c/",
			wantID:      "c",
			wantParents: []string{"a", "b"},
		},
		{
			// Regression: an earlier version split the URL without stripping
			// the host first, returning ("a", ["http:", "127.0.0.1:1"]) and
			// causing the poller to construct malformed Jenkins API URLs like
			// http://127.0.0.1:1/job/http:/job/127.0.0.1:1/job/a/api/json.
			name:   "absolute_url_with_port_and_ip",
			url:    "http://127.0.0.1:1/job/a/",
			wantID: "a",
		},
		{
			name:   "https_scheme_handled",
			url:    "https://jenkins/job/foo/",
			wantID: "foo",
		},
		{
			name:   "relative_path_single_job",
			url:    "/job/foo/",
			wantID: "foo",
		},
		{
			name:        "relative_path_with_folder",
			url:         "/job/folder/job/leaf/",
			wantID:      "leaf",
			wantParents: []string{"folder"},
		},
		{
			name:   "no_leading_slash",
			url:    "job/foo/",
			wantID: "foo",
		},
		{
			name:        "no_trailing_slash",
			url:         "http://jenkins/job/folder/job/leaf",
			wantID:      "leaf",
			wantParents: []string{"folder"},
		},
		{
			name:   "empty_url",
			url:    "",
			wantID: "",
		},
		{
			name:   "only_slashes",
			url:    "///",
			wantID: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			gotID, gotParents := jobref.ToIDAndParents(tt.url)
			if gotID != tt.wantID {
				t.Errorf("id = %q, want %q", gotID, tt.wantID)
			}
			if !reflect.DeepEqual(gotParents, tt.wantParents) {
				t.Errorf("parents = %#v, want %#v", gotParents, tt.wantParents)
			}
		})
	}
}

func TestClassPredicates(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name         string
		class        string
		wantFolder   bool
		wantMulti    bool
		wantPipeline bool
	}{
		{
			name:       "folder",
			class:      "com.cloudbees.hudson.plugins.folder.Folder",
			wantFolder: true,
		},
		{
			name:      "multibranch_pipeline",
			class:     "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject",
			wantMulti: true,
		},
		{
			name:         "pipeline_job",
			class:        "org.jenkinsci.plugins.workflow.job.WorkflowJob",
			wantPipeline: true,
		},
		{
			name:         "multibranch_branch_is_a_plain_pipeline",
			class:        "org.jenkinsci.plugins.workflow.job.WorkflowJob",
			wantPipeline: true,
		},
		{
			name:  "freestyle_matches_none",
			class: "hudson.model.FreeStyleProject",
		},
		{
			name:  "empty_matches_none",
			class: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := jobref.IsFolder(tt.class); got != tt.wantFolder {
				t.Errorf("IsFolder(%q) = %v, want %v", tt.class, got, tt.wantFolder)
			}
			if got := jobref.IsMultiBranch(tt.class); got != tt.wantMulti {
				t.Errorf("IsMultiBranch(%q) = %v, want %v", tt.class, got, tt.wantMulti)
			}
			if got := jobref.IsPipeline(tt.class); got != tt.wantPipeline {
				t.Errorf("IsPipeline(%q) = %v, want %v", tt.class, got, tt.wantPipeline)
			}
		})
	}
}

// FuzzToIDAndParents asserts the parser never panics and never emits "job"
// or empty strings inside the result, regardless of input shape. These are
// the structural invariants every caller of jobref.ToIDAndParents relies on
// when feeding (id, parents) into gojenkins.GetJob.
func FuzzToIDAndParents(f *testing.F) {
	f.Add("http://jenkins/job/foo/")
	f.Add("/job/folder/job/leaf/")
	f.Add("")
	f.Add("///")
	f.Add("http://127.0.0.1:1/job/a/")
	f.Add("https://jenkins/job/folder/job/leaf")
	f.Add("job/foo/")

	f.Fuzz(func(t *testing.T, url string) {
		id, parents := jobref.ToIDAndParents(url)
		if id == "job" {
			t.Errorf("id should never equal %q for input %q", id, url)
		}
		for _, p := range parents {
			if p == "" || p == "job" {
				t.Errorf("parent %q is not a valid folder segment for input %q", p, url)
			}
		}
	})
}

func TestKindFromClass(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name  string
		class string
		want  jobref.Kind
	}{
		{"plain_folder", "com.cloudbees.hudson.plugins.folder.Folder", jobref.KindFolder},
		{"organization_folder", "jenkins.branch.OrganizationFolder", jobref.KindFolder},
		{"multibranch", "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject", jobref.KindMultiBranch},
		{"pipeline", "org.jenkinsci.plugins.workflow.job.WorkflowJob", jobref.KindPipeline},
		{"freestyle_is_not_classified", "hudson.model.FreeStyleProject", jobref.KindUnknown},
		{"empty_class", "", jobref.KindUnknown},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := jobref.KindFromClass(tt.class); got != tt.want {
				t.Errorf("KindFromClass(%q) = %v, want %v", tt.class, got, tt.want)
			}
		})
	}
}

func TestKindIsContainer(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind jobref.Kind
		want bool
	}{
		{jobref.KindFolder, true},
		{jobref.KindMultiBranch, true},
		{jobref.KindPipeline, false},
		{jobref.KindUnknown, false},
	}
	for _, tt := range tests {
		if got := tt.kind.IsContainer(); got != tt.want {
			t.Errorf("%v.IsContainer() = %v, want %v", tt.kind, got, tt.want)
		}
	}
}

func TestKindFavoriteKindRoundTrip(t *testing.T) {
	t.Parallel()
	tests := []struct {
		kind jobref.Kind
		want string
	}{
		{jobref.KindFolder, "folder"},
		{jobref.KindMultiBranch, "multibranch"},
		{jobref.KindPipeline, "job"},
		{jobref.KindUnknown, "job"},
	}
	for _, tt := range tests {
		if got := tt.kind.FavoriteKind(); got != tt.want {
			t.Errorf("%v.FavoriteKind() = %q, want %q", tt.kind, got, tt.want)
		}
	}

	// A stored favorite must round-trip back into a routing kind, so a
	// favorited container still opens without a discovery request.
	if got := jobref.KindFromFavoriteKind("folder"); got != jobref.KindFolder {
		t.Errorf(`KindFromFavoriteKind("folder") = %v, want KindFolder`, got)
	}
	if got := jobref.KindFromFavoriteKind("multibranch"); got != jobref.KindMultiBranch {
		t.Errorf(`KindFromFavoriteKind("multibranch") = %v, want KindMultiBranch`, got)
	}
	// "job" covers pipelines and freestyle alike, so it stays unknown and
	// takes the discovery path rather than guessing wrong.
	if got := jobref.KindFromFavoriteKind("job"); got != jobref.KindUnknown {
		t.Errorf(`KindFromFavoriteKind("job") = %v, want KindUnknown`, got)
	}
}
