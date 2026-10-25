package poll

import (
	"slices"
	"testing"

	"github.com/atm1020/jenktui/src/buildstatus"
)

func TestJobWindowFullObservationBecomesCanonicalSnapshot(t *testing.T) {
	var window jobWindow
	window.apply([]RunSnapshot{
		{BuildNumber: 2, Status: buildstatus.StatusInProgress},
		{BuildNumber: 10, Status: buildstatus.StatusSuccess},
		{BuildNumber: 2, Status: buildstatus.StatusSuccess},
	}, true)

	got := window.snapshot()
	if numbers := runNumbers(got); !slices.Equal(numbers, []BuildNumber{10, 2}) {
		t.Fatalf("build numbers = %v, want [10 2]", numbers)
	}
	if got[1].Status != buildstatus.StatusSuccess {
		t.Fatalf("duplicate run status = %q, want latest observation %q", got[1].Status, buildstatus.StatusSuccess)
	}
}

func TestJobWindowIncrementalObservationMergesByBuildNumber(t *testing.T) {
	var window jobWindow
	window.apply([]RunSnapshot{
		{BuildNumber: 10, Status: buildstatus.StatusInProgress},
		{BuildNumber: 9, Status: buildstatus.StatusSuccess},
	}, true)

	window.apply([]RunSnapshot{
		{BuildNumber: 11, Status: buildstatus.StatusInProgress},
		{BuildNumber: 10, Status: buildstatus.StatusSuccess},
	}, false)

	got := window.snapshot()
	if numbers := runNumbers(got); !slices.Equal(numbers, []BuildNumber{11, 10, 9}) {
		t.Fatalf("build numbers = %v, want [11 10 9]", numbers)
	}
	if got[1].Status != buildstatus.StatusSuccess {
		t.Fatalf("updated run status = %q, want %q", got[1].Status, buildstatus.StatusSuccess)
	}
}

func TestJobWindowCursorUsesOldestActiveRunInsideServerWindow(t *testing.T) {
	var window jobWindow
	runs := make([]RunSnapshot, 0, 11)
	for number := BuildNumber(15); number >= 5; number-- {
		activity := buildstatus.ActivityFinished
		if number == 14 || number == 7 || number == 5 {
			activity = buildstatus.ActivityRunning
		}
		runs = append(runs, RunSnapshot{BuildNumber: number, Activity: activity})
	}
	window.apply(runs, true)

	if got := window.cursor(); got != 7 {
		t.Fatalf("cursor = %d, want oldest active build 7 inside newest ten runs", got)
	}
}

func TestJobWindowRequestsFullReconciliationAfterEightIncrementals(t *testing.T) {
	var window jobWindow
	window.apply([]RunSnapshot{{BuildNumber: 10}}, true)

	for poll := 1; poll <= 8; poll++ {
		window.apply(nil, false)
		if poll < 8 && window.needsFullReconciliation() {
			t.Fatalf("requested full reconciliation after %d incrementals, want after 8", poll)
		}
	}
	if !window.needsFullReconciliation() {
		t.Fatal("did not request full reconciliation after 8 incrementals")
	}

	window.apply([]RunSnapshot{{BuildNumber: 11}}, true)
	if window.needsFullReconciliation() {
		t.Fatal("full observation did not reset reconciliation cadence")
	}
}

func TestJobWindowExactObservationUpdatesOnlyExistingRun(t *testing.T) {
	var window jobWindow
	window.apply([]RunSnapshot{{
		BuildNumber: 42, Status: buildstatus.StatusInProgress, Activity: buildstatus.ActivityRunning,
	}}, true)

	exact := RunSnapshot{
		BuildNumber: 42, Status: buildstatus.StatusSuccess, Activity: buildstatus.ActivityFinished,
		Stages: []StageSnapshot{{ID: "1", Name: "Deploy"}},
	}
	if !window.mergeExact(exact) {
		t.Fatal("existing exact run was not merged")
	}
	exact.Stages[0].Name = "mutated after merge"

	if window.mergeExact(RunSnapshot{BuildNumber: 1}) {
		t.Fatal("exact run outside the canonical window was appended")
	}
	got := window.snapshot()
	if len(got) != 1 || got[0].Status != buildstatus.StatusSuccess || got[0].Stages[0].Name != "Deploy" {
		t.Fatalf("snapshot after exact merge = %#v", got)
	}
}

func TestJobWindowCursorFallsBackToHighestBuildNumber(t *testing.T) {
	var window jobWindow
	window.apply([]RunSnapshot{
		{BuildNumber: 9, Activity: buildstatus.ActivityFinished},
		{BuildNumber: 12, Activity: buildstatus.ActivityFinished},
	}, true)

	if got := window.cursor(); got != 12 {
		t.Fatalf("cursor = %d, want highest build number 12", got)
	}
}

func TestJobWindowFullObservationReplacesIncrementalHistory(t *testing.T) {
	var window jobWindow
	window.apply([]RunSnapshot{{BuildNumber: 10}, {BuildNumber: 9}}, true)
	window.apply([]RunSnapshot{{BuildNumber: 11}}, false)
	window.apply([]RunSnapshot{{BuildNumber: 12}, {BuildNumber: 11}}, true)

	if numbers := runNumbers(window.snapshot()); !slices.Equal(numbers, []BuildNumber{12, 11}) {
		t.Fatalf("build numbers = %v, want full observation to replace old history", numbers)
	}
}

func TestJobWindowOwnsItsSnapshotData(t *testing.T) {
	incoming := []RunSnapshot{{
		BuildNumber: 42, Stages: []StageSnapshot{{ID: "1", Name: "Build"}},
	}}
	var window jobWindow
	window.apply(incoming, true)

	incoming[0].Stages[0].Name = "changed input"
	first := window.snapshot()
	first[0].Stages[0].Name = "changed output"
	second := window.snapshot()

	if got := second[0].Stages[0].Name; got != "Build" {
		t.Fatalf("stored stage name = %q, want independent snapshot data", got)
	}
}

func runNumbers(runs []RunSnapshot) []BuildNumber {
	numbers := make([]BuildNumber, len(runs))
	for i := range runs {
		numbers[i] = runs[i].BuildNumber
	}
	return numbers
}
