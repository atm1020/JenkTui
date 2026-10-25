package poll

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/atm1020/gojenkins"
	"github.com/atm1020/jenktui/src/store"
)

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

type TriggerIntent struct {
	IntentID                                     string
	JobURL                                       string
	BaselineBuild                                BuildNumber
	QueueID                                      *int64
	BuildNumber                                  BuildNumber
	State                                        TriggerState
	CreatedAt, AcceptedAt, ExpiresAt, ResolvedAt time.Time
}

// Records the highest build number already known for a Jenkins job.
func KnownBuildBaseline(job *gojenkins.Job) BuildNumber {
	if job == nil || job.GetDetails() == nil {
		return 0
	}
	details := job.GetDetails()
	latest := max(details.LastBuild.Number, details.NextBuildNumber-1)
	for i := range details.Builds {
		latest = max(latest, details.Builds[i].Number)
	}
	return BuildNumber(max(latest, 0))
}

func NewTriggerIntent(jobURL string, baseline BuildNumber, now time.Time) (TriggerIntent, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return TriggerIntent{}, fmt.Errorf("create trigger intent ID: %w", err)
	}
	if now.IsZero() {
		now = time.Now()
	}
	return TriggerIntent{IntentID: hex.EncodeToString(id[:]), JobURL: jobURL, BaselineBuild: baseline, State: TriggerPrepared, CreatedAt: now, ExpiresAt: now.Add(24 * time.Hour)}, nil
}

func (m *Manager) Track(ctx context.Context, intent TriggerIntent) error {
	if m.repo == nil {
		return errors.New("trigger tracking requires a repository")
	}
	if intent.State == "" {
		intent.State = TriggerPrepared
	}
	if intent.CreatedAt.IsZero() {
		intent.CreatedAt = time.Now()
	}
	if intent.ExpiresAt.IsZero() {
		intent.ExpiresAt = intent.CreatedAt.Add(24 * time.Hour)
	}
	if err := m.repo.trackTrigger(ctx, intent); err != nil {
		return err
	}
	if intent.State == TriggerPrepared || intent.State == TriggerAccepted {
		m.ensureInternal(Target{Kind: TargetJobWindow, JobURL: intent.JobURL})
	} else {
		m.refreshTriggerDemand(ctx, intent.JobURL)
	}
	return nil
}

type triggerTracker interface {
	Track(context.Context, TriggerIntent) error
}

func CoordinateTrigger(ctx context.Context, manager triggerTracker, intent TriggerIntent, invoke func(context.Context) (int64, error)) (int64, error) {
	if manager == nil {
		return 0, errors.New("poll manager is required")
	}
	intent.State = TriggerPrepared
	if err := manager.Track(ctx, intent); err != nil {
		return 0, fmt.Errorf("prepare trigger tracking: %w", err)
	}
	queueID, err := invoke(ctx)
	if err != nil {
		intent.State, intent.ResolvedAt = TriggerFailed, time.Now()
		if trackErr := manager.Track(ctx, intent); trackErr != nil {
			return 0, errors.Join(err, trackErr)
		}
		return 0, err
	}
	intent.State, intent.AcceptedAt = TriggerAccepted, time.Now()
	if queueID > 0 {
		intent.QueueID = store.QueueID(queueID)
	}
	if err := manager.Track(ctx, intent); err != nil {
		return queueID, fmt.Errorf("persist accepted queue receipt %d: %w", queueID, err)
	}
	return queueID, nil
}

func (m *Manager) ensureInternal(target Target) {
	m.internalMu.Lock()
	defer m.internalMu.Unlock()
	if _, ok := m.internalWatches[target]; ok {
		return
	}
	watch, _, err := m.Watch(WatchSpec{Target: target})
	if err == nil {
		m.internalWatches[target] = watch
	}
}

func (m *Manager) releaseInternal(target Target) {
	m.internalMu.Lock()
	watch := m.internalWatches[target]
	delete(m.internalWatches, target)
	m.internalMu.Unlock()
	if watch != nil {
		watch.Release()
	}
}

func (m *Manager) refreshTriggerDemand(ctx context.Context, jobURL string) {
	intents, err := m.repo.unresolvedTriggers(ctx)
	if err != nil {
		return
	}
	for _, intent := range intents {
		if intent.JobURL == jobURL {
			return
		}
	}
	m.releaseInternal(Target{Kind: TargetJobWindow, JobURL: jobURL})
}

func (m *Manager) loadDurableDemand() {
	if m.repo == nil {
		return
	}
	ctx, cancel := context.WithTimeout(m.ctx, m.config.RequestTimeout)
	defer cancel()
	if targets, err := m.repo.activeRuns(ctx); err == nil {
		for _, target := range targets {
			m.ensureInternal(target)
		}
	}
	if intents, err := m.repo.unresolvedTriggers(ctx); err == nil {
		for _, intent := range intents {
			m.ensureInternal(Target{Kind: TargetJobWindow, JobURL: intent.JobURL})
		}
	}
}
