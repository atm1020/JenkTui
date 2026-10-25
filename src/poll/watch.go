package poll

import (
	"errors"
	"slices"
	"sync"
)

const maxReplaySnapshots = 64

// WatchSpec describes one temporary reason to keep a target fresh.
type WatchSpec struct {
	Target Target
}

// Watch is one caller's capability to match snapshots and release demand.
type Watch struct {
	manager     *Manager
	target      Target
	generation  uint64
	releaseOnce sync.Once
}

// Matches reports whether a snapshot belongs to this watch generation.
func (w *Watch) Matches(snapshot Snapshot) bool {
	return w != nil && snapshot.Target == w.target && snapshot.Generation == w.generation
}

// Release removes this watch's demand. Other watches for the target remain.
func (w *Watch) Release() {
	if w == nil {
		return
	}
	w.releaseOnce.Do(func() {
		w.manager.releaseWatch(w.target)
	})
}

// watchDemandState is the one compact mutable record shared by every owner of
// a target.
type watchDemandState struct {
	owners     int
	generation uint64
}

// watchDemand is the immutable projection consumed by the scheduler.
type watchDemand struct {
	Target     Target
	Generation uint64
}

// watchRegistry synchronizes demand and replay mutations. It performs no I/O
// and does not decide when work is scheduled.
type watchRegistry struct {
	mu             sync.Mutex
	demands        map[Target]watchDemandState
	nextGeneration uint64
	replays        map[Target]Snapshot
	replayOrder    []Target
}

func newWatchRegistry() watchRegistry {
	return watchRegistry{
		demands: make(map[Target]watchDemandState),
		replays: make(map[Target]Snapshot),
	}
}

// Watch registers polling demand synchronously and wakes the scheduler.
func (m *Manager) Watch(spec WatchSpec) (*Watch, Replay, error) {
	if err := validateTarget(spec.Target); err != nil {
		return nil, Replay{}, err
	} // This is really needed?

	generation, replay, activated := m.watches.acquire(spec)
	if activated {
		m.signalWake()
	}
	return &Watch{
		manager:    m,
		target:     spec.Target,
		generation: generation,
	}, replay, nil
}

func validateTarget(target Target) error {
	if target.JobURL == "" {
		return errors.New("job URL cannot be empty")
	}
	switch target.Kind {
	case TargetJobWindow:
		if target.BuildNumber != 0 {
			return errors.New("job-window target cannot include a build number")
		}
	case TargetExactRun:
		if target.BuildNumber <= 0 {
			return errors.New("exact-run target requires a positive build number")
		}
	default:
		return errors.New("unknown target kind")
	}
	return nil
}

func (r *watchRegistry) acquire(spec WatchSpec) (uint64, Replay, bool) {
	r.mu.Lock()
	state := r.demands[spec.Target]
	activated := state.owners == 0
	if activated {
		r.nextGeneration++
		state.generation = r.nextGeneration
	}
	state.owners++
	r.demands[spec.Target] = state

	var replay Replay
	if cached, ok := r.replays[spec.Target]; ok {
		cached = cloneSnapshot(cached)
		cached.Generation = state.generation
		replay = Replay{Snapshot: cached, OK: true}
		r.touchReplayLocked(spec.Target)
	}
	r.mu.Unlock()

	return state.generation, replay, activated
}

func (m *Manager) releaseWatch(target Target) {
	if m.watches.release(target) {
		m.signalWake()
	}
}

func (r *watchRegistry) release(target Target) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.demands[target]
	if !ok {
		return false
	}
	state.owners--
	if state.owners == 0 {
		delete(r.demands, target)
		return true
	}
	r.demands[target] = state
	return false
}

// recordSnapshot accepts state only for the target's current lifetime. Replay
// is deliberately separate from jobWindow, so cache eviction cannot change a
// polling cursor.
func (r *watchRegistry) recordSnapshot(snapshot Snapshot) bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.demands[snapshot.Target]
	if !ok || state.generation != snapshot.Generation {
		return false
	}
	r.replays[snapshot.Target] = cloneSnapshot(snapshot)
	r.touchReplayLocked(snapshot.Target)
	if len(r.replayOrder) > maxReplaySnapshots {
		evicted := r.replayOrder[0]
		r.replayOrder = r.replayOrder[1:]
		delete(r.replays, evicted)
	}
	return true
}

func (r *watchRegistry) touchReplayLocked(target Target) {
	if index := slices.Index(r.replayOrder, target); index >= 0 {
		r.replayOrder = append(r.replayOrder[:index], r.replayOrder[index+1:]...)
	}
	r.replayOrder = append(r.replayOrder, target)
}

func (r *watchRegistry) demandsSnapshot() []watchDemand {
	r.mu.Lock()
	defer r.mu.Unlock()

	demands := make([]watchDemand, 0, len(r.demands))
	for target, state := range r.demands {
		demands = append(demands, watchDemand{
			Target:     target,
			Generation: state.generation,
		})
	}
	return demands
}

func (r *watchRegistry) current(demand watchDemand) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	state, ok := r.demands[demand.Target]
	return ok && state.generation == demand.Generation
}

func (r *watchRegistry) demand(target Target) (watchDemand, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state, ok := r.demands[target]
	if !ok {
		return watchDemand{}, false
	}
	return watchDemand{
		Target:     target,
		Generation: state.generation,
	}, true
}
