package poll

import "time"

// clock is the scheduler's only time seam. Production uses realClock; tests
// inject a fake so cadence and backoff assertions do not depend on wall time.
type clock interface {
	now() time.Time
	newTimer(d time.Duration) schedulerTimer
}

// schedulerTimer is the single-timer subset of time.Timer the scheduler drives.
// stop reports whether the timer was still pending, so callers can tell a
// stopped timer from one that already delivered a tick.
type schedulerTimer interface {
	ticks() <-chan time.Time
	stop() bool
	reset(d time.Duration)
}

type realClock struct{}

func (realClock) now() time.Time { return time.Now() }

func (realClock) newTimer(d time.Duration) schedulerTimer {
	return realTimer{timer: time.NewTimer(d)}
}

type realTimer struct{ timer *time.Timer }

func (t realTimer) ticks() <-chan time.Time { return t.timer.C }
func (t realTimer) stop() bool              { return t.timer.Stop() }
func (t realTimer) reset(d time.Duration)   { t.timer.Reset(d) }
