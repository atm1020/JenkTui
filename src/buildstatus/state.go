package buildstatus

// Activity describes where a build is in its lifecycle independently of its
// result.
type Activity uint8

const (
	ActivityUnknown Activity = iota
	ActivityQueued
	ActivityPending
	ActivityRunning
	ActivityPaused
	ActivityFinished
	ActivityIdle
	ActivityDisabled
)

// IsActive reports whether this activity still requires Jenkins to make progress.
func (a Activity) IsActive() bool {
	switch a {
	case ActivityQueued, ActivityPending, ActivityRunning, ActivityPaused:
		return true
	default:
		return false
	}
}

// Outcome describes a completed or last-known build result independently of
// whether Jenkins is currently doing work.
type Outcome uint8

const (
	OutcomeUnknown Outcome = iota
	OutcomeSuccess
	OutcomeFailure
	OutcomeUnstable
	OutcomeAborted
	OutcomeNotBuilt
)
