package folder

import (
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"github.com/atm1020/gojenkins"
)

func boolPtr(b bool) *bool { return &b }

func TestGetWeatherIcon(t *testing.T) {
	t.Parallel()
	d := InnerJobsDelegate{}
	theme := styles.GetTheme()

	t.Run("no_health_data_is_unavailable", func(t *testing.T) {
		t.Parallel()
		if got := d.getWeatherIcon(InnerJobsDelegate{HealthReport: nil}); got != theme.S().StyledIcons.NotAvailable {
			t.Errorf("missing health data = %q, want the unavailable indicator %q", got, theme.S().StyledIcons.NotAvailable)
		}
	})

	tests := []struct {
		name  string
		score int64
		want  string
	}{
		{"sunny_at_100", 100, theme.S().StyledIcons.Sunny},
		{"sunny_at_80_boundary", 80, theme.S().StyledIcons.Sunny},
		{"one_cloud_at_79_boundary", 79, theme.S().StyledIcons.OneCloud},
		{"one_cloud_at_60_boundary", 60, theme.S().StyledIcons.OneCloud},
		{"cloudy_at_59_boundary", 59, theme.S().StyledIcons.Cloudy},
		{"cloudy_at_40_boundary", 40, theme.S().StyledIcons.Cloudy},
		{"storm_at_39_boundary", 39, theme.S().StyledIcons.ThunderStorm},
		{"storm_at_0", 0, theme.S().StyledIcons.ThunderStorm},
	}
	for _, tt := range tests {
		got := d.getWeatherIcon(InnerJobsDelegate{HealthReport: []gojenkins.HealthReport{{Score: tt.score}}})
		if got != tt.want {
			t.Errorf("%s: getWeatherIcon(score=%d) = %q, want %q", tt.name, tt.score, got, tt.want)
		}
	}
}

// TestGetWeatherIcon_UsesLowestOfSeveralReports covers the case Jenkins
// actually produces on a job with more than one health metric (build
// stability plus test results, say): the weather column must show the most
// concerning of them, not whichever happened to be listed first.
func TestGetWeatherIcon_UsesLowestOfSeveralReports(t *testing.T) {
	t.Parallel()
	d := InnerJobsDelegate{}
	theme := styles.GetTheme()

	got := d.getWeatherIcon(InnerJobsDelegate{HealthReport: []gojenkins.HealthReport{
		{Score: 100},
		{Score: 20},
		{Score: 65},
	}})
	if want := theme.S().StyledIcons.ThunderStorm; got != want {
		t.Errorf("getWeatherIcon(100,20,65) = %q, want storm %q from the lowest score", got, want)
	}
}

func TestIsOrphaned(t *testing.T) {
	t.Parallel()
	d := InnerJobsDelegate{}
	const pipelineClass = "org.jenkinsci.plugins.workflow.job.WorkflowJob"
	tests := []struct {
		name string
		item InnerJobsDelegate
		want bool
	}{
		{
			// Jenkins omits buildable for kinds it does not apply to, so an
			// absent value means "not applicable" and must not disable a row.
			name: "absent_buildable_is_not_disabled",
			item: InnerJobsDelegate{Class: pipelineClass, Buildable: nil},
			want: false,
		},
		{
			name: "orphaned_branch",
			item: InnerJobsDelegate{Class: pipelineClass, Buildable: boolPtr(false)},
			want: true,
		},
		{
			name: "active_branch",
			item: InnerJobsDelegate{Class: pipelineClass, Buildable: boolPtr(true)},
			want: false,
		},
		{
			// Regression: a plain sub-folder is never "buildable" either, so
			// without the class check every folder child would render as
			// struck-through/disabled.
			name: "plain_subfolder_never_orphaned",
			item: InnerJobsDelegate{Class: "com.cloudbees.hudson.plugins.folder.Folder", Buildable: boolPtr(false)},
			want: false,
		},
		{
			name: "multibranch_container_never_orphaned",
			item: InnerJobsDelegate{Class: "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject", Buildable: boolPtr(false)},
			want: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := d.isOrphaned(tt.item); got != tt.want {
				t.Errorf("isOrphaned(%+v) = %v, want %v", tt.item, got, tt.want)
			}
		})
	}
}

func TestFormatBuildRefTime(t *testing.T) {
	t.Parallel()
	d := InnerJobsDelegate{}

	if got := d.formatBuildRefTime(nil); got != "N/A" {
		t.Errorf("nil ref = %q, want N/A", got)
	}
	const timestamp = int64(1731000000000)
	got := d.formatBuildRefTime(&gojenkins.BuildRef{Number: 5, Timestamp: timestamp})
	if !strings.Contains(got, "#5") {
		t.Errorf("formatBuildRefTime = %q, want it to carry the build number #5", got)
	}
	if want := util.FormatStartTime(timestamp); !strings.Contains(got, want) {
		t.Errorf("formatBuildRefTime = %q, want it to carry the build timestamp %q", got, want)
	}
}

func TestFormatLastBuildDuration(t *testing.T) {
	t.Parallel()
	d := InnerJobsDelegate{}

	tests := []struct {
		name string
		item InnerJobsDelegate
		want string
	}{
		{
			name: "no_last_build",
			item: InnerJobsDelegate{Color: "notbuilt"},
			want: "N/A",
		},
		{
			name: "completed_build_shows_duration",
			item: InnerJobsDelegate{Color: "blue", LastBuild: &gojenkins.BuildRef{Duration: 26000}},
			want: "26s",
		},
		{
			// Jenkins reports duration 0 while a build is running, so without
			// the animated-color check an in-progress job claims it finished
			// instantly.
			name: "running_build_is_not_zero_duration",
			item: InnerJobsDelegate{Color: "blue_anime", LastBuild: &gojenkins.BuildRef{Duration: 0}},
			want: "Running",
		},
		{
			name: "first_ever_run_is_running",
			item: InnerJobsDelegate{Color: "notbuilt_anime", LastBuild: &gojenkins.BuildRef{Duration: 0}},
			want: "Running",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := d.formatLastBuildDuration(tt.item); got != tt.want {
				t.Errorf("formatLastBuildDuration(%+v) = %q, want %q", tt.item, got, tt.want)
			}
		})
	}
}

// TestAnimatedColorsKeepMeaningfulStatus guards against in-progress jobs
// falling through to "Unknown": Jenkins encodes "building" as an "_anime"
// suffix on the outcome color, not as a distinct color.
func TestAnimatedColorsKeepMeaningfulStatus(t *testing.T) {
	t.Parallel()
	d := InnerJobsDelegate{}
	theme := styles.GetTheme()

	for _, color := range []string{"blue_anime", "red_anime", "yellow_anime", "notbuilt_anime"} {
		if got := d.getStatusLabel(color); got != "Running" {
			t.Errorf("getStatusLabel(%q) = %q, want Running", color, got)
		}
		if got := d.getColorIcon(color); got != theme.S().StyledIcons.Running {
			t.Errorf("getColorIcon(%q) = %q, want the running icon", color, got)
		}
	}

	// Non-animated colors are unaffected.
	if got := d.getStatusLabel("blue"); got != "Success" {
		t.Errorf(`getStatusLabel("blue") = %q, want Success`, got)
	}
	if got := d.getStatusLabel("wat"); got != "Unknown" {
		t.Errorf(`getStatusLabel("wat") = %q, want Unknown`, got)
	}
}

func TestComputeColumnWidths_NameGetsDoubleWeight(t *testing.T) {
	t.Parallel()
	w := computeColumnWidths(140)
	if w.Name != w.Type*2 {
		t.Errorf("Name width = %d, want double Type width %d", w.Name, w.Type*2)
	}
	if w.Weather != weatherColumnWidth {
		t.Errorf("Weather width = %d, want fixed %d", w.Weather, weatherColumnWidth)
	}
	if w.Total() > 140 {
		t.Errorf("Total() = %d, must not exceed contentWidth 140", w.Total())
	}
}

// TestChildTypeReflectsItsContainer: a branch of a MultiBranch Pipeline is an
// ordinary WorkflowJob under the hood, so class alone renders it as
// "Pipeline". Inside a MultiBranch page that's technically true and
// practically useless — every row would say the same thing. The container's
// own kind is what makes the row meaningful.
func TestChildTypeReflectsItsContainer(t *testing.T) {
	t.Parallel()
	d := InnerJobsDelegate{}
	theme := styles.GetTheme()

	const (
		pipelineClass    = "org.jenkinsci.plugins.workflow.job.WorkflowJob"
		folderClass      = "com.cloudbees.hudson.plugins.folder.Folder"
		multiBranchClass = "org.jenkinsci.plugins.workflow.multibranch.WorkflowMultiBranchProject"
	)

	tests := []struct {
		name      string
		item      InnerJobsDelegate
		wantLabel string
		wantIcon  string
	}{
		{
			name:      "workflow_job_inside_multibranch_is_a_branch",
			item:      InnerJobsDelegate{Class: pipelineClass, ParentKind: jobref.KindMultiBranch},
			wantLabel: "Branch",
			wantIcon:  theme.S().StyledIcons.Branch,
		},
		{
			name:      "workflow_job_inside_a_folder_is_a_pipeline",
			item:      InnerJobsDelegate{Class: pipelineClass, ParentKind: jobref.KindFolder},
			wantLabel: "Pipeline",
			wantIcon:  theme.S().StyledIcons.WorkflowJobIcon,
		},
		{
			// An Organization Folder's children are MultiBranch projects;
			// those keep their own identity regardless of the parent.
			name:      "multibranch_inside_a_folder_stays_multibranch",
			item:      InnerJobsDelegate{Class: multiBranchClass, ParentKind: jobref.KindFolder},
			wantLabel: "Multibranch",
			wantIcon:  theme.S().StyledIcons.Branch,
		},
		{
			name:      "nested_folder_is_unaffected_by_its_parent",
			item:      InnerJobsDelegate{Class: folderClass, ParentKind: jobref.KindMultiBranch},
			wantLabel: "Folder",
			wantIcon:  theme.S().StyledIcons.Folder,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := d.getClassLabel(tt.item); got != tt.wantLabel {
				t.Errorf("getClassLabel = %q, want %q", got, tt.wantLabel)
			}
			if got := d.getClassIcon(tt.item); got != tt.wantIcon {
				t.Errorf("getClassIcon = %q, want %q", got, tt.wantIcon)
			}
		})
	}
}
