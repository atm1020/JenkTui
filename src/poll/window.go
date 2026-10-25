package poll

// This file turns partial Jenkins job responses into stable state for the UI.
// The first request returns a full run window, but a later `since` request is
// incremental and inclusive. For example, a cached [10, 9, 8] followed by a
// response [11, 10] must become [11, 10, 9, 8], not replace the cache with
// [11, 10]. jobWindow therefore merges by build number, removes the repeated
// cursor run, sorts the result, keeps active runs inside the next cursor, and
// periodically replaces merged state with a full response to repair gaps or
// stale entries.
//
// This model is needed only because poll uses incremental requests. An
// always-full strategy could remove the merge, cursor, and reconciliation
// state, but would fetch the latest runs and all of their stage data on every
// poll for every watched job.

import "sort"

const (
	jobWindowLimit                      = 10
	jobWindowReconcileAfterIncrementals = 8
)

// jobWindow is the canonical run state for one Jenkins job. It is independent
// from the bounded replay cache used by watches.
type jobWindow struct {
	runs         []RunSnapshot
	incrementals int
}

func (w *jobWindow) apply(incoming []RunSnapshot, full bool) {
	if full || len(w.runs) == 0 {
		w.runs = canonicalRuns(incoming)
		w.incrementals = 0
		return
	}

	merged := make([]RunSnapshot, 0, len(w.runs)+len(incoming))
	merged = append(merged, w.runs...)
	merged = append(merged, incoming...)
	w.runs = canonicalRuns(merged)
	w.incrementals++
}

func (w jobWindow) snapshot() []RunSnapshot {
	return cloneRuns(w.runs)
}

func (w *jobWindow) mergeExact(observed RunSnapshot) bool {
	for i := range w.runs {
		if w.runs[i].BuildNumber == observed.BuildNumber {
			w.runs[i] = cloneRun(observed)
			return true
		}
	}
	return false
}

func (w jobWindow) needsFullReconciliation() bool {
	return w.incrementals >= jobWindowReconcileAfterIncrementals
}

func (w jobWindow) cursor() BuildNumber {
	var newest BuildNumber
	var oldestActive BuildNumber

	for i := range w.runs {
		number := w.runs[i].BuildNumber
		if number > newest {
			newest = number
		}
		if i < jobWindowLimit && w.runs[i].Activity.IsActive() && (oldestActive == 0 || number < oldestActive) {
			oldestActive = number
		}
	}

	if oldestActive != 0 {
		return oldestActive
	}
	return newest
}

func canonicalRuns(runs []RunSnapshot) []RunSnapshot {
	byNumber := make(map[BuildNumber]int, len(runs))
	canonical := make([]RunSnapshot, 0, len(runs))
	// copy and deduplication
	for i := range runs {
		run := cloneRun(runs[i])
		if existing, ok := byNumber[run.BuildNumber]; ok {
			canonical[existing] = run
			continue
		}
		byNumber[run.BuildNumber] = len(canonical)
		canonical = append(canonical, run)
	}

	// sort from
	sort.SliceStable(canonical, func(i, j int) bool {
		return newerRun(canonical[i], canonical[j])
	})
	return canonical
}

func newerRun(left, right RunSnapshot) bool {
	return left.BuildNumber > right.BuildNumber
}
