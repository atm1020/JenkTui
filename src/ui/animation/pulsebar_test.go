package animation

import (
	"strconv"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// A StepMsg scheduled before a re-Init belongs to the old tick chain and must
// be rejected, otherwise every router Back/Forward Init() leaves one more
// concurrent chain running (animation speeds up, CPU climbs).
func TestPulseBarRejectsStaleChainAfterReInit(t *testing.T) {
	p := New(Settings{Width: 10, FPS: 1000})

	chainA := p.Init()
	// Router navigates back/forward to the view: Init is called again while
	// chain A's tick is still in flight.
	chainB := p.Init()

	// Chain A's pending tick fires after the re-init.
	staleMsg := chainA()
	if cmd := p.Update(staleMsg); cmd != nil {
		t.Fatal("stale StepMsg from pre-reinit chain was accepted; duplicate tick chains accumulate")
	}

	// The current chain must keep running.
	freshMsg := chainB()
	if cmd := p.Update(freshMsg); cmd == nil {
		t.Fatal("StepMsg from current chain was rejected; animation would stop")
	}
}

func TestPulseBarStepAdvancesWithoutATimer(t *testing.T) {
	p := New(Settings{Width: 14, FPS: 10})
	before := p.View()
	p.Step()
	if p.View() == before {
		t.Fatal("Step did not advance the visible pulse")
	}
}

func TestPulseBarMovesAcrossTheVisibleCycle(t *testing.T) {
	for _, width := range []int{2, 14, 32} {
		t.Run("width="+strconv.Itoa(width), func(t *testing.T) {
			p := New(Settings{Width: width, FPS: 1000})
			cycle := 2 * (width - 1)
			first := p.View()
			previous := first

			for range cycle {
				if cmd := p.Update(StepMsg{id: p.id}); cmd == nil {
					t.Fatal("current animation step did not schedule the next step")
				}
				current := p.View()
				if current == previous {
					t.Fatalf("visible frame stopped at %q", current)
				}
				if got := ansi.StringWidth(current); got != width {
					t.Fatalf("rendered width = %d, want %d", got, width)
				}
				previous = current
			}
			if p.View() != first {
				t.Fatalf("cycle did not return to initial frame: got %q, want %q", p.View(), first)
			}
		})
	}
}

// A half-step position lights two full blocks and widens the pulse; every
// frame must show exactly one.
func TestPulseBarFramesShowOneFullBlock(t *testing.T) {
	p := New(Settings{Width: 6, FPS: 10})
	want := []string{"█▓▒░░░", "▓█▓▒░░", "▒▓█▓▒░", "░▒▓█▓▒", "░░▒▓█▓", "░░░▒▓█", "░░▒▓█▓"}
	for i, frame := range want {
		if got := p.View(); got != frame {
			t.Fatalf("frame %d = %q, want %q", i, got, frame)
		}
		p.Step()
	}
	wide := New(Settings{Width: 32, FPS: 10})
	for range 2 * 32 {
		if n := strings.Count(wide.View(), "█"); n != 1 {
			t.Fatalf("frame has %d full blocks, want 1: %q", n, wide.View())
		}
		wide.Step()
	}
}
