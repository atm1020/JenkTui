package poll

import (
	"context"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/store"
)

func TestCompletionPersistsBeforeDeliveryAndRetriesAfterRestart(t *testing.T) {
	runStore := store.NewForServer(t.TempDir(), "server-a")
	t.Cleanup(func() { _ = runStore.Close() })
	jobURL := "http://jenkins/job/demo/"
	if err := runStore.AddRun(store.RunRecord{ID: jobURL + "#42", JobURL: jobURL, BuildID: "42", Status: buildstatus.StatusInProgress}); err != nil {
		t.Fatal(err)
	}

	repo := storeRepository{store: runStore}
	changed, err := repo.apply(context.Background(), Observation{Target: Target{Kind: TargetExactRun, JobURL: jobURL, BuildNumber: 42}, ObservedAt: time.Now(), Runs: []RunSnapshot{{BuildNumber: 42, Status: buildstatus.StatusSuccess, Activity: buildstatus.ActivityFinished, EndTimeMillis: 2}}})
	if err != nil || !changed {
		t.Fatalf("apply = %v, %v", changed, err)
	}
	if pending, err := runStore.PendingCompletionCount(context.Background()); err != nil || pending != 1 {
		t.Fatalf("pending before manager = %d, %v", pending, err)
	}

	first := newManager(Config{PollInterval: time.Hour}, noopExecutor{})
	first.repo = repo
	first.Start()
	delivery := nextDelivery(t, first)
	if len(delivery.Batch.Completions) != 1 || delivery.Batch.Completions[0].Key.BuildNumber != 42 {
		t.Fatalf("delivery = %#v", delivery)
	}
	first.Stop() // no acknowledgement simulates process exit after delivery

	second := newManager(Config{PollInterval: time.Hour}, noopExecutor{})
	second.repo = repo
	second.Start()
	redelivery := nextDelivery(t, second)
	if len(redelivery.Batch.Completions) != 1 {
		t.Fatalf("restart delivery = %#v", redelivery)
	}
	if err := redelivery.Ack(context.Background(), []CompletionKey{redelivery.Batch.Completions[0].Key}); err != nil {
		t.Fatal(err)
	}
	second.Stop()
	if pending, err := runStore.PendingCompletionCount(context.Background()); err != nil || pending != 0 {
		t.Fatalf("pending after ack = %d, %v", pending, err)
	}
}

func TestAggregateCoalescesSnapshotsUnderBackpressure(t *testing.T) {
	manager := newManager(Config{PollInterval: time.Hour}, noopExecutor{})
	manager.Start()
	defer manager.Stop()
	target := Target{Kind: TargetExactRun, JobURL: "http://jenkins/job/demo/", BuildNumber: 42}
	watch, _, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatal(err)
	}
	defer watch.Release()
	demand, _ := manager.watches.demand(target)
	for number := BuildNumber(1); number <= 20; number++ {
		manager.publishSnapshot(Snapshot{Target: target, Generation: demand.Generation, Runs: []RunSnapshot{{BuildNumber: number}}})
	}
	delivery := nextDelivery(t, manager)
	// A delivery may race with publishers, but each aggregate contains at most
	// one replaceable snapshot for the target and a later delivery reaches 20.
	latest := delivery.Batch.Snapshots[0].Runs[0].BuildNumber
	deadline := time.After(time.Second)
	for latest != 20 {
		select {
		case next := <-manager.Results():
			latest = next.Batch.Snapshots[0].Runs[0].BuildNumber
		case <-deadline:
			t.Fatalf("latest snapshot = %d, want 20", latest)
		}
	}
}

func nextDelivery(t *testing.T, manager *Manager) Delivery {
	t.Helper()
	select {
	case delivery := <-manager.Results():
		return delivery
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for delivery")
		return Delivery{}
	}
}
