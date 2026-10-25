package poll

import (
	"context"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
)

type countingObservationSource struct {
	window Observation
	exacts map[BuildNumber]Observation

	windowCalls   int
	exactCalls    int
	windowCursors []BuildNumber
}

func (s *countingObservationSource) fetchWindow(_ context.Context, _ string, cursor BuildNumber) (Observation, error) {
	s.windowCalls++
	s.windowCursors = append(s.windowCursors, cursor)
	return s.window, nil
}

func (s *countingObservationSource) fetchExact(_ context.Context, target Target) (Observation, error) {
	s.exactCalls++
	return s.exacts[target.BuildNumber], nil
}

func exactRunWork(t *testing.T, manager *Manager, jobURL string, numbers []BuildNumber) jobWork {
	t.Helper()
	for _, number := range numbers {
		watch, _, err := manager.Watch(WatchSpec{Target: Target{
			Kind: TargetExactRun, JobURL: jobURL, BuildNumber: number,
		}})
		if err != nil {
			t.Fatalf("Watch build %d: %v", number, err)
		}
		t.Cleanup(watch.Release)
	}
	return jobWork{jobURL: jobURL, demands: manager.watches.demandsSnapshot()}
}

func TestPollingExecutorReusesDemandedWindowForExactRun(t *testing.T) {
	const jobURL = "https://jenkins/job/demo/"
	manager := newManager(Config{}, noopExecutor{})
	defer manager.cancel()
	windowWatch, _, err := manager.Watch(WatchSpec{Target: Target{Kind: TargetJobWindow, JobURL: jobURL}})
	if err != nil {
		t.Fatalf("Watch job window: %v", err)
	}
	t.Cleanup(windowWatch.Release)
	work := exactRunWork(t, manager, jobURL, []BuildNumber{327})

	source := &countingObservationSource{
		window: Observation{
			Target: Target{Kind: TargetJobWindow, JobURL: jobURL},
			Runs:   []RunSnapshot{{BuildNumber: 327, Status: buildstatus.StatusInProgress, Activity: buildstatus.ActivityRunning}},
		},
		exacts: make(map[BuildNumber]Observation),
	}
	if err := (pollingExecutor{manager: manager, source: source}).execute(context.Background(), work); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if source.windowCalls != 1 || source.exactCalls != 0 {
		t.Fatalf("requests = %d window + %d exact, want 1 demanded window + 0 exact", source.windowCalls, source.exactCalls)
	}
}

func TestPollingExecutorBatchesBackgroundExactRunsForOneJob(t *testing.T) {
	const jobURL = "https://jenkins/job/demo/"
	numbers := []BuildNumber{327, 328, 329}
	runs := make([]RunSnapshot, len(numbers))
	for i, number := range numbers {
		runs[i] = RunSnapshot{
			BuildNumber: number,
			Status:      buildstatus.StatusInProgress,
			Activity:    buildstatus.ActivityRunning,
		}
	}

	manager := newManager(Config{}, noopExecutor{})
	defer manager.cancel()
	work := exactRunWork(t, manager, jobURL, numbers)
	source := &countingObservationSource{
		window: Observation{
			Target:     Target{Kind: TargetJobWindow, JobURL: jobURL},
			ObservedAt: time.Now(),
			Runs:       runs,
		},
		exacts: make(map[BuildNumber]Observation),
	}
	if err := (pollingExecutor{manager: manager, source: source}).execute(context.Background(), work); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if source.windowCalls != 1 || source.exactCalls != 0 {
		t.Fatalf("requests = %d window + %d exact, want 1 window + 0 exact", source.windowCalls, source.exactCalls)
	}
	if got := source.windowCursors; len(got) != 1 || got[0] != 327 {
		t.Fatalf("window cursors = %v, want oldest exact run [327]", got)
	}
}

func TestPollingExecutorFallsBackForRunMissingFromBatchWindow(t *testing.T) {
	const jobURL = "https://jenkins/job/demo/"
	manager := newManager(Config{}, noopExecutor{})
	defer manager.cancel()
	work := exactRunWork(t, manager, jobURL, []BuildNumber{327, 328})

	source := &countingObservationSource{
		window: Observation{
			Target: Target{Kind: TargetJobWindow, JobURL: jobURL},
			Runs: []RunSnapshot{{
				BuildNumber: 328,
				Status:      buildstatus.StatusInProgress,
				Activity:    buildstatus.ActivityRunning,
			}},
		},
		exacts: map[BuildNumber]Observation{
			327: {
				Target: Target{Kind: TargetExactRun, JobURL: jobURL, BuildNumber: 327},
				Runs:   []RunSnapshot{{BuildNumber: 327, Status: buildstatus.StatusInProgress, Activity: buildstatus.ActivityRunning}},
			},
		},
	}
	if err := (pollingExecutor{manager: manager, source: source}).execute(context.Background(), work); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if source.windowCalls != 1 || source.exactCalls != 1 {
		t.Fatalf("requests = %d window + %d exact, want 1 window + 1 exact fallback", source.windowCalls, source.exactCalls)
	}
}

func TestPollingExecutorUsesWindowEndpointForSingleBackgroundRun(t *testing.T) {
	const jobURL = "https://jenkins/job/demo/"
	manager := newManager(Config{}, noopExecutor{})
	defer manager.cancel()
	work := exactRunWork(t, manager, jobURL, []BuildNumber{327})

	source := &countingObservationSource{
		window: Observation{
			Target: Target{Kind: TargetJobWindow, JobURL: jobURL},
			Runs:   []RunSnapshot{{BuildNumber: 327, Status: buildstatus.StatusInProgress, Activity: buildstatus.ActivityRunning}},
		},
		exacts: make(map[BuildNumber]Observation),
	}
	if err := (pollingExecutor{manager: manager, source: source}).execute(context.Background(), work); err != nil {
		t.Fatalf("execute: %v", err)
	}

	if source.windowCalls != 1 || source.exactCalls != 0 {
		t.Fatalf("requests = %d window + %d exact, want 1 window + 0 exact", source.windowCalls, source.exactCalls)
	}
}
