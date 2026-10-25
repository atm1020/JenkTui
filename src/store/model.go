package store

import "github.com/atm1020/jenktui/src/buildstatus"

// RunRecord represents a single build run tracked by the user.
// ObservedRun is the normalized Jenkins state applied by the poller.
type ObservedRun struct {
	BuildID   string
	Status    buildstatus.PipelineStatus
	StartTime int64
	EndTime   int64
	Duration  int64
}

// RunObservationBatch controls one transactional job observation update.
type RunObservationBatch struct {
	JobURL       string
	JobName      string
	Runs         []ObservedRun
	TrackNewRuns bool
	Baseline     int64
	ObservedAt   int64
}

// ApplyRunObservationResult reports whether the committed projection changed.
type ApplyRunObservationResult struct {
	Changed     bool
	Completions []CompletionRecord
}

// CompletionKey is the durable identity of one externally deliverable fact.
type CompletionKey struct {
	ServerID string
	JobURL   string
	BuildID  string
	Kind     string
}

// CompletionRecord is a pending completion notification projection.
type CompletionRecord struct {
	Key       CompletionKey
	JobName   string
	Status    buildstatus.PipelineStatus
	StartTime int64
	EndTime   int64
	Duration  int64
	CreatedAt int64
}

type TriggerState string

const (
	TriggerPrepared TriggerState = "prepared"
	TriggerAccepted TriggerState = "accepted"
	TriggerResolved TriggerState = "resolved"
	TriggerFailed   TriggerState = "failed"
	TriggerCanceled TriggerState = "canceled"
	TriggerExpired  TriggerState = "expired"
	TriggerDeleted  TriggerState = "deleted"
)

// TriggerIntentRecord durably correlates one local trigger attempt with one
// Jenkins queue receipt and, eventually, one exact executable build.
type TriggerIntentRecord struct {
	IntentID      string
	ServerID      string
	JobURL        string
	BaselineBuild int64
	// QueueID is the Jenkins queue item. nil means Jenkins never returned one
	// (script replay: PipelineRun.Replay only reports success). Stored as SQL NULL.
	QueueID    *int64
	BuildID    string
	State      TriggerState
	CreatedAt  int64
	AcceptedAt int64
	ExpiresAt  int64
	ResolvedAt int64
}

type RunRecord struct {
	ID        string                     `json:"id"`         // composite key: "{jobURL}#{buildID}"
	ServerID  string                     `json:"server_id"`  // Jenkins connection that owns this run
	JobName   string                     `json:"job_name"`   // human-readable, e.g. "my-folder/my-pipeline"
	JobURL    string                     `json:"job_url"`    // full Jenkins job URL for re-fetching
	BuildID   string                     `json:"build_id"`   // pipeline run ID (e.g. "42")
	Status    buildstatus.PipelineStatus `json:"status"`     // Jenkins wire value stored as TEXT
	StartTime int64                      `json:"start_time"` // millis since epoch
	EndTime   int64                      `json:"end_time"`   // 0 if still running
	Duration  int64                      `json:"duration"`   // millis, 0 if still running
	CreatedAt int64                      `json:"created_at"` // when JenkTui recorded this
}

// IsTerminal returns true if the run status is a final state.
//
// NOT_EXECUTED is ambiguous: wfapi reports it both for a run that genuinely
// finished with no stage graph (e.g. a Jenkinsfile parse error) and,
// transiently, for a run that hasn't started producing stage data yet — most
// visible on a brand-new branch's first build. EndTime disambiguates: Jenkins
// only sets it once a run has actually finished. Lifecycle semantics live in
// buildstatus.PipelineStatus.
func (r RunRecord) IsTerminal() bool {
	return r.Status.IsTerminal(r.EndTime)
}

// FavoriteRecord represents a favorited Jenkins job or folder.
type FavoriteRecord struct {
	URL       string `json:"url"`        // full Jenkins job/folder URL
	ServerID  string `json:"server_id"`  // Jenkins connection that owns this favorite
	Name      string `json:"name"`       // human-readable, e.g. "folder ⟩ my-pipeline"
	Kind      string `json:"kind"`       // "job" | "folder" | "multibranch"
	CreatedAt int64  `json:"created_at"` // millis since epoch, when favorited
}
