package store

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s := New(t.TempDir())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func sampleRun(id string) RunRecord {
	return RunRecord{
		ID:        id,
		JobName:   "folder/job",
		JobURL:    "http://jenkins/job/folder/job/job/",
		BuildID:   "1",
		Status:    "IN_PROGRESS",
		StartTime: 1000,
		CreatedAt: 2000,
	}
}

func TestApplyRunObservationUpdatesTrackedRunsAtomically(t *testing.T) {
	s := newTestStore(t)
	jobURL := "http://jenkins/job/demo/"
	if err := s.AddRun(RunRecord{
		ID: jobURL + "#42", JobName: "demo", JobURL: jobURL, BuildID: "42",
		Status: "IN_PROGRESS", StartTime: 1000, CreatedAt: 1000,
	}); err != nil {
		t.Fatal(err)
	}

	result, err := s.ApplyRunObservation(context.Background(), RunObservationBatch{
		JobURL:  jobURL,
		JobName: "demo",
		Runs: []ObservedRun{
			{BuildID: "42", Status: "SUCCESS", StartTime: 1000, EndTime: 2000, Duration: 1000},
			{BuildID: "43", Status: "IN_PROGRESS", StartTime: 3000},
		},
	})
	if err != nil {
		t.Fatalf("ApplyRunObservation: %v", err)
	}
	if !result.Changed {
		t.Fatal("tracked terminal update was not reported as changed")
	}

	runs, err := s.GetAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].BuildID != "42" || runs[0].Status != "SUCCESS" {
		t.Fatalf("runs = %#v, want only updated tracked run 42", runs)
	}

	result, err = s.ApplyRunObservation(context.Background(), RunObservationBatch{
		JobURL:  jobURL,
		JobName: "demo",
		Runs: []ObservedRun{{
			BuildID: "42", Status: "SUCCESS", StartTime: 1000, EndTime: 2000, Duration: 1000,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("repeated identical observation was reported as changed")
	}

	result, err = s.ApplyRunObservation(context.Background(), RunObservationBatch{
		JobURL: jobURL,
		Runs: []ObservedRun{{
			BuildID: "42", Status: "IN_PROGRESS", StartTime: 1000,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("stale active observation regressed a committed terminal run")
	}
}

func TestTriggerIntentPersistsPreparedAndAcceptedQueueReceiptAcrossRestart(t *testing.T) {
	dir := t.TempDir()
	s := NewForServer(dir, "server-a")
	intent := TriggerIntentRecord{
		IntentID: "intent-1", JobURL: "http://jenkins/job/demo/", BaselineBuild: 41,
		State: TriggerPrepared, CreatedAt: 1000, ExpiresAt: 61000,
	}
	if err := s.TrackTriggerIntent(context.Background(), intent); err != nil {
		t.Fatalf("prepare: %v", err)
	}
	intent.State = TriggerAccepted
	intent.QueueID = QueueID(9001)
	intent.AcceptedAt = 2000
	if err := s.TrackTriggerIntent(context.Background(), intent); err != nil {
		t.Fatalf("accept: %v", err)
	}
	retriedPrepare := intent
	retriedPrepare.State = TriggerPrepared
	retriedPrepare.QueueID = nil
	retriedPrepare.AcceptedAt = 0
	if err := s.TrackTriggerIntent(context.Background(), retriedPrepare); err != nil {
		t.Fatalf("retry prepare: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s = NewForServer(dir, "server-a")
	t.Cleanup(func() { _ = s.Close() })
	pending, err := s.UnresolvedTriggerIntents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("unresolved = %#v, want one accepted intent", pending)
	}
	if pending[0].State != TriggerAccepted || pending[0].QueueID == nil || *pending[0].QueueID != 9001 || pending[0].BaselineBuild != 41 {
		t.Fatalf("reloaded intent = %#v", pending[0])
	}
}

func TestQueueReceiptsResolveConcurrentIntentsToIndependentExactRuns(t *testing.T) {
	s := newTestStore(t)
	jobURL := "http://jenkins/job/demo/"
	for _, intent := range []TriggerIntentRecord{
		{IntentID: "intent-a", JobURL: jobURL, QueueID: QueueID(9001), State: TriggerAccepted, CreatedAt: 1000, ExpiresAt: 61000},
		{IntentID: "intent-b", JobURL: jobURL, QueueID: QueueID(9002), State: TriggerAccepted, CreatedAt: 1001, ExpiresAt: 61000},
	} {
		if err := s.TrackTriggerIntent(context.Background(), intent); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.FinishTriggerIntent(context.Background(), "intent-b", TriggerResolved, "43", 2000); err != nil {
		t.Fatal(err)
	}
	if err := s.FinishTriggerIntent(context.Background(), "intent-a", TriggerResolved, "42", 2001); err != nil {
		t.Fatal(err)
	}
	runs, err := s.GetInProgressRuns()
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, run := range runs {
		got[run.BuildID] = true
	}
	if len(runs) != 2 || !got["42"] || !got["43"] {
		t.Fatalf("resolved runs = %#v, want independent builds 42 and 43", runs)
	}
	if pending, err := s.UnresolvedTriggerIntents(context.Background()); err != nil || len(pending) != 0 {
		t.Fatalf("unresolved = %#v, %v", pending, err)
	}
}

func TestQueueLessReplayIntentStoresNullQueueID(t *testing.T) {
	s := newTestStore(t)
	if err := s.TrackTriggerIntent(context.Background(), TriggerIntentRecord{
		IntentID: "replay-1", JobURL: "http://jenkins/job/demo/", BaselineBuild: 41,
		State: TriggerAccepted, CreatedAt: 1000, AcceptedAt: 1100, ExpiresAt: 61000,
	}); err != nil {
		t.Fatal(err)
	}

	var raw sql.NullInt64
	if err := s.db.QueryRowContext(t.Context(), `SELECT queue_id FROM trigger_intents WHERE intent_id = ?`, "replay-1").Scan(&raw); err != nil {
		t.Fatal(err)
	}
	if raw.Valid {
		t.Fatalf("queue_id = %v, want NULL for a replay with no Jenkins queue receipt", raw.Int64)
	}

	pending, err := s.UnresolvedTriggerIntents(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 || pending[0].QueueID != nil {
		t.Fatalf("unresolved = %#v, want nil QueueID in memory", pending)
	}
}

func TestPreparedTriggerFallbackCorrelatesTerminalRunWithoutInventingCompletion(t *testing.T) {
	s := newTestStore(t)
	jobURL := "http://jenkins/job/demo/"
	if err := s.TrackTriggerIntent(context.Background(), TriggerIntentRecord{
		IntentID: "intent-1", JobURL: jobURL, BaselineBuild: 41,
		State: TriggerPrepared, CreatedAt: 1000, ExpiresAt: 61000,
	}); err != nil {
		t.Fatal(err)
	}

	result, err := s.ApplyRunObservation(context.Background(), RunObservationBatch{
		JobURL: jobURL, JobName: "demo", ObservedAt: 2000,
		Runs: []ObservedRun{{BuildID: "42", Status: "SUCCESS", EndTime: 1900}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completions) != 0 {
		t.Fatalf("completions = %#v, want no transition for first terminal observation", result.Completions)
	}
	if pending, err := s.UnresolvedTriggerIntents(context.Background()); err != nil || len(pending) != 0 {
		t.Fatalf("unresolved after correlation = %#v, %v", pending, err)
	}
	runs, err := s.GetAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].BuildID != "42" || !runs[0].IsTerminal() {
		t.Fatalf("tracked runs = %#v, want terminal build 42", runs)
	}
}

func TestTriggerFallbackRejectsRunThatPredatesIntent(t *testing.T) {
	s := newTestStore(t)
	jobURL := "http://jenkins/job/demo/"
	createdAt := int64(1_000_000)
	if err := s.TrackTriggerIntent(context.Background(), TriggerIntentRecord{
		IntentID: "intent-1", JobURL: jobURL, BaselineBuild: 41,
		State: TriggerPrepared, CreatedAt: createdAt, ExpiresAt: createdAt + 60_000,
	}); err != nil {
		t.Fatal(err)
	}
	result, err := s.ApplyRunObservation(context.Background(), RunObservationBatch{
		JobURL: jobURL, JobName: "demo", ObservedAt: createdAt + 1000,
		Runs: []ObservedRun{{BuildID: "42", Status: "IN_PROGRESS", StartTime: createdAt - int64(10*time.Minute/time.Millisecond)}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Changed {
		t.Fatal("fallback claimed a run that predates the trigger intent")
	}
	if pending, err := s.UnresolvedTriggerIntents(context.Background()); err != nil || len(pending) != 1 {
		t.Fatalf("unresolved = %#v, %v", pending, err)
	}
}

func TestQueueLessReplayBurstShowsNewBuildsInQueryRuns(t *testing.T) {
	// Script replay cannot return a Jenkins queue ID; PipelineRun.Replay
	// only reports success. Rapid replays therefore leave several queue-less
	// accepted intents. My Runs reads QueryRuns, so those new builds must
	// still appear there once Jenkins reports them — otherwise a long job
	// like mobile-app gets stuck after a replay burst.
	s := newTestStore(t)
	jobURL := "http://jenkins/job/DevTeam/job/Frontend/job/mobile-app/"
	for i, intent := range []TriggerIntentRecord{
		{IntentID: "replay-1", JobURL: jobURL, BaselineBuild: 206, State: TriggerAccepted, CreatedAt: 1000, AcceptedAt: 1100, ExpiresAt: 61000},
		{IntentID: "replay-2", JobURL: jobURL, BaselineBuild: 207, State: TriggerAccepted, CreatedAt: 1200, AcceptedAt: 1300, ExpiresAt: 61000},
	} {
		if err := s.TrackTriggerIntent(context.Background(), intent); err != nil {
			t.Fatalf("track replay %d: %v", i+1, err)
		}
	}

	if _, err := s.ApplyRunObservation(context.Background(), RunObservationBatch{
		JobURL: jobURL, JobName: "DevTeam ⟩ Frontend ⟩ mobile-app", ObservedAt: 2000,
		Runs: []ObservedRun{
			{BuildID: "207", Status: "IN_PROGRESS", StartTime: 1400},
			{BuildID: "208", Status: "IN_PROGRESS", StartTime: 1500},
		},
	}); err != nil {
		t.Fatal(err)
	}

	runs, err := s.QueryRuns(RunQuery{})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	got := map[string]bool{}
	for _, run := range runs {
		got[run.BuildID] = true
	}
	if !got["207"] || !got["208"] {
		t.Fatalf("QueryRuns after queue-less replay burst = %#v, want builds 207 and 208", runs)
	}
}

func TestTriggerFallbackClaimsOneBuildAndLeavesLeftoverIntent(t *testing.T) {
	// Two queue-less intents and one new build: claim the oldest intent so the
	// run appears in My Runs, and leave the leftover intent for a later build.
	s := newTestStore(t)
	jobURL := "http://jenkins/job/demo/"
	for _, intent := range []TriggerIntentRecord{
		{IntentID: "intent-a", JobURL: jobURL, BaselineBuild: 41, State: TriggerPrepared, CreatedAt: 1000, ExpiresAt: 61000},
		{IntentID: "intent-b", JobURL: jobURL, BaselineBuild: 41, State: TriggerPrepared, CreatedAt: 1100, ExpiresAt: 61000},
	} {
		if err := s.TrackTriggerIntent(context.Background(), intent); err != nil {
			t.Fatal(err)
		}
	}
	result, err := s.ApplyRunObservation(context.Background(), RunObservationBatch{
		JobURL: jobURL, JobName: "demo", ObservedAt: 2000,
		Runs: []ObservedRun{{BuildID: "42", Status: "IN_PROGRESS"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Changed {
		t.Fatal("oldest queue-less intent did not claim the one new build")
	}
	runs, err := s.GetAll()
	if err != nil || len(runs) != 1 || runs[0].BuildID != "42" {
		t.Fatalf("tracked runs = %#v, want only build 42", runs)
	}
	pending, err := s.UnresolvedTriggerIntents(context.Background())
	if err != nil || len(pending) != 1 || pending[0].IntentID != "intent-b" {
		t.Fatalf("unresolved leftover = %#v, %v, want intent-b", pending, err)
	}
}

func TestCompletionOutboxSurvivesRestartAndAcknowledgesOnce(t *testing.T) {
	dir := t.TempDir()
	jobURL := "http://jenkins/job/demo/"
	s := NewForServer(dir, "server-a")
	if err := s.AddRun(RunRecord{
		ID: jobURL + "#42", JobName: "demo", JobURL: jobURL, BuildID: "42",
		Status: "IN_PROGRESS", StartTime: 1000, CreatedAt: 1000,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s = NewForServer(dir, "server-a")
	result, err := s.ApplyRunObservation(context.Background(), RunObservationBatch{
		JobURL: jobURL,
		Runs: []ObservedRun{{
			BuildID: "42", Status: "SUCCESS", StartTime: 1000, EndTime: 2000, Duration: 1000,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completions) != 1 || result.Completions[0].Key.BuildID != "42" {
		t.Fatalf("completions = %#v, want build 42", result.Completions)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	s = NewForServer(dir, "server-a")
	pendingAfterCrash, err := s.PendingCompletions(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingAfterCrash) != 1 {
		t.Fatalf("pending after unacknowledged restart = %#v, want one redelivery", pendingAfterCrash)
	}

	result, err = s.ApplyRunObservation(context.Background(), RunObservationBatch{
		JobURL: jobURL,
		Runs: []ObservedRun{{
			BuildID: "42", Status: "SUCCESS", StartTime: 1000, EndTime: 2000, Duration: 1000,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Completions) != 1 {
		t.Fatalf("pending completion count after repeat = %d, want one unique fact", len(result.Completions))
	}
	key := result.Completions[0].Key
	if err := s.AckCompletions(context.Background(), []CompletionKey{key}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s = NewForServer(dir, "server-a")
	defer func() { _ = s.Close() }()
	pending, err := s.PendingCompletions(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("pending completions after ack and restart = %#v, want none", pending)
	}
}

func TestApplyRunObservationTracksOnlyRunsAcceptedByTriggerBaseline(t *testing.T) {
	s := newTestStore(t)
	jobURL := "http://jenkins/job/demo/"
	result, err := s.ApplyRunObservation(context.Background(), RunObservationBatch{
		JobURL:       jobURL,
		JobName:      "demo",
		TrackNewRuns: true,
		Baseline:     42,
		ObservedAt:   5000,
		Runs: []ObservedRun{
			{BuildID: "40", Status: "SUCCESS", EndTime: 4000},
			{BuildID: "43", Status: "FAILED", StartTime: 4500, EndTime: 4600, Duration: 100},
		},
	})
	if err != nil {
		t.Fatalf("ApplyRunObservation: %v", err)
	}
	if !result.Changed {
		t.Fatal("new trigger-correlated run was not reported as changed")
	}
	runs, err := s.GetAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 1 || runs[0].BuildID != "43" {
		t.Fatalf("runs = %#v, want only build 43 above baseline", runs)
	}
}

func TestAddRun_AndGetAll_NewestFirst(t *testing.T) {
	s := newTestStore(t)

	older := sampleRun("http://a/#1")
	older.CreatedAt = 100
	newer := sampleRun("http://a/#2")
	newer.CreatedAt = 200

	if err := s.AddRun(older); err != nil {
		t.Fatalf("AddRun older: %v", err)
	}
	if err := s.AddRun(newer); err != nil {
		t.Fatalf("AddRun newer: %v", err)
	}

	runs, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("len(runs) = %d, want 2", len(runs))
	}
	if runs[0].ID != newer.ID || runs[1].ID != older.ID {
		t.Errorf("order = [%s %s], want [%s %s] (created_at DESC)",
			runs[0].ID, runs[1].ID, newer.ID, older.ID)
	}
}

// TestGetAll_DecodesHistoricalPercentEncodedJobName covers rows written
// before jobref.ToDisplayName learned to decode Jenkins' percent-encoded
// branch names (e.g. a multibranch pipeline branch "merge-queue/PR-58"
// persisted as the literal "merge-queue%2FPR-58"). Those rows already exist
// in users' databases, so reads must decode them without a migration.
func TestGetAll_DecodesHistoricalPercentEncodedJobName(t *testing.T) {
	s := newTestStore(t)

	rec := sampleRun("http://a/#1")
	rec.JobName = "merge-queue-test  merge-queue%2FPR-58"
	if err := s.AddRun(rec); err != nil {
		t.Fatalf("AddRun: %v", err)
	}

	runs, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	want := "merge-queue-test  merge-queue/PR-58"
	if runs[0].JobName != want {
		t.Errorf("JobName = %q, want %q", runs[0].JobName, want)
	}
}

func TestAddRun_FillsServerIDFromStore(t *testing.T) {
	s := NewForServer(t.TempDir(), "srv-1")
	t.Cleanup(func() { _ = s.Close() })

	rec := sampleRun("http://a/#1")
	if err := s.AddRun(rec); err != nil {
		t.Fatalf("AddRun: %v", err)
	}

	runs, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(runs) != 1 || runs[0].ServerID != "srv-1" {
		t.Errorf("runs = %+v, want one run with ServerID srv-1", runs)
	}
}

func TestAddRun_DuplicateReturnsError(t *testing.T) {
	s := newTestStore(t)

	rec := sampleRun("http://a/#1")
	if err := s.AddRun(rec); err != nil {
		t.Fatalf("first AddRun: %v", err)
	}
	if err := s.AddRun(rec); err == nil {
		t.Error("second AddRun with same (id, server_id): want constraint error, got nil")
	}
}

func TestAddRunIfNotExists_Idempotent(t *testing.T) {
	s := newTestStore(t)

	rec := sampleRun("http://a/#1")
	added, err := s.AddRunIfNotExists(rec)
	if err != nil || !added {
		t.Fatalf("first AddRunIfNotExists = (%v, %v), want (true, nil)", added, err)
	}
	added, err = s.AddRunIfNotExists(rec)
	if err != nil || added {
		t.Fatalf("second AddRunIfNotExists = (%v, %v), want (false, nil)", added, err)
	}

	runs, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("len(runs) = %d, want 1", len(runs))
	}
}

func TestUpdateRun_UpdatesFields(t *testing.T) {
	s := newTestStore(t)

	rec := sampleRun("http://a/#1")
	if err := s.AddRun(rec); err != nil {
		t.Fatalf("AddRun: %v", err)
	}
	if err := s.UpdateRun(rec.ID, "SUCCESS", 5000, 4000, 1000); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	runs, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	got := runs[0]
	if got.Status != "SUCCESS" || got.EndTime != 5000 || got.Duration != 4000 || got.StartTime != 1000 {
		t.Errorf("run = %+v, want status SUCCESS end 5000 duration 4000 start 1000", got)
	}
}

// Callers pass startTime=0 when a poll snapshot has no start time yet
// (e.g. queued builds); that must not zero out a known start time.
func TestUpdateRun_ZeroStartTimeKeepsExisting(t *testing.T) {
	s := newTestStore(t)

	rec := sampleRun("http://a/#1")
	rec.StartTime = 1234
	if err := s.AddRun(rec); err != nil {
		t.Fatalf("AddRun: %v", err)
	}
	if err := s.UpdateRun(rec.ID, "SUCCESS", 5000, 4000, 0); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	runs, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if runs[0].StartTime != 1234 {
		t.Errorf("StartTime = %d, want 1234 preserved", runs[0].StartTime)
	}
}

func TestUpdateRun_MissingIDIsNoOp(t *testing.T) {
	s := newTestStore(t)
	if err := s.UpdateRun("nope#1", "SUCCESS", 1, 1, 1); err != nil {
		t.Errorf("UpdateRun on missing ID = %v, want nil", err)
	}
}

func TestUpdateRunFull_RewritesPendingRun(t *testing.T) {
	s := newTestStore(t)

	pending := sampleRun("http://a/#pending-123")
	pending.BuildID = ""
	if err := s.AddRun(pending); err != nil {
		t.Fatalf("AddRun: %v", err)
	}

	err := s.UpdateRunFull(pending.ID, "http://a/#42", "42", "SUCCESS", 1000, 5000, 4000)
	if err != nil {
		t.Fatalf("UpdateRunFull: %v", err)
	}

	runs, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1", len(runs))
	}
	got := runs[0]
	if got.ID != "http://a/#42" || got.BuildID != "42" || got.Status != "SUCCESS" ||
		got.StartTime != 1000 || got.EndTime != 5000 || got.Duration != 4000 {
		t.Errorf("run = %+v, want rewritten real build", got)
	}
}

// If poll already persisted the real build row before the pending row
// resolves, rewriting the ID would violate the PK. The pending row is
// stale: drop it, keep the real one.
func TestUpdateRunFull_ConflictDeletesPendingRow(t *testing.T) {
	s := newTestStore(t)

	pending := sampleRun("http://a/#pending-123")
	pending.BuildID = ""
	if err := s.AddRun(pending); err != nil {
		t.Fatalf("AddRun pending: %v", err)
	}
	real := sampleRun("http://a/#42")
	real.BuildID = "42"
	real.Status = "IN_PROGRESS"
	if err := s.AddRun(real); err != nil {
		t.Fatalf("AddRun real: %v", err)
	}

	err := s.UpdateRunFull(pending.ID, real.ID, "42", "SUCCESS", 1000, 5000, 4000)
	if err != nil {
		t.Fatalf("UpdateRunFull with conflict = %v, want nil", err)
	}

	runs, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != real.ID {
		t.Errorf("runs = %+v, want only the real build row", runs)
	}
}

func TestDeleteRun_RemovesRecord(t *testing.T) {
	s := newTestStore(t)

	rec := sampleRun("http://a/#1")
	if err := s.AddRun(rec); err != nil {
		t.Fatalf("AddRun: %v", err)
	}
	if err := s.DeleteRun(rec.ID); err != nil {
		t.Fatalf("DeleteRun: %v", err)
	}

	runs, err := s.GetAll()
	if err != nil {
		t.Fatalf("GetAll: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("len(runs) = %d, want 0", len(runs))
	}
}

func TestDeleteRun_MissingIDIsNoOp(t *testing.T) {
	s := newTestStore(t)
	if err := s.DeleteRun("nope#1"); err != nil {
		t.Errorf("DeleteRun on missing ID = %v, want nil", err)
	}
}

func TestGetAll_ScopedToServer(t *testing.T) {
	dir := t.TempDir()
	s1 := NewForServer(dir, "srv-1")
	t.Cleanup(func() { _ = s1.Close() })
	s2 := NewForServer(dir, "srv-2")
	t.Cleanup(func() { _ = s2.Close() })

	a := sampleRun("http://a/#1")
	if err := s1.AddRun(a); err != nil {
		t.Fatalf("s1.AddRun: %v", err)
	}
	b := sampleRun("http://b/#1")
	if err := s2.AddRun(b); err != nil {
		t.Fatalf("s2.AddRun: %v", err)
	}

	runs1, err := s1.GetAll()
	if err != nil {
		t.Fatalf("s1.GetAll: %v", err)
	}
	if len(runs1) != 1 || runs1[0].JobURL != a.JobURL {
		t.Errorf("s1 runs = %+v, want only srv-1 run", runs1)
	}

	runs2, err := s2.GetAll()
	if err != nil {
		t.Fatalf("s2.GetAll: %v", err)
	}
	if len(runs2) != 1 || runs2[0].JobURL != b.JobURL {
		t.Errorf("s2 runs = %+v, want only srv-2 run", runs2)
	}
}

func TestGetInProgressRuns_FiltersTerminal(t *testing.T) {
	s := newTestStore(t)

	for i, status := range []string{"IN_PROGRESS", "SUCCESS", "QUEUED", "FAILED", "PENDING", "ABORTED", "UNSTABLE", "NOT_EXECUTED"} {
		rec := sampleRun(status + "#1")
		rec.Status = buildstatus.PipelineStatus(status)
		rec.CreatedAt = int64(i)
		if status == "NOT_EXECUTED" {
			// A genuinely-finished NOT_EXECUTED run (e.g. a Jenkinsfile parse
			// error) has an end time — see TestGetInProgressRuns_NotExecutedWithoutEndTimeIsInProgress
			// for the unfinished case.
			rec.EndTime = 5000
		}
		if err := s.AddRun(rec); err != nil {
			t.Fatalf("AddRun %s: %v", status, err)
		}
	}

	runs, err := s.GetInProgressRuns()
	if err != nil {
		t.Fatalf("GetInProgressRuns: %v", err)
	}
	if len(runs) != 3 {
		t.Fatalf("len(runs) = %d, want 3 (IN_PROGRESS, QUEUED, PENDING)", len(runs))
	}
	for _, r := range runs {
		if r.IsTerminal() {
			t.Errorf("terminal status %q in in-progress results", r.Status)
		}
	}
}

// TestGetInProgressRuns_NotExecutedWithoutEndTimeIsInProgress covers a
// brand-new build caught by a poll before wfapi starts reporting real
// stage/status data — it can transiently read NOT_EXECUTED with no end time.
// That must not count as terminal, or the row becomes permanently stuck:
// GetRunsByJobURL excludes terminal rows, so later polls treat the build as
// never-seen and AddRunIfNotExists silently no-ops instead of correcting it.
func TestGetInProgressRuns_NotExecutedWithoutEndTimeIsInProgress(t *testing.T) {
	s := newTestStore(t)

	rec := sampleRun("http://jenkins/job/new-branch/#1")
	rec.Status = "NOT_EXECUTED"
	rec.EndTime = 0
	if err := s.AddRun(rec); err != nil {
		t.Fatalf("AddRun: %v", err)
	}

	runs, err := s.GetInProgressRuns()
	if err != nil {
		t.Fatalf("GetInProgressRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("len(runs) = %d, want 1 (NOT_EXECUTED with no end time is still in progress)", len(runs))
	}
}

func TestGetRunsByJobURL_NonTerminalKeyedByBuildID(t *testing.T) {
	s := newTestStore(t)

	jobURL := "http://jenkins/job/a/"
	running := sampleRun(jobURL + "#1")
	running.JobURL = jobURL
	running.BuildID = "1"
	running.Status = "IN_PROGRESS"
	done := sampleRun(jobURL + "#2")
	done.JobURL = jobURL
	done.BuildID = "2"
	done.Status = "SUCCESS"
	otherJob := sampleRun("http://jenkins/job/b/#1")
	otherJob.JobURL = "http://jenkins/job/b/"
	otherJob.BuildID = "1"
	otherJob.Status = "IN_PROGRESS"

	for _, r := range []RunRecord{running, done, otherJob} {
		if err := s.AddRun(r); err != nil {
			t.Fatalf("AddRun %s: %v", r.ID, err)
		}
	}

	byBuild, err := s.GetRunsByJobURL(jobURL)
	if err != nil {
		t.Fatalf("GetRunsByJobURL: %v", err)
	}
	if len(byBuild) != 1 {
		t.Fatalf("len(byBuild) = %d, want 1 (only non-terminal run of job a)", len(byBuild))
	}
	if byBuild["1"] == nil || byBuild["1"].Status != "IN_PROGRESS" {
		t.Errorf("byBuild[1] = %+v, want the in-progress run keyed by build ID", byBuild["1"])
	}
}

// Data must survive closing and reopening the store — this is the whole
// point of moving to a real database file.
func TestPersistence_AcrossReopen(t *testing.T) {
	dir := t.TempDir()

	s := NewForServer(dir, "srv-1")
	rec := sampleRun("http://a/#1")
	if err := s.AddRun(rec); err != nil {
		t.Fatalf("AddRun: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2 := NewForServer(dir, "srv-1")
	t.Cleanup(func() { _ = s2.Close() })
	runs, err := s2.GetAll()
	if err != nil {
		t.Fatalf("GetAll after reopen: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != rec.ID {
		t.Errorf("runs after reopen = %+v, want the persisted run", runs)
	}
}

func TestNew_ConfiguresSingleConnectionAndMigratesSchema(t *testing.T) {
	s := newTestStore(t)

	if got := s.db.Stats().MaxOpenConnections; got != 1 {
		t.Errorf("MaxOpenConnections = %d, want 1", got)
	}

	var version int
	if err := s.db.QueryRowContext(t.Context(), `SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != 1 {
		t.Errorf("schema version = %d, want 1", version)
	}
}

func TestClose_AllowsCleanShutdown(t *testing.T) {
	s := New(t.TempDir())
	if err := s.Close(); err != nil {
		t.Errorf("Close = %v, want nil", err)
	}
}

func sampleFavorite(url string) FavoriteRecord {
	return FavoriteRecord{
		URL:       url,
		Name:      "folder ⟩ job",
		Kind:      "job",
		CreatedAt: 2000,
	}
}

func TestAddFavorite_AndGetAllFavorites_NewestFirst(t *testing.T) {
	s := newTestStore(t)

	older := sampleFavorite("http://a/job/x/")
	older.CreatedAt = 100
	newer := sampleFavorite("http://a/job/y/")
	newer.CreatedAt = 200

	if err := s.AddFavorite(older); err != nil {
		t.Fatalf("AddFavorite older: %v", err)
	}
	if err := s.AddFavorite(newer); err != nil {
		t.Fatalf("AddFavorite newer: %v", err)
	}

	favs, err := s.GetAllFavorites()
	if err != nil {
		t.Fatalf("GetAllFavorites: %v", err)
	}
	if len(favs) != 2 {
		t.Fatalf("len(favs) = %d, want 2", len(favs))
	}
	if favs[0].URL != newer.URL || favs[1].URL != older.URL {
		t.Errorf("order = [%s %s], want [%s %s] (created_at DESC)",
			favs[0].URL, favs[1].URL, newer.URL, older.URL)
	}
}

func TestAddFavorite_FillsServerIDFromStore(t *testing.T) {
	s := NewForServer(t.TempDir(), "srv-1")
	t.Cleanup(func() { _ = s.Close() })

	rec := sampleFavorite("http://a/job/x/")
	if err := s.AddFavorite(rec); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}

	favs, err := s.GetAllFavorites()
	if err != nil {
		t.Fatalf("GetAllFavorites: %v", err)
	}
	if len(favs) != 1 || favs[0].ServerID != "srv-1" {
		t.Errorf("favs = %+v, want one favorite with ServerID srv-1", favs)
	}
}

func TestAddFavorite_DuplicateReturnsError(t *testing.T) {
	s := newTestStore(t)

	rec := sampleFavorite("http://a/job/x/")
	if err := s.AddFavorite(rec); err != nil {
		t.Fatalf("first AddFavorite: %v", err)
	}
	if err := s.AddFavorite(rec); err == nil {
		t.Error("second AddFavorite with same (url, server_id): want constraint error, got nil")
	}
}

func TestIsFavorite_TrueAfterAdd_FalseAfterRemove(t *testing.T) {
	s := newTestStore(t)
	rec := sampleFavorite("http://a/job/x/")

	is, err := s.IsFavorite(rec.URL)
	if err != nil || is {
		t.Fatalf("IsFavorite before add = (%v, %v), want (false, nil)", is, err)
	}

	if err := s.AddFavorite(rec); err != nil {
		t.Fatalf("AddFavorite: %v", err)
	}
	is, err = s.IsFavorite(rec.URL)
	if err != nil || !is {
		t.Fatalf("IsFavorite after add = (%v, %v), want (true, nil)", is, err)
	}

	if err := s.RemoveFavorite(rec.URL); err != nil {
		t.Fatalf("RemoveFavorite: %v", err)
	}
	is, err = s.IsFavorite(rec.URL)
	if err != nil || is {
		t.Fatalf("IsFavorite after remove = (%v, %v), want (false, nil)", is, err)
	}
}

func TestRemoveFavorite_MissingURLIsNoOp(t *testing.T) {
	s := newTestStore(t)
	if err := s.RemoveFavorite("http://nope/"); err != nil {
		t.Errorf("RemoveFavorite on missing URL = %v, want nil", err)
	}
}

func TestGetAllFavorites_ScopedToServer(t *testing.T) {
	dir := t.TempDir()
	s1 := NewForServer(dir, "srv-1")
	t.Cleanup(func() { _ = s1.Close() })
	s2 := NewForServer(dir, "srv-2")
	t.Cleanup(func() { _ = s2.Close() })

	a := sampleFavorite("http://a/job/x/")
	if err := s1.AddFavorite(a); err != nil {
		t.Fatalf("s1.AddFavorite: %v", err)
	}
	b := sampleFavorite("http://b/job/y/")
	if err := s2.AddFavorite(b); err != nil {
		t.Fatalf("s2.AddFavorite: %v", err)
	}

	favs1, err := s1.GetAllFavorites()
	if err != nil {
		t.Fatalf("s1.GetAllFavorites: %v", err)
	}
	if len(favs1) != 1 || favs1[0].URL != a.URL {
		t.Errorf("s1 favorites = %+v, want only srv-1 favorite", favs1)
	}

	favs2, err := s2.GetAllFavorites()
	if err != nil {
		t.Fatalf("s2.GetAllFavorites: %v", err)
	}
	if len(favs2) != 1 || favs2[0].URL != b.URL {
		t.Errorf("s2 favorites = %+v, want only srv-2 favorite", favs2)
	}
}

func TestFavorites_SameURLDifferentServersIndependent(t *testing.T) {
	dir := t.TempDir()
	s1 := NewForServer(dir, "srv-1")
	t.Cleanup(func() { _ = s1.Close() })
	s2 := NewForServer(dir, "srv-2")
	t.Cleanup(func() { _ = s2.Close() })

	url := "http://shared/job/x/"
	if err := s1.AddFavorite(sampleFavorite(url)); err != nil {
		t.Fatalf("s1.AddFavorite: %v", err)
	}

	is1, err := s1.IsFavorite(url)
	if err != nil || !is1 {
		t.Fatalf("s1.IsFavorite = (%v, %v), want (true, nil)", is1, err)
	}
	is2, err := s2.IsFavorite(url)
	if err != nil || is2 {
		t.Fatalf("s2.IsFavorite = (%v, %v), want (false, nil) — independent server scope", is2, err)
	}
}

func TestMigrate_CurrentSchemaIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	s2 := New(dir)
	t.Cleanup(func() { _ = s2.Close() })

	var version int
	if err := s2.db.QueryRowContext(t.Context(), `SELECT version FROM schema_version`).Scan(&version); err != nil {
		t.Fatalf("read schema version: %v", err)
	}
	if version != 1 {
		t.Errorf("schema version = %d, want 1", version)
	}
	if _, err := s2.GetAllFavorites(); err != nil {
		t.Errorf("GetAllFavorites after reopen: %v", err)
	}
}
