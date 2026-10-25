package build

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/perfprofile"
	"github.com/atm1020/jenktui/src/ui/styles"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

const (
	animationWidth  = 160
	animationHeight = 50
)

// Keep this fixture identical across optimization tickets. No Init call: the
// fetched message replaces network/store/poll setup at the page boundary.
func newAnimationPage() (*Model, tea.Cmd) {
	styles.SwitchTheme(styles.Github)
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins.invalid")
	job := &gojenkins.Job{
		Jenkins: jenkins,
		Base:    "/job/build-animation",
		Raw:     &gojenkins.JobResponse{Name: "build-animation", URL: "http://jenkins.invalid/job/build-animation/"},
	}
	const startTime = 1_700_000_000_000
	stageNames := []string{"Checkout", "Build", "Unit tests", "Package", "Publish", "Deploy"}
	stages := make([]gojenkins.PipelineNode, len(stageNames))
	for i, name := range stageNames {
		stages[i] = gojenkins.PipelineNode{
			ID: fmt.Sprint(10 + i), Name: name, Status: "NOT_EXECUTED", ExecNode: "linux-agent-1",
			StartTime: startTime + int64(i)*60_000,
		}
	}
	stages[0].Status, stages[0].Duration = "SUCCESS", 42_000
	stages[1].Status, stages[1].Duration = "IN_PROGRESS", 95_000
	run := gojenkins.PipelineRun{
		Job: job, Base: job.Base + "/42", ID: "42", Name: "#42", Status: "IN_PROGRESS",
		StartTime: startTime, Duration: 137_000, Stages: stages,
	}

	steps := make([]gojenkins.PipelineNode, 5)
	for i := range steps {
		steps[i] = gojenkins.PipelineNode{
			ID: fmt.Sprint(100 + i), Name: fmt.Sprintf("sh: make step-%d", i+1), Status: "SUCCESS",
			StartTime: startTime + 60_000 + int64(i)*5_000, Duration: 4_000,
		}
	}
	// The running step stays inside the visible stage-details viewport.
	steps[3].Status = "IN_PROGRESS"
	steps[4].Status = "NOT_EXECUTED"
	node := stages[1]
	node.StageFlowNodes = steps

	var raw gojenkins.BuildResponse
	if err := json.Unmarshal([]byte(`{"number":42,"building":true,"artifacts":[
		{"fileName":"app.tar.gz","relativePath":"dist/app.tar.gz"},
		{"fileName":"coverage.xml","relativePath":"reports/coverage.xml"},
		{"fileName":"sbom.json","relativePath":"dist/sbom.json"}]}`), &raw); err != nil {
		panic(err)
	}
	build := gojenkins.Build{Raw: &raw, Job: job, Jenkins: jenkins, Base: run.Base}

	page := NewModel(&context.ProgramContext{Config: config.GetDefaultConfig(), Jenkins: jenkins}, run, animationWidth, animationHeight)
	// The router's Init would also fetch; the fixture delivers the fetch result.
	page.entered = true
	_, cmd := page.Update(BuildFetchedMsg{
		model: page, Generation: page.fetchGeneration, PipelineRun: run, Node: &node, Build: build,
		StartedBy: "Profile User", StartedByDetails: "Started by user Profile User",
	})
	_, _ = page.Update(tea.WindowSizeMsg{Width: animationWidth, Height: animationHeight})
	return page, cmd
}

type buildAnimationWorkload struct {
	page *Model
}

func newBuildAnimationWorkload(tb testing.TB) *buildAnimationWorkload {
	tb.Helper()
	page, cmd := newAnimationPage()
	tb.Cleanup(func() { _ = page.CleanUp() })
	if cmd == nil || page.loading || !page.clock.Running() {
		tb.Fatal("loaded running build did not start its frame clock")
	}
	_ = page.View()
	return &buildAnimationWorkload{page: page}
}

// advance delivers one page animation frame. Timer waiting is outside the
// benchmark; the live profile executes the real delayed command.
func (w *buildAnimationWorkload) advance() {
	_, _ = w.page.Update(w.page.clock.Frame())
}

func buildAnimationScenario() perfprofile.Scenario {
	page, start := newAnimationPage()
	return perfprofile.Scenario{
		Page: page, Start: start, Width: animationWidth, Height: animationHeight,
		Metrics: map[string]any{
			"scenario": "build-animation-v1", "scheduler": "page-frame", "frame_interval_ms": animation.FrameInterval.Milliseconds(),
			"replay_benchmark": "BenchmarkBuildPageAnimationFrame", "replay_views_per_op": 1,
			"stages": 6, "steps": 5, "artifacts": 3, "theme": "GitHub Dark",
		},
	}
}

func TestBuildAnimationWorkloadRendersAndAdvances(t *testing.T) {
	w := newBuildAnimationWorkload(t)
	before := w.page.View()
	for _, text := range []string{"Build #42", "In progress", "Artifacts", "app.tar.gz", "Checkout", "Unit tests", "sh: make step-4", "Started by: Profile User"} {
		if !strings.Contains(ansi.Strip(before), text) {
			t.Fatalf("workload is missing visible %q:\n%s", text, ansi.Strip(before))
		}
	}
	header := w.page.spinnerModel.View()
	stage := w.page.stageDetails.spinnerModel.View()
	w.advance()
	after := w.page.View()
	if after == before {
		t.Fatal("animation did not change the page")
	}
	if w.page.spinnerModel.View() == header || w.page.stageDetails.spinnerModel.View() == stage {
		t.Fatal("one frame did not advance the header and stage-details spinners together")
	}
	if !strings.Contains(after, w.page.spinnerModel.View()) || !strings.Contains(after, w.page.stageDetails.spinnerModel.View()) {
		t.Fatal("page does not render the advanced header and stage-details spinners")
	}
}

// BenchmarkBuildPageAnimationFrame is the build-animation-frame series. One
// operation is one animation message delivered and one view rendered, which
// is what each timer tick costs the app.
func BenchmarkBuildPageAnimationFrame(b *testing.B) {
	w := newBuildAnimationWorkload(b)
	b.ReportAllocs()
	for b.Loop() {
		w.advance()
		_ = w.page.View()
	}
	b.ReportMetric(1, "updates/op")
	b.ReportMetric(1, "views/op")
}

func TestBuildAnimationProfile(t *testing.T) {
	perfprofile.Run(t, buildAnimationScenario)
}
