package poll

import (
	"fmt"
	"testing"
)

func newWatchTestManager(t *testing.T) *Manager {
	t.Helper()
	manager := newManager(Config{}, noopExecutor{})
	manager.Start()
	t.Cleanup(manager.Stop)
	return manager
}

func TestWatchRejectsInvalidTarget(t *testing.T) {
	tests := []struct {
		name   string
		target Target
	}{
		{name: "empty job URL", target: Target{Kind: TargetJobWindow}},
		{name: "unknown kind", target: Target{Kind: TargetUnknown, JobURL: "https://jenkins/job/demo/"}},
		{name: "window with build", target: Target{Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/", BuildNumber: 42}},
		{name: "exact without build", target: Target{Kind: TargetExactRun, JobURL: "https://jenkins/job/demo/"}},
		{name: "exact with negative build", target: Target{Kind: TargetExactRun, JobURL: "https://jenkins/job/demo/", BuildNumber: -1}},
	}

	manager := newWatchTestManager(t)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			watch, replay, err := manager.Watch(WatchSpec{Target: tt.target})
			if err == nil || watch != nil || replay.OK {
				t.Fatalf("Watch() = %#v, %#v, %v; want validation error", watch, replay, err)
			}
		})
	}
}

func TestWatchAddsDemandWithoutColdReplay(t *testing.T) {
	manager := newWatchTestManager(t)

	target := Target{Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/"}
	watch, replay, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(watch.Release)
	if replay.OK {
		t.Fatal("cold watch unexpectedly returned replay")
	}

	demand, ok := manager.watches.demand(target)
	if !ok {
		t.Fatal("Watch did not create polling demand")
	}
	if !watch.Matches(Snapshot{Target: target, Generation: demand.Generation}) {
		t.Fatal("watch did not match its target generation")
	}
}

func TestWatchDoesNotMatchSnapshotForDifferentJobURL(t *testing.T) {
	manager := newWatchTestManager(t)
	target := Target{Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/"}
	watch, _, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(watch.Release)
	demand, ok := manager.watches.demand(target)
	if !ok {
		t.Fatal("Watch did not create polling demand")
	}

	snapshot := Snapshot{
		Target: Target{
			Kind:   TargetJobWindow,
			JobURL: "https://jenkins/job/another/",
		},
		Generation: demand.Generation,
	}
	if watch.Matches(snapshot) {
		t.Fatal("watch matched snapshot for a different job URL")
	}
}

func TestReleasedAndReacquiredTargetGetsNewGeneration(t *testing.T) {
	manager := newWatchTestManager(t)
	target := Target{Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/"}

	first, _, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("first Watch: %v", err)
	}
	firstDemand, _ := manager.watches.demand(target)
	first.Release()

	second, _, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("second Watch: %v", err)
	}
	defer second.Release()
	secondDemand, _ := manager.watches.demand(target)

	if secondDemand.Generation <= firstDemand.Generation {
		t.Fatalf("reacquired generation = %d, want newer than %d", secondDemand.Generation, firstDemand.Generation)
	}
	if second.Matches(Snapshot{Target: target, Generation: firstDemand.Generation}) {
		t.Fatal("reacquired watch matched stale generation")
	}
	if !second.Matches(Snapshot{Target: target, Generation: secondDemand.Generation}) {
		t.Fatal("reacquired watch did not match current generation")
	}
}

func TestStaleGenerationCannotReplaceReplay(t *testing.T) {
	manager := newWatchTestManager(t)
	target := Target{Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/"}

	first, _, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("first Watch: %v", err)
	}
	firstDemand, _ := manager.watches.demand(target)
	if !manager.watches.recordSnapshot(Snapshot{
		Target: target, Generation: firstDemand.Generation,
		Runs: []RunSnapshot{{BuildNumber: 1}},
	}) {
		t.Fatal("first-generation snapshot was rejected")
	}
	first.Release()

	second, _, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("second Watch: %v", err)
	}
	defer second.Release()
	secondDemand, _ := manager.watches.demand(target)
	if manager.watches.recordSnapshot(Snapshot{
		Target: target, Generation: firstDemand.Generation,
		Runs: []RunSnapshot{{BuildNumber: 99}},
	}) {
		t.Fatal("stale first-generation snapshot was accepted")
	}
	if !manager.watches.recordSnapshot(Snapshot{
		Target: target, Generation: secondDemand.Generation,
		Runs: []RunSnapshot{{BuildNumber: 2}},
	}) {
		t.Fatal("current second-generation snapshot was rejected")
	}

	third, replay, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("third Watch: %v", err)
	}
	defer third.Release()
	if !replay.OK || len(replay.Snapshot.Runs) != 1 || replay.Snapshot.Runs[0].BuildNumber != 2 {
		t.Fatalf("replay = %#v, want current build 2", replay)
	}
}

func TestWatchReplaysCopiedSnapshotWithCurrentGeneration(t *testing.T) {
	manager := newWatchTestManager(t)
	target := Target{Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/"}

	first, _, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("first Watch: %v", err)
	}
	firstDemand, _ := manager.watches.demand(target)
	snapshot := Snapshot{
		Target: target, Generation: firstDemand.Generation,
		Runs: []RunSnapshot{{BuildNumber: 42, Stages: []StageSnapshot{{ID: "1", Name: "Build"}}}},
	}
	if !manager.watches.recordSnapshot(snapshot) {
		t.Fatal("current snapshot was rejected")
	}
	snapshot.Runs[0].Stages[0].Name = "changed input"
	first.Release()

	second, replay, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("second Watch: %v", err)
	}
	defer second.Release()
	if !replay.OK || !second.Matches(replay.Snapshot) {
		t.Fatalf("replay = %#v, want matching current-generation snapshot", replay)
	}
	if got := replay.Snapshot.Runs[0].Stages[0].Name; got != "Build" {
		t.Fatalf("replayed stage = %q, want copied value Build", got)
	}

	replay.Snapshot.Runs[0].Stages[0].Name = "changed replay"
	third, nextReplay, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("third Watch: %v", err)
	}
	defer third.Release()
	if got := nextReplay.Snapshot.Runs[0].Stages[0].Name; got != "Build" {
		t.Fatalf("second replay stage = %q, want independent value Build", got)
	}
}

func TestReplayCacheEvictsLeastRecentTargetAtBound(t *testing.T) {
	manager := newWatchTestManager(t)
	targets := make([]Target, maxReplaySnapshots+1)
	for i := range targets {
		target := Target{
			Kind:   TargetJobWindow,
			JobURL: fmt.Sprintf("https://jenkins/job/%d/", i),
		}
		targets[i] = target
		watch, _, err := manager.Watch(WatchSpec{Target: target})
		if err != nil {
			t.Fatalf("Watch target %d: %v", i, err)
		}
		demand, _ := manager.watches.demand(target)
		if !manager.watches.recordSnapshot(Snapshot{Target: target, Generation: demand.Generation}) {
			t.Fatalf("record target %d", i)
		}
		watch.Release()
	}

	first, firstReplay, err := manager.Watch(WatchSpec{Target: targets[0]})
	if err != nil {
		t.Fatalf("reacquire first target: %v", err)
	}
	first.Release()
	if firstReplay.OK {
		t.Fatal("oldest replay was retained beyond cache bound")
	}

	last, lastReplay, err := manager.Watch(WatchSpec{Target: targets[len(targets)-1]})
	if err != nil {
		t.Fatalf("reacquire last target: %v", err)
	}
	last.Release()
	if !lastReplay.OK {
		t.Fatal("most recent replay was evicted")
	}
}

func TestSharedWatchesReleaseTheirOwnDemand(t *testing.T) {
	manager := newWatchTestManager(t)
	target := Target{Kind: TargetExactRun, JobURL: "https://jenkins/job/demo/", BuildNumber: 42}

	first, _, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("first Watch: %v", err)
	}
	second, _, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("second Watch: %v", err)
	}

	demand, ok := manager.watches.demand(target)
	if !ok {
		t.Fatal("shared watches did not create demand")
	}
	if !first.Matches(Snapshot{Target: target, Generation: demand.Generation}) ||
		!second.Matches(Snapshot{Target: target, Generation: demand.Generation}) {
		t.Fatal("shared watches did not use the same target generation")
	}

	first.Release()
	first.Release()
	if _, ok := manager.watches.demand(target); !ok {
		t.Fatal("releasing one owner removed shared demand")
	}

	second.Release()
	if _, ok := manager.watches.demand(target); ok {
		t.Fatal("releasing the final owner retained demand")
	}
}
