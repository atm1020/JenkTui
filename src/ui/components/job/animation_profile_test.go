package job

import (
	"testing"

	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/perfprofile"
)

func TestJobAnimationProfile(t *testing.T) {
	perfprofile.Run(t, func() perfprofile.Scenario {
		page, start := newAnimationPage()
		return perfprofile.Scenario{
			Page: page, Start: start, Width: animationWidth, Height: animationHeight,
			Metrics: map[string]any{
				"scenario": "job-animation-v1", "scheduler": "page-frame",
				"frame_interval_ms": animation.FrameInterval.Milliseconds(),
				"replay_benchmark":  "BenchmarkJobPageAnimationFrame", "replay_views_per_op": 1,
				"runs": 20, "stages_per_run": 6, "theme": "GitHub Dark",
			},
		}
	})
}
