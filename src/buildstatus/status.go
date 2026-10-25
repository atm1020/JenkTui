// Package buildstatus interprets Jenkins pipeline statuses and classic job
// colors without leaking their wire representations into callers.
package buildstatus

import "strings"

// PipelineStatus is the status string returned by Jenkins' pipeline API and
// stored verbatim in the run database.
type PipelineStatus string

const (
	StatusQueued             PipelineStatus = "QUEUED"
	StatusPending            PipelineStatus = "PENDING"
	StatusInProgress         PipelineStatus = "IN_PROGRESS"
	StatusRunning            PipelineStatus = "RUNNING"
	StatusPausedPendingInput PipelineStatus = "PAUSED_PENDING_INPUT"
	StatusSuccess            PipelineStatus = "SUCCESS"
	StatusFailed             PipelineStatus = "FAILED"
	StatusFailure            PipelineStatus = "FAILURE"
	StatusUnstable           PipelineStatus = "UNSTABLE"
	StatusAborted            PipelineStatus = "ABORTED"
	StatusNotExecuted        PipelineStatus = "NOT_EXECUTED"
)

var runStatuses = [...]PipelineStatus{
	StatusQueued,
	StatusPending,
	StatusInProgress,
	StatusRunning,
	StatusPausedPendingInput,
	StatusSuccess,
	StatusFailed,
	StatusUnstable,
	StatusAborted,
	StatusNotExecuted,
}

func (s PipelineStatus) normalized() PipelineStatus {
	return PipelineStatus(strings.ToUpper(strings.TrimSpace(string(s))))
}

// RunStatuses returns a copy of the pipeline statuses shown by run-history
// filters in stable lifecycle-first order. Returning the package-owned array
// directly would let callers accidentally change that shared order.
func RunStatuses() []PipelineStatus {
	statuses := make([]PipelineStatus, len(runStatuses))
	copy(statuses, runStatuses[:])
	return statuses
}

// String returns user-facing text so generic formatting cannot accidentally
// expose a Jenkins wire value in the UI. Use string(s) explicitly at API,
// persistence, and diagnostic boundaries that require the raw value.
func (s PipelineStatus) String() string {
	return s.Label()
}

// Label returns user-facing text. Unknown wire values deliberately do not leak
// into the UI; callers can still log or persist string(s).
func (s PipelineStatus) Label() string {
	switch s.normalized() {
	case StatusQueued:
		return "Queued"
	case StatusPending:
		return "Pending"
	case StatusInProgress:
		return "In progress"
	case StatusRunning:
		return "Running"
	case StatusPausedPendingInput:
		return "Waiting for approval"
	case StatusSuccess:
		return "Success"
	case StatusFailed, StatusFailure:
		return "Failed"
	case StatusUnstable:
		return "Unstable"
	case StatusAborted:
		return "Aborted"
	case StatusNotExecuted:
		return "Not executed"
	default:
		return "Unknown"
	}
}

// Outcome returns the result represented by the status independently of its
// current lifecycle activity.
func (s PipelineStatus) Outcome() Outcome {
	switch s.normalized() {
	case StatusSuccess:
		return OutcomeSuccess
	case StatusFailed, StatusFailure:
		return OutcomeFailure
	case StatusUnstable:
		return OutcomeUnstable
	case StatusAborted:
		return OutcomeAborted
	case StatusNotExecuted:
		return OutcomeNotBuilt
	default:
		return OutcomeUnknown
	}
}

// Activity returns the lifecycle activity represented by the status. Jenkins
// transiently reports NOT_EXECUTED before a run has an end time, so that value
// remains unknown until end-time or stage context disambiguates it.
func (s PipelineStatus) Activity(endTime int64) Activity {
	switch s.normalized() {
	case StatusQueued:
		return ActivityQueued
	case StatusPending:
		return ActivityPending
	case StatusInProgress, StatusRunning:
		return ActivityRunning
	case StatusPausedPendingInput:
		return ActivityPaused
	case StatusSuccess, StatusFailed, StatusFailure, StatusUnstable, StatusAborted:
		return ActivityFinished
	case StatusNotExecuted:
		if endTime > 0 {
			return ActivityFinished
		}
		// Without stage context this value is ambiguous. NormalizeQueueState
		// turns the pre-stage form into StatusQueued where that context exists.
		return ActivityUnknown
	default:
		return ActivityUnknown
	}
}

// NormalizeQueueState converts misleading pre-stage wfapi values into the
// explicit queued state used by JenkTui. The caller supplies the stage count
// because PipelineStatus does not own the stage graph, but both the zero-stage
// check and its interpretation stay here so UI and polling clients do not
// duplicate this Jenkins workaround. Other values are returned verbatim;
// callers may persist either the original value or this deliberate QUEUED
// normalization.
func (s PipelineStatus) NormalizeQueueState(stageCount int, endTime int64) PipelineStatus {
	if stageCount == 0 {
		switch s.normalized() {
		case StatusInProgress, StatusRunning:
			return StatusQueued
		case StatusNotExecuted:
			if endTime == 0 {
				return StatusQueued
			}
		}
	}
	return s
}

// IsTerminal reports whether Jenkins has finished the run.
func (s PipelineStatus) IsTerminal(endTime int64) bool {
	return s.Activity(endTime) == ActivityFinished
}

// IsActive reports whether a known status still requires Jenkins to make
// progress. Unknown statuses remain non-terminal but are not presented as
// actively running.
func (s PipelineStatus) IsActive(endTime int64) bool {
	return s.Activity(endTime).IsActive()
}

// IsExecuting reports whether Jenkins is currently executing build work.
func (s PipelineStatus) IsExecuting() bool {
	return s.Activity(0) == ActivityRunning
}

// IsWaiting reports whether the run is queued, pending, or paused for input.
func (s PipelineStatus) IsWaiting(endTime int64) bool {
	switch s.Activity(endTime) {
	case ActivityQueued, ActivityPending, ActivityPaused:
		return true
	default:
		return false
	}
}

// ShouldAnimate reports whether a status should receive continuous progress
// animation. Paused input is active but intentionally static so it reads as an
// action request rather than ongoing execution.
func (s PipelineStatus) ShouldAnimate(endTime int64) bool {
	switch s.Activity(endTime) {
	case ActivityQueued, ActivityPending, ActivityRunning:
		return true
	default:
		return false
	}
}
