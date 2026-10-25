package poll

import (
	"context"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/store"
)

func TestQueueLessCorrelationCreatesBackgroundExactRunDemand(t *testing.T) {
	runStore := store.NewForServer(t.TempDir(), "server-a")
	defer func() { _ = runStore.Close() }()
	manager := newManager(Config{}, noopExecutor{})
	manager.repo = storeRepository{store: runStore}
	defer manager.Stop()

	jobURL := "http://jenkins/job/demo/"
	intent := TriggerIntent{
		IntentID:      "replay-1",
		JobURL:        jobURL,
		State:         TriggerPrepared,
		CreatedAt:     time.Now(),
		ExpiresAt:     time.Now().Add(time.Hour),
		BaselineBuild: 41,
	}
	if err := manager.Track(context.Background(), intent); err != nil {
		t.Fatal(err)
	}
	windowTarget := Target{Kind: TargetJobWindow, JobURL: jobURL}
	demand, ok := manager.watches.demand(windowTarget)
	if !ok {
		t.Fatal("queue-less trigger did not create job-window demand")
	}

	observation := Observation{
		Target:     windowTarget,
		ObservedAt: time.Now(),
		Runs: []RunSnapshot{{
			BuildNumber: 42,
			Status:      buildstatus.StatusInProgress,
			Activity:    buildstatus.ActivityRunning,
		}},
	}
	if err := manager.applyWindow(context.Background(), demand, observation, true); err != nil {
		t.Fatal(err)
	}

	exactTarget := Target{Kind: TargetExactRun, JobURL: jobURL, BuildNumber: 42}
	manager.internalMu.Lock()
	_, watching := manager.internalWatches[exactTarget]
	manager.internalMu.Unlock()
	if !watching {
		t.Fatal("correlated active run has no manager-owned exact watch; polling depends on visiting a page")
	}

	// The next reconciliation releases trigger-window demand because the store
	// resolved the queue-less intent. Exact-run demand must survive that release.
	if err := manager.reconcileTriggers(context.Background(), jobURL); err != nil {
		t.Fatal(err)
	}
	if _, ok := manager.watches.demand(windowTarget); ok {
		t.Fatal("resolved trigger retained manager-owned job-window demand")
	}
	if _, ok := manager.watches.demand(exactTarget); !ok {
		t.Fatal("background exact-run polling stopped after trigger reconciliation")
	}
}
