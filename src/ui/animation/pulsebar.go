package animation

import (
	"math"
	"strings"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/log/v2"
)

// Default settings.
const (
	defaultWidth = 40
	defaultFPS   = 10
)

var lastID atomic.Int64

func nextID() int {
	return int(lastID.Add(1))
}

type StepMsg struct{ id int }

type Settings struct {
	Width int // Width of the bar in characters
	FPS   int // Frames per second
}

type PulseBar struct {
	width     int
	fps       int
	iteration atomic.Int64
	id        int
}

func New(opts Settings) *PulseBar {
	if opts.Width < 1 {
		opts.Width = defaultWidth
	}
	if opts.FPS < 1 {
		opts.FPS = defaultFPS
	}

	return &PulseBar{
		width: opts.Width,
		fps:   opts.FPS,
		id:    nextID(),
	}
}

func (p *PulseBar) Width() int {
	return p.width
}

// Frames is the number of distinct views in one pulse cycle.
func (p *PulseBar) Frames() int {
	return max(1, 2*(p.width-1))
}

func (p *PulseBar) Init() tea.Cmd {
	p.id = nextID()
	log.Debug("pulsebar: Init starting animation", "barID", p.id)
	return p.step()
}

// Stop invalidates pending steps without needing to cancel a timer command.
// The next Init starts a fresh chain with a new identity.
func (p *PulseBar) Stop() {
	p.id = nextID()
}

// Step advances one visible frame without scheduling a timer. Callers that
// own a shared frame clock use this so the pulse does not start its own chain.
func (p *PulseBar) Step() {
	if p == nil {
		return
	}
	p.iteration.Add(1)
}

func (p *PulseBar) Update(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case StepMsg:
		if msg.id != p.id {
			log.Debug("pulsebar: rejected stale StepMsg", "barID", p.id, "msgID", msg.id)
			return nil
		}

		p.iteration.Add(1)
		return p.step()
	default:
		return nil
	}
}

func (p *PulseBar) View() string {
	iteration := int(p.iteration.Load())
	position := pulsePosition(iteration, p.width)

	var bar strings.Builder
	for i := range p.width {
		distance := math.Abs(float64(position - i))
		if distance < 1 {
			bar.WriteString("█")
		} else if distance < 2 {
			bar.WriteString("▓")
		} else if distance < 3 {
			bar.WriteString("▒")
		} else {
			bar.WriteString("░")
		}
	}

	return bar.String()
}

// pulsePosition reflects the pulse at the right edge instead of letting it
// travel beyond the visible bar. The old width*2 cycle left the pulse outside
// the rendered area for several frames, making the indicator appear frozen.
func pulsePosition(iteration, width int) int {
	if width <= 1 {
		return 0
	}
	cycle := 2 * (width - 1)
	position := iteration % cycle
	if position >= width {
		return cycle - position
	}
	return position
}

func (p *PulseBar) step() tea.Cmd {
	// Capture the id now: reading p.id when the tick fires would stamp ticks
	// from an old chain with the post-Init id, letting stale chains survive
	// re-Init and multiply.
	id := p.id
	return tea.Tick(time.Second/time.Duration(p.fps), func(t time.Time) tea.Msg {
		return StepMsg{id: id}
	})
}
