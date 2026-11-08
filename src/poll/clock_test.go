package poll

import (
	"sync"
	"testing"
	"time"
)

// fakeClock freezes scheduler time so a test, not the machine, decides when a
// poll becomes due. The scheduler stops its timer exactly once per loop
// iteration, which the fake counts as an iteration boundary: advance and
// finishWork use it to return only after the scheduler has observed the
// change, so later assertions never race the loop.
type fakeClock struct {
	mu         sync.Mutex
	current    time.Time
	timers     []*fakeTimer
	iterations int
	changed    chan struct{}
}

func newFakeClock() *fakeClock {
	return &fakeClock{
		current: time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC),
		changed: make(chan struct{}),
	}
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

func (c *fakeClock) newTimer(d time.Duration) schedulerTimer {
	c.mu.Lock()
	defer c.mu.Unlock()
	timer := &fakeTimer{
		clock:    c,
		c:        make(chan time.Time, 1),
		deadline: c.current.Add(d),
		pending:  true,
	}
	c.timers = append(c.timers, timer)
	return timer
}

// iteration returns the scheduler loop iterations observed so far.
func (c *fakeClock) iteration() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.iterations
}

// waitIteration blocks until the scheduler loop has reached want iterations.
func (c *fakeClock) waitIteration(t *testing.T, want int) {
	t.Helper()
	timeout := time.After(5 * time.Second)
	for {
		c.mu.Lock()
		reached, changed := c.iterations >= want, c.changed
		c.mu.Unlock()
		if reached {
			return
		}
		select {
		case <-changed:
		case <-timeout:
			t.Fatalf("scheduler stalled before loop iteration %d", want)
		}
	}
}

// advance moves virtual time forward and delivers every tick that became due.
func (c *fakeClock) advance(t *testing.T, d time.Duration) {
	t.Helper()
	c.mu.Lock()
	c.current = c.current.Add(d)
	var fired []*fakeTimer
	for _, timer := range c.timers {
		if timer.pending && !timer.deadline.After(c.current) {
			timer.pending = false
			fired = append(fired, timer)
		}
	}
	want, tick := c.iterations+len(fired), c.current
	c.mu.Unlock()

	for _, timer := range fired {
		select {
		case timer.c <- tick:
		default:
		}
	}
	if len(fired) > 0 {
		c.waitIteration(t, want)
	}
}

func (c *fakeClock) noteIteration() {
	c.mu.Lock()
	c.iterations++
	close(c.changed)
	c.changed = make(chan struct{})
	c.mu.Unlock()
}

type fakeTimer struct {
	clock    *fakeClock
	c        chan time.Time
	deadline time.Time
	pending  bool
}

func (t *fakeTimer) ticks() <-chan time.Time { return t.c }

func (t *fakeTimer) stop() bool {
	t.clock.mu.Lock()
	was := t.pending
	t.pending = false
	t.clock.mu.Unlock()
	t.clock.noteIteration()
	return was
}

func (t *fakeTimer) reset(d time.Duration) {
	t.clock.mu.Lock()
	t.deadline = t.clock.current.Add(d)
	t.pending = true
	t.clock.mu.Unlock()
}
