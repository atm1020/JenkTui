package job

import (
	"fmt"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/styles"

	"github.com/atm1020/gojenkins"

	tea "charm.land/bubbletea/v2"
)

const (
	animationWidth  = 160
	animationHeight = 50
)

// Keep this fixture identical across optimization tickets. No Init call: the
// loaded message replaces network/store/poll setup at the page boundary, and
// the page is marked entered as Init would.
func newAnimationPage() (*Model, tea.Cmd) {
	styles.SwitchTheme(styles.Github)
	jenkins := gojenkins.CreateJenkins(nil, "http://jenkins.invalid")
	job := &gojenkins.Job{
		Jenkins: jenkins,
		Base:    "/job/animation-profile",
		Raw: &gojenkins.JobResponse{
			Name: "animation-profile", URL: "http://jenkins.invalid/job/animation-profile/", Color: "blue_anime",
			HealthReport: []struct {
				Description   string `json:"description"`
				IconClassName string `json:"iconClassName"`
				IconUrl       string `json:"iconUrl"`
				Score         int64  `json:"score"`
			}{{Score: 80, Description: "Build stability: 4 out of the last 5 builds succeeded."}},
		},
	}
	runs := make([]gojenkins.PipelineRun, 20)
	for i := range runs {
		id := fmt.Sprint(120 - i)
		run := fakePipelineRun(id, 6, "SUCCESS")
		run.Job = job
		run.Base = job.Base + "/" + id
		run.EndTime = run.StartTime + run.Duration
		if i == 0 {
			run.Status = "IN_PROGRESS"
			run.EndTime = 0
			run.Stages[1].Status = "IN_PROGRESS"
			for j := 2; j < len(run.Stages); j++ {
				run.Stages[j].Status = "NOT_EXECUTED"
			}
		}
		runs[i] = *run
		job.Raw.Builds = append(job.Raw.Builds, gojenkins.JobBuild{Number: int64(120 - i), URL: job.Raw.URL + id + "/"})
	}
	page := NewModel(&context.ProgramContext{Config: config.GetDefaultConfig(), Jenkins: jenkins}, animationWidth, animationHeight, job)
	page.entered = true
	_, cmd := page.Update(JobLoadedMsg{Job: job, PipelineRuns: runs, HasInProgressBuild: true})
	_, _ = page.Update(tea.WindowSizeMsg{Width: animationWidth, Height: animationHeight})
	return page, cmd
}

type animationWorkload struct {
	page *Model
}

func newAnimationWorkload(tb testing.TB) *animationWorkload {
	tb.Helper()
	page, cmd := newAnimationPage()
	tb.Cleanup(func() { _ = page.CleanUp() })
	if cmd == nil || !page.clock.Running() {
		tb.Fatal("loaded active page did not start the frame scheduler")
	}
	_ = page.View() // Warm stable rows and viewport content.
	return &animationWorkload{page: page}
}

// advance delivers one page animation frame. Timer waiting is outside the
// benchmark; the live profile executes the real delayed command.
func (w *animationWorkload) advance() {
	_, _ = w.page.Update(w.page.clock.Frame())
}

// BenchmarkJobPageAnimationFrame is the job-animation-frame series. One
// operation is one Update/View pair that advances header, stage, and pulse
// together. It is not comparable to BenchmarkJobPageAnimation, whose operation
// was three Update/View pairs.
func BenchmarkJobPageAnimationFrame(b *testing.B) {
	w := newAnimationWorkload(b)
	b.ReportAllocs()
	for b.Loop() {
		w.advance()
		_ = w.page.View()
	}
	b.ReportMetric(1, "updates/op")
	b.ReportMetric(1, "views/op")
}
