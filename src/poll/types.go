package poll

import (
	"context"
	"strconv"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
)

// TargetKind identifies the Jenkins resource that must remain fresh.
type TargetKind uint8

const (
	TargetUnknown TargetKind = iota
	TargetJobWindow
	TargetExactRun
)

// BuildNumber is the positive numeric identity Jenkins assigns to one build.
// The Jenkins adapter must reject unsupported wire IDs before constructing it.
type BuildNumber int64

// Target is the stable identity shared by demand, polling, and delivery.
// BuildNumber is zero for a job window and positive for an exact run.
type Target struct {
	Kind        TargetKind
	JobURL      string
	BuildNumber BuildNumber
}

// Observation is a Jenkins-client-independent view of one target at a point in time.
type Observation struct {
	Target     Target
	ObservedAt time.Time
	Runs       []RunSnapshot
}

// Snapshot is replayable target state. It is not a completion event.
type Snapshot struct {
	Target     Target
	Generation uint64
	ObservedAt time.Time
	Runs       []RunSnapshot
}

// Replay contains the latest in-process snapshot when one is available.
type Replay struct {
	Snapshot Snapshot
	OK       bool
}

// CompletionKey is the durable identity of one completion fact.
type CompletionKey struct {
	ServerID    string
	JobURL      string
	BuildNumber BuildNumber
	Kind        string
}

// Completion is an active-to-terminal transition persisted in the outbox.
type Completion struct {
	Key     CompletionKey
	JobName string
	Status  buildstatus.PipelineStatus
	Run     RunSnapshot
}

type PollFailure struct {
	Target      Target
	At          time.Time
	Err         error
	Occurrences uint64
}

type Batch struct {
	Snapshots   []Snapshot
	Completions []Completion
	Failures    []PollFailure
	RunsChanged bool
}

// Delivery is one aggregate update. Ack must be called only for completion
// side effects that succeeded.
type Delivery struct {
	Batch Batch
	Ack   func(context.Context, []CompletionKey) error
}

// BuildID formats the numeric Jenkins build identity for UI and persistence boundaries.
func (n BuildNumber) BuildID() string { return strconv.FormatInt(int64(n), 10) }

// RunSnapshot contains the Jenkins run state needed by polling, persistence,
// and the UI without exposing a mutable gojenkins object.
type RunSnapshot struct {
	BuildNumber BuildNumber
	Name        string
	Status      buildstatus.PipelineStatus
	Activity    buildstatus.Activity

	StartTimeMillis int64
	EndTimeMillis   int64
	DurationMillis  int64
	Stages          []StageSnapshot
}

// StageSnapshot contains the live stage state shown by build views.
type StageSnapshot struct {
	ID              string
	Name            string
	Status          buildstatus.PipelineStatus
	ExecNode        string
	StartTimeMillis int64
	DurationMillis  int64
}

func cloneRuns(runs []RunSnapshot) []RunSnapshot {
	cloned := make([]RunSnapshot, len(runs))
	for i := range runs {
		cloned[i] = cloneRun(runs[i])
	}
	return cloned
}

func cloneRun(run RunSnapshot) RunSnapshot {
	run.Stages = append([]StageSnapshot(nil), run.Stages...)
	return run
}

func cloneSnapshot(snapshot Snapshot) Snapshot {
	snapshot.Runs = cloneRuns(snapshot.Runs)
	return snapshot
}
