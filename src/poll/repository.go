package poll

import (
	"context"
	"fmt"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/store"
)

type repository interface {
	apply(context.Context, Observation) (bool, error)
	activeRuns(context.Context) ([]Target, error)
	pendingCompletions(context.Context, int) ([]Completion, error)
	ackCompletions(context.Context, []CompletionKey) error
	trackTrigger(context.Context, TriggerIntent) error
	unresolvedTriggers(context.Context) ([]TriggerIntent, error)
	finishTrigger(context.Context, string, TriggerState, BuildNumber, time.Time) error
}

type storeRepository struct{ store *store.Store }

func (r storeRepository) apply(ctx context.Context, observation Observation) (bool, error) {
	runs := make([]store.ObservedRun, len(observation.Runs))
	for i := range observation.Runs {
		run := observation.Runs[i]
		runs[i] = store.ObservedRun{BuildID: run.BuildNumber.BuildID(), Status: run.Status, StartTime: run.StartTimeMillis, EndTime: run.EndTimeMillis, Duration: run.DurationMillis}
	}
	observedAt := observation.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	result, err := r.store.ApplyRunObservation(ctx, store.RunObservationBatch{JobURL: observation.Target.JobURL, JobName: jobref.ToDisplayName(observation.Target.JobURL), Runs: runs, ObservedAt: observedAt.UnixMilli()})
	return result.Changed, err
}

func (r storeRepository) activeRuns(ctx context.Context) ([]Target, error) {
	runs, err := r.store.GetInProgressRunsContext(ctx)
	if err != nil {
		return nil, err
	}
	targets := make([]Target, 0, len(runs))
	for i := range runs {
		number, parseErr := parseBuildNumber(runs[i].BuildID)
		if parseErr != nil {
			continue
		}
		targets = append(targets, Target{Kind: TargetExactRun, JobURL: runs[i].JobURL, BuildNumber: number})
	}
	return targets, nil
}

func (r storeRepository) pendingCompletions(ctx context.Context, limit int) ([]Completion, error) {
	records, err := r.store.PendingCompletions(ctx, limit)
	if err != nil {
		return nil, err
	}
	result := make([]Completion, 0, len(records))
	for i := range records {
		number, parseErr := parseBuildNumber(records[i].Key.BuildID)
		if parseErr != nil {
			return nil, fmt.Errorf("read completion build ID: %w", parseErr)
		}
		record := records[i]
		result = append(result, Completion{Key: CompletionKey{ServerID: record.Key.ServerID, JobURL: record.Key.JobURL, BuildNumber: number, Kind: record.Key.Kind}, JobName: record.JobName, Status: record.Status, Run: RunSnapshot{BuildNumber: number, Status: record.Status, Activity: buildstatus.ActivityFinished, StartTimeMillis: record.StartTime, EndTimeMillis: record.EndTime, DurationMillis: record.Duration}})
	}
	return result, nil
}

func (r storeRepository) ackCompletions(ctx context.Context, keys []CompletionKey) error {
	mapped := make([]store.CompletionKey, len(keys))
	for i := range keys {
		mapped[i] = store.CompletionKey{ServerID: keys[i].ServerID, JobURL: keys[i].JobURL, BuildID: keys[i].BuildNumber.BuildID(), Kind: keys[i].Kind}
	}
	return r.store.AckCompletions(ctx, mapped)
}

func (r storeRepository) trackTrigger(ctx context.Context, intent TriggerIntent) error {
	return r.store.TrackTriggerIntent(ctx, store.TriggerIntentRecord{IntentID: intent.IntentID, JobURL: intent.JobURL, BaselineBuild: int64(intent.BaselineBuild), QueueID: intent.QueueID, BuildID: persistenceBuildID(intent.BuildNumber), State: store.TriggerState(intent.State), CreatedAt: unixMillis(intent.CreatedAt), AcceptedAt: unixMillis(intent.AcceptedAt), ExpiresAt: unixMillis(intent.ExpiresAt), ResolvedAt: unixMillis(intent.ResolvedAt)})
}

func (r storeRepository) unresolvedTriggers(ctx context.Context) ([]TriggerIntent, error) {
	records, err := r.store.UnresolvedTriggerIntents(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]TriggerIntent, len(records))
	for i := range records {
		record := records[i]
		result[i] = TriggerIntent{IntentID: record.IntentID, JobURL: record.JobURL, BaselineBuild: BuildNumber(record.BaselineBuild), QueueID: record.QueueID, State: TriggerState(record.State), CreatedAt: fromUnixMillis(record.CreatedAt), AcceptedAt: fromUnixMillis(record.AcceptedAt), ExpiresAt: fromUnixMillis(record.ExpiresAt), ResolvedAt: fromUnixMillis(record.ResolvedAt)}
	}
	return result, nil
}

func (r storeRepository) finishTrigger(ctx context.Context, id string, state TriggerState, number BuildNumber, at time.Time) error {
	return r.store.FinishTriggerIntent(ctx, id, store.TriggerState(state), persistenceBuildID(number), at.UnixMilli())
}

func persistenceBuildID(number BuildNumber) string {
	if number <= 0 {
		return ""
	}
	return number.BuildID()
}

func unixMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

func fromUnixMillis(v int64) time.Time {
	if v == 0 {
		return time.Time{}
	}
	return time.UnixMilli(v)
}
