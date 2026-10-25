package buildstatus_test

import (
	"testing"

	"github.com/atm1020/jenktui/src/buildstatus"
)

func TestActivityIsActive(t *testing.T) {
	tests := []struct {
		activity buildstatus.Activity
		want     bool
	}{
		{activity: buildstatus.ActivityUnknown},
		{activity: buildstatus.ActivityQueued, want: true},
		{activity: buildstatus.ActivityPending, want: true},
		{activity: buildstatus.ActivityRunning, want: true},
		{activity: buildstatus.ActivityPaused, want: true},
		{activity: buildstatus.ActivityFinished},
		{activity: buildstatus.ActivityIdle},
		{activity: buildstatus.ActivityDisabled},
	}

	for _, tt := range tests {
		if got := tt.activity.IsActive(); got != tt.want {
			t.Errorf("Activity(%d).IsActive() = %v, want %v", tt.activity, got, tt.want)
		}
	}
}
