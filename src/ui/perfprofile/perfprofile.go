// Package perfprofile runs a page's live animation profile: the page's real
// commands delivered by Bubble Tea, its real renderer writing into a counting
// sink, and CPU, heap, goroutine, allocation, and terminal-output captures.
// Page packages call Run from an opt-in test that `task profile:*` and
// dev/perf execute.
package perfprofile

import (
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sync"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
)

// The environment contract shared by the Taskfile and dev/perf.
const (
	DirEnv      = "JENKTUI_ANIMATION_PROFILE_DIR"
	DurationEnv = "JENKTUI_ANIMATION_PROFILE_DURATION"
)

// ColorProfile is forced because the sink is not a TTY: colour detection
// would pick NoTTY and strip every style. TrueColor measures the bytes a
// modern terminal receives.
const ColorProfile = colorprofile.TrueColor

// Scenario is one fixed page workload. Metrics holds its identifying
// metadata (scenario name, fixture shape, replay benchmark); Run adds the
// measured values.
type Scenario struct {
	Page    util.JenkTuiModel
	Start   tea.Cmd
	Width   int
	Height  int
	Metrics map[string]any
}

type profileDone struct{}

// Model adapts a page to tea.Model and counts page calls. Bubble Tea owns the
// command goroutines and message delivery, just as it does in the app.
type Model struct {
	page     util.JenkTuiModel
	start    tea.Cmd
	duration time.Duration
	updates  int
	views    int
}

func NewModel(page util.JenkTuiModel, start tea.Cmd, duration time.Duration) *Model {
	return &Model{page: page, start: start, duration: duration}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.start, tea.Tick(m.duration, func(time.Time) tea.Msg { return profileDone{} }))
}

func (m *Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(profileDone); ok {
		_ = m.page.CleanUp()
		return m, tea.Quit
	}
	m.updates++
	_, cmd := m.page.Update(msg)
	return m, cmd
}

// View mirrors the app shell's view settings so the renderer emits the same
// alt-screen and OSC 11 background output as production.
func (m *Model) View() tea.View {
	m.views++
	view := tea.NewView(m.page.View())
	view.AltScreen = true
	view.BackgroundColor = styles.GetTheme().BgBase
	return view
}

// TerminalSink stands in for a terminal. The renderer writes each flushed
// frame with one Write; Bubble Tea's own startup/shutdown sequences are
// separate writes.
type TerminalSink struct {
	mu     sync.Mutex
	writes int64
	bytes  int64
}

func (s *TerminalSink) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	s.writes++
	s.bytes += int64(len(p))
	return len(p), nil
}

func (s *TerminalSink) Totals() (writes, written int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.writes, s.bytes
}

// NewProgram runs model through the real renderer into sink. Input stays nil:
// no raw mode, and the renderer maps newlines as it does without a TTY input.
func NewProgram(model tea.Model, sink io.Writer, width, height int) *tea.Program {
	return tea.NewProgram(model, tea.WithInput(nil), tea.WithOutput(sink),
		tea.WithColorProfile(ColorProfile), tea.WithoutSignalHandler(),
		tea.WithWindowSize(width, height),
		tea.WithEnvironment([]string{"TERM=xterm-256color", "COLORTERM=truecolor", "TZ=UTC"}))
}

// Run captures the live profile into DirEnv. It is opt-in because wall-clock
// profiling is a measurement, not a timing-sensitive CI assertion.
func Run(t *testing.T, newScenario func() Scenario) {
	t.Helper()
	dir := os.Getenv(DirEnv)
	if dir == "" {
		t.Skip("set " + DirEnv + " (task profile:*) to capture the live animation profile")
	}
	duration := 10 * time.Second
	if value := os.Getenv(DurationEnv); value != "" {
		var err error
		duration, err = time.ParseDuration(value)
		if err != nil || duration < time.Second {
			t.Fatal(DurationEnv + " must be a duration >= 1s")
		}
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	s := newScenario()
	defer func() { _ = s.Page.CleanUp() }()
	model := NewModel(s.Page, s.Start, duration)
	sink := &TerminalSink{}
	program := NewProgram(model, sink, s.Width, s.Height)

	cpu := createFile(t, dir, "cpu.pprof")
	defer cpu.Close()
	if err := pprof.StartCPUProfile(cpu); err != nil {
		t.Fatal(err)
	}
	defer pprof.StopCPUProfile()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	goroutinesBefore := runtime.NumGoroutine()
	began := time.Now()
	finished := make(chan error, 1)
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		_, err := program.Run()
		finished <- err
	}()
	defer func() {
		program.Kill()
		<-exited
	}()

	// Capture while the animation is live. GC makes the in-use heap a
	// retained-memory snapshot.
	select {
	case err := <-finished:
		t.Fatalf("profiling program ended before the snapshot: %v", err)
	case <-time.After(duration / 2):
	}
	goroutinesActive := runtime.NumGoroutine()
	runtime.GC()
	for _, name := range []string{"heap", "goroutine"} {
		writeRuntimeProfile(t, dir, name)
	}
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(duration + 5*time.Second):
		t.Fatal("profiling program did not stop")
	}
	elapsed := time.Since(began)
	runtime.ReadMemStats(&after)
	goroutinesAfter := runtime.NumGoroutine()
	writes, written := sink.Totals()
	pprof.StopCPUProfile()
	runtime.GC()
	writeRuntimeProfile(t, dir, "allocs")
	metrics := map[string]any{
		"width": s.Width, "height": s.Height,
		"go_version": runtime.Version(), "goos": runtime.GOOS, "goarch": runtime.GOARCH,
		"gomaxprocs": runtime.GOMAXPROCS(0), "elapsed_seconds": elapsed.Seconds(),
		"updates": model.updates, "views": model.views,
		"updates_per_second": float64(model.updates) / elapsed.Seconds(),
		"views_per_second":   float64(model.views) / elapsed.Seconds(),
		"renderer":           "bubbletea-cursed", "renderer_max_fps": 60, "color_profile": ColorProfile.String(),
		"terminal_writes": writes, "terminal_bytes": written,
		"terminal_writes_per_second": float64(writes) / elapsed.Seconds(),
		"terminal_bytes_per_second":  float64(written) / elapsed.Seconds(),
		"allocated_bytes":            after.TotalAlloc - before.TotalAlloc, "allocations": after.Mallocs - before.Mallocs,
		"goroutines_before": goroutinesBefore, "goroutines_active": goroutinesActive,
		"goroutines_after": goroutinesAfter,
	}
	maps.Copy(metrics, s.Metrics)
	file := createFile(t, dir, "metrics.json")
	defer file.Close()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(metrics); err != nil {
		t.Fatal(err)
	}
	t.Logf("%d page updates, %d views, %d terminal writes (%d bytes) in %s; profiles: %s",
		model.updates, model.views, writes, written, elapsed, dir)
}

func writeRuntimeProfile(t *testing.T, dir, name string) {
	t.Helper()
	file := createFile(t, dir, name+".pprof")
	err := pprof.Lookup(name).WriteTo(file, 0)
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatalf("write %s profile: %v; close: %v", name, err, closeErr)
	}
}

func createFile(t *testing.T, dir, name string) *os.File {
	t.Helper()
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(fmt.Errorf("create %s: %w", name, err))
	}
	return file
}
