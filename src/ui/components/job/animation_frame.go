package job

import (
	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/ui/animation"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
)

func (m *Model) handleAnimationFrame(msg animation.FrameMsg) tea.Cmd {
	if !m.clock.Accept(msg) {
		log.Debug("job: rejected stale animation frame")
		return nil
	}
	if !m.shouldAnimate() {
		return nil
	}
	if m.loading {
		m.loadingSpinner = animation.AdvanceSpinner(m.loadingSpinner)
	} else {
		m.advanceBuildAnimations()
	}
	return m.clock.Next()
}

// ensureAnimation is the clock's one sync point. Init, the end of Update, and
// ApplyPollSnapshot call it, so no individual state change has to.
func (m *Model) ensureAnimation() tea.Cmd {
	active := m.shouldAnimate()
	m.animating = active && !m.loading
	return m.clock.Ensure(active)
}

// shouldAnimate covers the loading spinner and any moving run or stage. A
// page outside Init/CleanUp is not on screen, so it never animates.
func (m *Model) shouldAnimate() bool {
	return m.entered && (m.loading || m.buildsShouldAnimate())
}

func (m *Model) stopAnimation() {
	m.animating = false
	m.clock.Stop()
}

func (m *Model) advanceBuildAnimations() {
	m.spinnerModel = animation.AdvanceSpinner(m.spinnerModel)
	if m.stageView != nil {
		m.stageView.advanceAnimation()
	}
}

// buildsShouldAnimate reports whether any visible run or stage should keep
// moving. Paused input is still in progress, but it is static, so it must not
// keep the frame scheduler alive. hasInProgressRun remains the broader
// active-work check.
func (m *Model) buildsShouldAnimate() bool {
	if m.stageView == nil {
		return false
	}
	for _, run := range m.stageView.pipelineRuns {
		if run == nil {
			continue
		}
		status := buildstatus.PipelineStatus(run.Status).
			NormalizeQueueState(len(run.Stages), run.EndTime)
		if status.ShouldAnimate(run.EndTime) {
			return true
		}
		for i := range run.Stages {
			if buildstatus.PipelineStatus(run.Stages[i].Status).ShouldAnimate(0) {
				return true
			}
		}
	}
	return false
}
