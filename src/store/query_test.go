package store

import (
	"testing"

	"github.com/atm1020/jenktui/src/buildstatus"
)

func queryRun(id, jobName, buildID, status string) RunRecord {
	return RunRecord{
		ID:      id,
		JobName: jobName,
		JobURL:  "http://jenkins/job/" + jobName + "/",
		BuildID: buildID,
		Status:  buildstatus.PipelineStatus(status),
	}
}

func TestQueryRuns_JobNamePartialMatch(t *testing.T) {
	s := newTestStore(t)
	must(t, s.AddRun(queryRun("a#1", "deploy-prod", "1", "SUCCESS")))
	must(t, s.AddRun(queryRun("b#1", "build-app", "1", "SUCCESS")))

	runs, err := s.QueryRuns(RunQuery{JobName: "depl"})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].JobName != "deploy-prod" {
		t.Errorf("runs = %+v, want only deploy-prod", runs)
	}
}

func TestQueryRuns_JobNameCaseInsensitive(t *testing.T) {
	s := newTestStore(t)
	must(t, s.AddRun(queryRun("a#1", "Deploy-Prod", "1", "SUCCESS")))

	runs, err := s.QueryRuns(RunQuery{JobName: "DEPLOY"})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(runs) != 1 {
		t.Errorf("len(runs) = %d, want 1 (case-insensitive match)", len(runs))
	}
}

func TestQueryRuns_BuildIDPartialMatch(t *testing.T) {
	s := newTestStore(t)
	must(t, s.AddRun(queryRun("a#123", "job", "123", "SUCCESS")))
	must(t, s.AddRun(queryRun("a#456", "job", "456", "SUCCESS")))

	runs, err := s.QueryRuns(RunQuery{BuildID: "23"})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].BuildID != "123" {
		t.Errorf("runs = %+v, want only build 123", runs)
	}
}

func TestQueryRuns_BuildIDLeadingHashNormalized(t *testing.T) {
	s := newTestStore(t)
	must(t, s.AddRun(queryRun("a#42", "job", "42", "SUCCESS")))

	for _, input := range []string{"42", "#42", "  42  ", "  #42"} {
		runs, err := s.QueryRuns(RunQuery{BuildID: input})
		if err != nil {
			t.Fatalf("QueryRuns(%q): %v", input, err)
		}
		if len(runs) != 1 {
			t.Errorf("QueryRuns(BuildID=%q) = %d runs, want 1", input, len(runs))
		}
	}
}

func TestQueryRuns_ExactStatusFilter(t *testing.T) {
	s := newTestStore(t)
	must(t, s.AddRun(queryRun("a#1", "job", "1", "SUCCESS")))
	must(t, s.AddRun(queryRun("a#2", "job", "2", "FAILED")))

	runs, err := s.QueryRuns(RunQuery{Status: "failed"})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].Status != "FAILED" {
		t.Errorf("runs = %+v, want only the FAILED run (case-insensitive)", runs)
	}
}

func TestQueryRuns_EmptyStatusMeansAll(t *testing.T) {
	s := newTestStore(t)
	must(t, s.AddRun(queryRun("a#1", "job", "1", "SUCCESS")))
	must(t, s.AddRun(queryRun("a#2", "job", "2", "FAILED")))

	runs, err := s.QueryRuns(RunQuery{Status: ""})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("len(runs) = %d, want 2 (empty status is not a filter)", len(runs))
	}
}

func TestQueryRuns_CombinedNameBuildStatus(t *testing.T) {
	s := newTestStore(t)
	must(t, s.AddRun(queryRun("a#1", "deploy-prod", "1", "FAILED")))
	must(t, s.AddRun(queryRun("a#2", "deploy-prod", "2", "SUCCESS")))
	must(t, s.AddRun(queryRun("a#3", "build-app", "1", "FAILED")))

	runs, err := s.QueryRuns(RunQuery{JobName: "deploy", BuildID: "1", Status: "FAILED"})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "a#1" {
		t.Errorf("runs = %+v, want only a#1", runs)
	}
}

func TestQueryRuns_LiteralLikeMetacharacters(t *testing.T) {
	s := newTestStore(t)
	must(t, s.AddRun(queryRun("a#1", "50%_done\\job", "1", "SUCCESS")))
	must(t, s.AddRun(queryRun("a#2", "unrelated-job", "1", "SUCCESS")))

	runs, err := s.QueryRuns(RunQuery{JobName: `%_done\`})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "a#1" {
		t.Errorf("runs = %+v, want only the run containing literal %%, _, and \\", runs)
	}
}

func TestQueryRuns_EmptyFiltersMatchEverything(t *testing.T) {
	s := newTestStore(t)
	must(t, s.AddRun(queryRun("a#1", "job-a", "1", "SUCCESS")))
	must(t, s.AddRun(queryRun("a#2", "job-b", "2", "FAILED")))

	runs, err := s.QueryRuns(RunQuery{})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("len(runs) = %d, want 2", len(runs))
	}
}

func TestQueryRuns_ScopedToServer(t *testing.T) {
	dir := t.TempDir()
	s1 := NewForServer(dir, "srv-1")
	t.Cleanup(func() { _ = s1.Close() })
	s2 := NewForServer(dir, "srv-2")
	t.Cleanup(func() { _ = s2.Close() })

	must(t, s1.AddRun(queryRun("a#1", "job", "1", "SUCCESS")))
	must(t, s2.AddRun(queryRun("b#1", "job", "1", "SUCCESS")))

	runs, err := s1.QueryRuns(RunQuery{})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if len(runs) != 1 || runs[0].ID != "a#1" {
		t.Errorf("runs = %+v, want only srv-1's run", runs)
	}
}

func TestQueryRuns_DateOrdering(t *testing.T) {
	s := newTestStore(t)
	early := queryRun("a#1", "job", "1", "SUCCESS")
	early.StartTime, early.CreatedAt = 100, 100
	late := queryRun("a#2", "job", "2", "SUCCESS")
	late.StartTime, late.CreatedAt = 200, 200
	must(t, s.AddRun(early))
	must(t, s.AddRun(late))

	asc, err := s.QueryRuns(RunQuery{SortBy: RunSortDate, Direction: SortAscending})
	if err != nil {
		t.Fatalf("QueryRuns asc: %v", err)
	}
	if asc[0].ID != "a#1" || asc[1].ID != "a#2" {
		t.Errorf("ascending order = %v, want [a#1 a#2]", ids(asc))
	}

	desc, err := s.QueryRuns(RunQuery{SortBy: RunSortDate, Direction: SortDescending})
	if err != nil {
		t.Fatalf("QueryRuns desc: %v", err)
	}
	if desc[0].ID != "a#2" || desc[1].ID != "a#1" {
		t.Errorf("descending order = %v, want [a#2 a#1]", ids(desc))
	}
}

func TestQueryRuns_DateOrdering_UnknownStartTimeSortsLast(t *testing.T) {
	s := newTestStore(t)
	known := queryRun("a#1", "job", "1", "SUCCESS")
	known.StartTime, known.CreatedAt = 100, 100
	unknown := queryRun("a#2", "job", "2", "IN_PROGRESS")
	unknown.StartTime, unknown.CreatedAt = 0, 200
	must(t, s.AddRun(known))
	must(t, s.AddRun(unknown))

	for _, dir := range []SortDirection{SortAscending, SortDescending} {
		runs, err := s.QueryRuns(RunQuery{SortBy: RunSortDate, Direction: dir})
		if err != nil {
			t.Fatalf("QueryRuns %s: %v", dir, err)
		}
		if runs[len(runs)-1].ID != "a#2" {
			t.Errorf("direction=%s: order = %v, want unknown start_time (a#2) last", dir, ids(runs))
		}
	}
}

func TestQueryRuns_DurationOrdering_UnknownDurationSortsLast(t *testing.T) {
	s := newTestStore(t)
	finished := queryRun("a#1", "job", "1", "SUCCESS")
	finished.Duration, finished.CreatedAt = 5000, 100
	running := queryRun("a#2", "job", "2", "IN_PROGRESS")
	running.Duration, running.CreatedAt = 0, 200
	must(t, s.AddRun(finished))
	must(t, s.AddRun(running))

	for _, dir := range []SortDirection{SortAscending, SortDescending} {
		runs, err := s.QueryRuns(RunQuery{SortBy: RunSortDuration, Direction: dir})
		if err != nil {
			t.Fatalf("QueryRuns %s: %v", dir, err)
		}
		if runs[len(runs)-1].ID != "a#2" {
			t.Errorf("direction=%s: order = %v, want unknown duration (a#2) last", dir, ids(runs))
		}
	}
}

func TestQueryRuns_StatusOrdering(t *testing.T) {
	s := newTestStore(t)
	must(t, s.AddRun(queryRun("a#1", "job", "1", "SUCCESS")))
	must(t, s.AddRun(queryRun("a#2", "job", "2", "ABORTED")))
	must(t, s.AddRun(queryRun("a#3", "job", "3", "FAILED")))

	asc, err := s.QueryRuns(RunQuery{SortBy: RunSortStatus, Direction: SortAscending})
	if err != nil {
		t.Fatalf("QueryRuns asc: %v", err)
	}
	if got := ids(asc); got[0] != "a#2" || got[1] != "a#3" || got[2] != "a#1" {
		t.Errorf("ascending status order = %v, want [a#2(ABORTED) a#3(FAILED) a#1(SUCCESS)]", got)
	}

	desc, err := s.QueryRuns(RunQuery{SortBy: RunSortStatus, Direction: SortDescending})
	if err != nil {
		t.Fatalf("QueryRuns desc: %v", err)
	}
	if got := ids(desc); got[0] != "a#1" || got[1] != "a#3" || got[2] != "a#2" {
		t.Errorf("descending status order = %v, want [a#1(SUCCESS) a#3(FAILED) a#2(ABORTED)]", got)
	}
}

func TestQueryRuns_DeterministicTieBreak(t *testing.T) {
	s := newTestStore(t)
	// Same start_time: created_at DESC, then id ASC must break the tie identically every run.
	a := queryRun("a#1", "job", "1", "SUCCESS")
	a.StartTime, a.CreatedAt = 100, 500
	b := queryRun("b#1", "job", "1", "SUCCESS")
	b.StartTime, b.CreatedAt = 100, 500
	must(t, s.AddRun(a))
	must(t, s.AddRun(b))

	first, err := s.QueryRuns(RunQuery{SortBy: RunSortDate, Direction: SortDescending})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	for i := 0; i < 5; i++ {
		again, err := s.QueryRuns(RunQuery{SortBy: RunSortDate, Direction: SortDescending})
		if err != nil {
			t.Fatalf("QueryRuns: %v", err)
		}
		if ids(again)[0] != ids(first)[0] || ids(again)[1] != ids(first)[1] {
			t.Errorf("iteration %d: order = %v, want stable %v", i, ids(again), ids(first))
		}
	}
	// a#1 sorts before b#1 by id ASC once start_time and created_at tie.
	if ids(first)[0] != "a#1" {
		t.Errorf("order = %v, want a#1 first on id ASC tie-break", ids(first))
	}
}

func TestQueryRuns_InvalidSortFallsBackToDateDescending(t *testing.T) {
	s := newTestStore(t)
	early := queryRun("a#1", "job", "1", "SUCCESS")
	early.StartTime, early.CreatedAt = 100, 100
	late := queryRun("a#2", "job", "2", "SUCCESS")
	late.StartTime, late.CreatedAt = 200, 200
	must(t, s.AddRun(early))
	must(t, s.AddRun(late))

	runs, err := s.QueryRuns(RunQuery{SortBy: "bogus", Direction: "sideways"})
	if err != nil {
		t.Fatalf("QueryRuns: %v", err)
	}
	if runs[0].ID != "a#2" || runs[1].ID != "a#1" {
		t.Errorf("order = %v, want date-descending fallback [a#2 a#1]", ids(runs))
	}
}

// A row that no longer matches an active status filter after an out-of-band
// UpdateRun must not appear in a re-run of the same query — the store-level
// guarantee that a poll-driven status change removes a filtered-out row.
func TestQueryRuns_StatusChangeRemovesRowFromActiveFilter(t *testing.T) {
	s := newTestStore(t)
	must(t, s.AddRun(queryRun("a#1", "job", "1", "IN_PROGRESS")))

	query := RunQuery{Status: "IN_PROGRESS"}
	before, err := s.QueryRuns(query)
	if err != nil {
		t.Fatalf("QueryRuns before: %v", err)
	}
	if len(before) != 1 {
		t.Fatalf("len(before) = %d, want 1", len(before))
	}

	if err := s.UpdateRun("a#1", "SUCCESS", 5000, 4000, 1000); err != nil {
		t.Fatalf("UpdateRun: %v", err)
	}

	after, err := s.QueryRuns(query)
	if err != nil {
		t.Fatalf("QueryRuns after: %v", err)
	}
	if len(after) != 0 {
		t.Errorf("len(after) = %d, want 0 (run moved out of the IN_PROGRESS filter)", len(after))
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func ids(runs []RunRecord) []string {
	out := make([]string, len(runs))
	for i, r := range runs {
		out[i] = r.ID
	}
	return out
}
