package animation

import (
	"sync/atomic"
	"time"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
)

// FrameInterval is the production page animation rate: 12 FPS.
// One frame advances each spinner and pulse bar by one visible step.
// The page clock overrides the spinners' standalone default timing.
const FrameInterval = time.Second / 12

// frameLostAfter is how long a running clock may go without a frame before
// Ensure treats its pending tick as lost and starts a new run. It is long
// enough that a slow frame never trips it. Crush uses the same 2s guard
// (animClockLostAfter in internal/ui/model/chat.go).
const frameLostAfter = 2 * time.Second

var frameIDSeq atomic.Int64

// FrameMsg is one page-level animation frame. Its id is unique across every
// clock, so a page only accepts frames from its own current run.
type FrameMsg struct{ id int }

// FrameClock is a page's single animation clock. At most one tick is in
// flight: Start is a no-op while running, and Stop invalidates the pending
// tick, so re-entry and repeated state changes cannot accumulate chains. The
// zero value ticks at FrameInterval.
type FrameClock struct {
	id       int
	running  bool
	interval time.Duration
	armedAt  time.Time
	now      func() time.Time
}

func NewFrameClock(interval time.Duration) FrameClock {
	return FrameClock{interval: interval}
}

// Ensure is the one call a page makes after any state change. It stops the
// clock when nothing on screen animates, starts it when something does, and
// starts a new run when the pending tick has not arrived within
// frameLostAfter, so a lost tick cannot freeze the page.
//
// This follows Crush's Chat.EnsureAnimating (internal/ui/model/chat.go),
// which its UI calls at the end of every Update. Before it, each page started
// and stopped its clock from several handlers, and one missed call left a
// spinner frozen or running on a page that was no longer shown.
func (c *FrameClock) Ensure(active bool) tea.Cmd {
	if !active {
		c.Stop()
		return nil
	}
	if c.running && c.clockNow().Sub(c.armedAt) < frameLostAfter {
		return nil
	}
	c.Stop()
	return c.Start()
}

// Start delivers the first frame immediately.
func (c *FrameClock) Start() tea.Cmd {
	if c.running {
		return nil
	}
	c.id = int(frameIDSeq.Add(1))
	c.running = true
	c.armedAt = c.clockNow()
	msg := FrameMsg{id: c.id}
	return func() tea.Msg { return msg }
}

// Next schedules the frame after an accepted one.
func (c *FrameClock) Next() tea.Cmd {
	if !c.running {
		return nil
	}
	interval := c.interval
	if interval <= 0 {
		interval = FrameInterval
	}
	c.armedAt = c.clockNow()
	msg := FrameMsg{id: c.id}
	return tea.Tick(interval, func(time.Time) tea.Msg { return msg })
}

func (c *FrameClock) Stop() {
	if !c.running {
		return
	}
	c.running = false
	c.id = int(frameIDSeq.Add(1))
}

func (c *FrameClock) clockNow() time.Time {
	if c.now != nil {
		return c.now()
	}
	return time.Now()
}

func (c *FrameClock) Accept(msg FrameMsg) bool {
	return c.running && msg.id == c.id
}

func (c *FrameClock) Running() bool {
	return c.running
}

// Frame is the message the current run would deliver next. Benchmarks and
// tests use it to advance a page without waiting on the timer.
func (c *FrameClock) Frame() FrameMsg {
	return FrameMsg{id: c.id}
}

// AdvanceSpinner moves one spinner frame and discards the spinner's own
// follow-up tick. The page's FrameClock owns the next deadline.
func AdvanceSpinner(current spinner.Model) spinner.Model {
	next, _ := current.Update(current.Tick())
	return next
}
