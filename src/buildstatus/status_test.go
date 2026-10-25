package buildstatus_test

import (
	"testing"

	"github.com/atm1020/jenktui/src/buildstatus"
)

func TestPipelineStatusInProgressHasUserFacingSemantics(t *testing.T) {
	status := buildstatus.PipelineStatus("IN_PROGRESS")

	if got := status.Label(); got != "In progress" {
		t.Errorf("Label() = %q, want %q", got, "In progress")
	}
	if got := status.String(); got != "In progress" {
		t.Errorf("String() = %q, want %q", got, "In progress")
	}
	if got := string(status); got != "IN_PROGRESS" {
		t.Errorf("explicit raw conversion = %q, want %q", got, "IN_PROGRESS")
	}
	if got := status.Activity(0); got != buildstatus.ActivityRunning {
		t.Errorf("Activity() = %v, want ActivityRunning", got)
	}
	if status.IsTerminal(0) {
		t.Error("IsTerminal() = true, want false")
	}
}

func TestPipelineStatusOutcomeIsIndependentFromActivity(t *testing.T) {
	tests := []struct {
		status  buildstatus.PipelineStatus
		outcome buildstatus.Outcome
	}{
		{"SUCCESS", buildstatus.OutcomeSuccess},
		{"FAILED", buildstatus.OutcomeFailure},
		{"FAILURE", buildstatus.OutcomeFailure},
		{"UNSTABLE", buildstatus.OutcomeUnstable},
		{"ABORTED", buildstatus.OutcomeAborted},
		{"NOT_EXECUTED", buildstatus.OutcomeNotBuilt},
		{"IN_PROGRESS", buildstatus.OutcomeUnknown},
		{"SOMETHING_NEW", buildstatus.OutcomeUnknown},
	}

	for _, tt := range tests {
		if got := tt.status.Outcome(); got != tt.outcome {
			t.Errorf("%q.Outcome() = %v, want %v", tt.status, got, tt.outcome)
		}
	}
}

func TestPipelineStatusPredicatesDistinguishActiveKinds(t *testing.T) {
	tests := []struct {
		status    buildstatus.PipelineStatus
		endTime   int64
		active    bool
		executing bool
		waiting   bool
		animate   bool
	}{
		{"QUEUED", 0, true, false, true, true},
		{"PENDING", 0, true, false, true, true},
		{"IN_PROGRESS", 0, true, true, false, true},
		{"RUNNING", 0, true, true, false, true},
		{"PAUSED_PENDING_INPUT", 0, true, false, true, false},
		{"NOT_EXECUTED", 0, false, false, false, false},
		{"NOT_EXECUTED", 1000, false, false, false, false},
		{"SUCCESS", 1000, false, false, false, false},
		{"SOMETHING_NEW", 0, false, false, false, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			if got := tt.status.IsActive(tt.endTime); got != tt.active {
				t.Errorf("IsActive() = %v, want %v", got, tt.active)
			}
			if got := tt.status.IsExecuting(); got != tt.executing {
				t.Errorf("IsExecuting() = %v, want %v", got, tt.executing)
			}
			if got := tt.status.IsWaiting(tt.endTime); got != tt.waiting {
				t.Errorf("IsWaiting() = %v, want %v", got, tt.waiting)
			}
			if got := tt.status.ShouldAnimate(tt.endTime); got != tt.animate {
				t.Errorf("ShouldAnimate() = %v, want %v", got, tt.animate)
			}
		})
	}
}

func TestPipelineStatusNormalizesPreStageRunsWithoutChangingStoredWireValues(t *testing.T) {
	tests := []struct {
		status     buildstatus.PipelineStatus
		stageCount int
		endTime    int64
		want       buildstatus.PipelineStatus
	}{
		{"IN_PROGRESS", 0, 0, buildstatus.StatusQueued},
		{"RUNNING", 0, 0, buildstatus.StatusQueued},
		{"NOT_EXECUTED", 0, 0, buildstatus.StatusQueued},
		{"NOT_EXECUTED", 0, 1000, buildstatus.StatusNotExecuted},
		{"IN_PROGRESS", 1, 0, buildstatus.StatusInProgress},
		{"SOMETHING_NEW", 0, 0, "SOMETHING_NEW"},
	}

	for _, tt := range tests {
		if got := tt.status.NormalizeQueueState(tt.stageCount, tt.endTime); got != tt.want {
			t.Errorf("%q.NormalizeQueueState(%d, %d) = %q, want %q", string(tt.status), tt.stageCount, tt.endTime, string(got), string(tt.want))
		}
	}
}

func TestRunStatusesReturnsIndependentDisplayOrder(t *testing.T) {
	first := buildstatus.RunStatuses()
	want := []buildstatus.PipelineStatus{
		buildstatus.StatusQueued,
		buildstatus.StatusPending,
		buildstatus.StatusInProgress,
		buildstatus.StatusRunning,
		buildstatus.StatusPausedPendingInput,
		buildstatus.StatusSuccess,
		buildstatus.StatusFailed,
		buildstatus.StatusUnstable,
		buildstatus.StatusAborted,
		buildstatus.StatusNotExecuted,
	}
	if len(first) != len(want) {
		t.Fatalf("len(RunStatuses()) = %d, want %d", len(first), len(want))
	}
	for i := range want {
		if first[i] != want[i] {
			t.Errorf("RunStatuses()[%d] = %q, want %q", i, first[i], want[i])
		}
	}

	first[0] = buildstatus.StatusSuccess
	if got := buildstatus.RunStatuses()[0]; got != buildstatus.StatusQueued {
		t.Errorf("caller mutated RunStatuses(): first value = %q, want QUEUED", got)
	}
}

func TestPipelineStatusKnownValuesHaveConsistentLifecycleAndLabels(t *testing.T) {
	tests := []struct {
		raw      string
		label    string
		activity buildstatus.Activity
		terminal bool
	}{
		{"QUEUED", "Queued", buildstatus.ActivityQueued, false},
		{"PENDING", "Pending", buildstatus.ActivityPending, false},
		{"IN_PROGRESS", "In progress", buildstatus.ActivityRunning, false},
		{"RUNNING", "Running", buildstatus.ActivityRunning, false},
		{"PAUSED_PENDING_INPUT", "Waiting for approval", buildstatus.ActivityPaused, false},
		{"SUCCESS", "Success", buildstatus.ActivityFinished, true},
		{"FAILED", "Failed", buildstatus.ActivityFinished, true},
		{"FAILURE", "Failed", buildstatus.ActivityFinished, true},
		{"UNSTABLE", "Unstable", buildstatus.ActivityFinished, true},
		{"ABORTED", "Aborted", buildstatus.ActivityFinished, true},
		{"NOT_EXECUTED", "Not executed", buildstatus.ActivityFinished, true},
		{"SOMETHING_NEW", "Unknown", buildstatus.ActivityUnknown, false},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			status := buildstatus.PipelineStatus(tt.raw)
			if got := status.Label(); got != tt.label {
				t.Errorf("Label() = %q, want %q", got, tt.label)
			}
			if got := status.Activity(1000); got != tt.activity {
				t.Errorf("Activity() = %v, want %v", got, tt.activity)
			}
			if got := status.IsTerminal(1000); got != tt.terminal {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}
