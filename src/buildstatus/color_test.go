package buildstatus_test

import (
	"testing"

	"github.com/atm1020/jenktui/src/buildstatus"
)

func TestJobColorSeparatesRunningActivityFromOutcome(t *testing.T) {
	tests := []struct {
		raw      string
		label    string
		activity buildstatus.Activity
		outcome  buildstatus.Outcome
	}{
		{"blue", "Success", buildstatus.ActivityIdle, buildstatus.OutcomeSuccess},
		{"red_anime", "Running", buildstatus.ActivityRunning, buildstatus.OutcomeFailure},
		{"yellow", "Unstable", buildstatus.ActivityIdle, buildstatus.OutcomeUnstable},
		{"aborted", "Aborted", buildstatus.ActivityIdle, buildstatus.OutcomeAborted},
		{"notbuilt_anime", "Running", buildstatus.ActivityRunning, buildstatus.OutcomeNotBuilt},
		{"disabled", "Disabled", buildstatus.ActivityDisabled, buildstatus.OutcomeUnknown},
		{"SOMETHING_NEW", "Unknown", buildstatus.ActivityUnknown, buildstatus.OutcomeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.raw, func(t *testing.T) {
			color := buildstatus.JobColor(tt.raw)
			if got := color.Label(); got != tt.label {
				t.Errorf("Label() = %q, want %q", got, tt.label)
			}
			if got := color.Activity(); got != tt.activity {
				t.Errorf("Activity() = %v, want %v", got, tt.activity)
			}
			if got := color.Outcome(); got != tt.outcome {
				t.Errorf("Outcome() = %v, want %v", got, tt.outcome)
			}
		})
	}
}
