package animation

import (
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestProductionFrameRate(t *testing.T) {
	if FrameInterval != time.Second/12 {
		t.Fatalf("frame interval = %s, want 12 FPS", FrameInterval)
	}
}

func TestFrameClockRunsOneChain(t *testing.T) {
	c := NewFrameClock(time.Hour)
	start := c.Start()
	if start == nil {
		t.Fatal("Start on a stopped clock returned no frame")
	}
	if again := c.Start(); again != nil {
		t.Fatal("Start on a running clock started a second chain")
	}
	first, ok := start().(FrameMsg)
	if !ok || !c.Accept(first) {
		t.Fatal("first frame was not accepted")
	}
	if c.Next() == nil {
		t.Fatal("accepted frame did not schedule the next one")
	}
}

func TestFrameClockStopRejectsPendingFrame(t *testing.T) {
	c := NewFrameClock(time.Hour)
	pending := c.Start()().(FrameMsg)
	c.Stop()
	if c.Accept(pending) || c.Running() || c.Next() != nil {
		t.Fatal("stopped clock accepted or scheduled a frame")
	}
	restarted := c.Start()().(FrameMsg)
	if c.Accept(pending) {
		t.Fatal("restarted clock accepted a frame from the previous run")
	}
	if !c.Accept(restarted) {
		t.Fatal("restarted clock rejected its own frame")
	}
}

func TestFrameClocksRejectEachOthersFrames(t *testing.T) {
	a, b := NewFrameClock(time.Hour), NewFrameClock(time.Hour)
	fromA := a.Start()().(FrameMsg)
	_ = b.Start()
	if b.Accept(fromA) {
		t.Fatal("clock accepted another clock's frame")
	}
}

func TestZeroFrameClockTicksAtFrameInterval(t *testing.T) {
	var c FrameClock
	_ = c.Start()
	next := c.Next()
	done := make(chan tea.Msg, 1)
	began := time.Now()
	go func() { done <- next() }()
	select {
	case msg := <-done:
		if !c.Accept(msg.(FrameMsg)) {
			t.Fatal("scheduled frame was rejected")
		}
		if elapsed := time.Since(began); elapsed < FrameInterval/2 {
			t.Fatalf("zero clock ticked after %s, want about %s", elapsed, FrameInterval)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("zero clock never ticked")
	}
}

func TestFrameClockEnsureStartsAndStops(t *testing.T) {
	c := NewFrameClock(time.Hour)
	if c.Ensure(false) != nil || c.Running() {
		t.Fatal("inactive Ensure started the clock")
	}
	first := c.Ensure(true)
	if first == nil || !c.Running() {
		t.Fatal("active Ensure did not start the clock")
	}
	if c.Ensure(true) != nil {
		t.Fatal("Ensure started a second chain while a tick was pending")
	}
	pending := first().(FrameMsg)
	if c.Ensure(false) != nil || c.Running() || c.Accept(pending) {
		t.Fatal("inactive Ensure left the clock running")
	}
}

func TestFrameClockEnsureReplacesLostTick(t *testing.T) {
	now := time.Unix(0, 0)
	c := NewFrameClock(time.Hour)
	c.now = func() time.Time { return now }
	lost := c.Ensure(true)().(FrameMsg)

	now = now.Add(frameLostAfter - time.Millisecond)
	if c.Ensure(true) != nil {
		t.Fatal("Ensure replaced a tick that was still within its deadline")
	}
	now = now.Add(time.Millisecond)
	restart := c.Ensure(true)
	if restart == nil {
		t.Fatal("Ensure did not replace a lost tick")
	}
	if c.Accept(lost) {
		t.Fatal("the lost tick would run alongside its replacement")
	}
	if !c.Accept(restart().(FrameMsg)) {
		t.Fatal("replacement tick was rejected")
	}
}

func TestFrameClockNextRefreshesDeadline(t *testing.T) {
	now := time.Unix(0, 0)
	c := NewFrameClock(time.Hour)
	c.now = func() time.Time { return now }
	_ = c.Ensure(true)
	now = now.Add(frameLostAfter)
	_ = c.Next()
	if c.Ensure(true) != nil {
		t.Fatal("a freshly scheduled tick was treated as lost")
	}
}
