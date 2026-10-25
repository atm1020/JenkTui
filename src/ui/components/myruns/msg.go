package myruns

import "github.com/atm1020/jenktui/src/store"

// RunsLoadedMsg is emitted once, in response to Init's initial query. It
// triggers the one-time poll-manager subscription and snapshot replay.
type RunsLoadedMsg struct {
	Runs  []store.RunRecord
	Error error
}

// RunsQueriedMsg is emitted by any subsequent reload of the active query
// (filter apply, poll-driven refresh). ReqID lets a stale reload that
// resolves after a newer one be dropped instead of overwriting fresher rows.
// PreserveSelection is false for reloads that reorder the whole list (e.g.
// reversing sort direction), where following the previous selection's ID
// to its new position would jerk the cursor across the list instead of
// leaving it where it was.
type RunsQueriedMsg struct {
	Runs              []store.RunRecord
	Error             error
	ReqID             int64
	PreserveSelection bool
}

type RunDeletedMsg struct {
	ID string
}

// FilterDebounceMsg fires filterDebounceDelay after a keystroke in the
// inline name/build fields. Gen must equal the model's current generation
// counter for the reload to run — an intervening keystroke bumps the
// counter and supersedes this tick.
type FilterDebounceMsg struct {
	Gen int64
}
