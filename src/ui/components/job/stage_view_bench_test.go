package job

import (
	"fmt"
	"testing"

	"github.com/atm1020/jenktui/src/ui/animation"

	"github.com/atm1020/gojenkins"
)

func fakePipelineRun(id string, stageCount int, status string) *gojenkins.PipelineRun {
	stages := make([]gojenkins.PipelineNode, stageCount)
	statuses := []string{"SUCCESS", "FAILED", "IN_PROGRESS", "PENDING", "ABORTED", "NOT_EXECUTED"}
	for i := range stageCount {
		s := status
		if s == "MIXED" {
			s = statuses[i%len(statuses)]
		}
		stages[i] = gojenkins.PipelineNode{
			ID:       fmt.Sprintf("%d", i+1),
			Name:     fmt.Sprintf("Stage %d with a longer name", i+1),
			Status:   s,
			Duration: int64(30000 + i*5000),
		}
	}
	return &gojenkins.PipelineRun{
		ID:        id,
		Status:    status,
		StartTime: 1710500000000,
		Duration:  120000,
		Stages:    stages,
		URLs: map[string]map[string]string{
			"artifacts": {"href": "/artifacts"},
		},
	}
}

func newBenchStageViewModel() StageViewModel {
	return StageViewModel{
		wWithh:           120,
		wHeight:          40,
		pulseBar:         animation.New(animation.Settings{Width: 14, FPS: 10}),
		seenRuns:         make(map[string]struct{}),
		stageRenderCache: make(map[string]*StageRenderCache),
	}
}

func BenchmarkRenderRun(b *testing.B) {
	for _, stageCount := range []int{3, 6, 12} {
		b.Run(fmt.Sprintf("stages_%d/success", stageCount), func(b *testing.B) {
			svm := newBenchStageViewModel()
			run := fakePipelineRun("42", stageCount, "SUCCESS")
			b.ReportAllocs()
			for b.Loop() {
				_ = svm.renderRun(run, 0, StageBoxWidth)
			}
		})

		b.Run(fmt.Sprintf("stages_%d/mixed", stageCount), func(b *testing.B) {
			svm := newBenchStageViewModel()
			run := fakePipelineRun("42", stageCount, "MIXED")
			b.ReportAllocs()
			for b.Loop() {
				_ = svm.renderRun(run, 0, StageBoxWidth)
			}
		})
	}
}

func BenchmarkRenderRun_Selected(b *testing.B) {
	svm := newBenchStageViewModel()
	svm.selectedIndex = 0
	run := fakePipelineRun("42", 6, "MIXED")

	b.ReportAllocs()
	for b.Loop() {
		_ = svm.renderRun(run, 0, StageBoxWidth)
	}
}

func BenchmarkStageView_View(b *testing.B) {
	for _, runCount := range []int{1, 5, 10} {
		b.Run(fmt.Sprintf("runs_%d", runCount), func(b *testing.B) {
			svm := NewStageViewModel(120, 40, StageViewKeyMap{})
			runs := make([]gojenkins.PipelineRun, runCount)
			for i := range runCount {
				r := fakePipelineRun(fmt.Sprintf("%d", i+1), 6, "SUCCESS")
				runs[i] = *r
			}
			svm.SetPipelineRun(runs)

			b.ReportAllocs()
			for b.Loop() {
				_ = svm.View(120, 40)
			}
		})
	}
}
