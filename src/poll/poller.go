package poll

import (
	"context"
	"errors"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
)

type pollingExecutor struct {
	manager *Manager
	source  observationSource
}

func (e pollingExecutor) execute(ctx context.Context, work jobWork) error {
	if err := e.manager.reconcileTriggers(ctx, work.jobURL); err != nil {
		return err
	}

	windowObservation, windowFetched, combined := e.fetchAndApplyDemandedWindow(ctx, work)

	if !windowFetched {
		var err error
		windowObservation, windowFetched, err = e.tryFetchWindowForExactRuns(ctx, work)
		if err != nil {
			return errors.Join(combined, err)
		}
	}

	exactErr := e.resolveAndApplyExactRunDemands(ctx, work.demands, windowObservation, windowFetched)
	return errors.Join(combined, exactErr)
}

// resolveAndApplyExactRunDemands resolves each exact target from the shared
// window, falling back to the exact-run endpoint when a build is absent.
func (e pollingExecutor) resolveAndApplyExactRunDemands(
	ctx context.Context,
	demands []watchDemand,
	window Observation,
	windowFetched bool,
) error {
	var combined error
	for _, demand := range demands {
		if demand.Target.Kind != TargetExactRun {
			continue
		}
		if windowFetched {
			if run, ok := findRun(window.Runs, demand.Target.BuildNumber); ok {
				observation := Observation{Target: demand.Target, ObservedAt: window.ObservedAt, Runs: []RunSnapshot{run}}
				if err := e.manager.applyExact(ctx, demand, observation); err != nil {
					e.manager.recordFailure(demand.Target, err)
					combined = errors.Join(combined, err)
				}
				continue
			}
		}
		observation, err := e.source.fetchExact(ctx, demand.Target)
		if err != nil {
			e.manager.recordFailure(demand.Target, err)
			combined = errors.Join(combined, err)
			continue
		}
		if err := e.manager.applyExact(ctx, demand, observation); err != nil {
			e.manager.recordFailure(demand.Target, err)
			combined = errors.Join(combined, err)
		}
	}
	return combined
}

// fetchAndApplyDemandedWindow fetches and applies explicit job-window demand
// first so exact-run demands for the same job can reuse its observation.
func (e pollingExecutor) fetchAndApplyDemandedWindow(ctx context.Context, work jobWork) (Observation, bool, error) {
	for _, demand := range work.demands {
		if demand.Target.Kind != TargetJobWindow {
			continue
		}
		cursor, full := e.manager.windowRequest(work.jobURL)
		observation, err := e.source.fetchWindow(ctx, work.jobURL, cursor)
		if err != nil {
			e.manager.recordFailure(demand.Target, err)
			return Observation{}, false, err
		}
		if err := e.manager.applyWindow(ctx, demand, observation, full); err != nil {
			e.manager.recordFailure(demand.Target, err)
			return Observation{}, false, err
		}
		return observation, true, nil
	}
	return Observation{}, false, nil
}

// tryFetchWindowForExactRuns fetches shared job state before falling back to
// one request per exact run. A normal window failure permits that fallback;
// cancellation stops the whole job worker.
func (e pollingExecutor) tryFetchWindowForExactRuns(ctx context.Context, work jobWork) (Observation, bool, error) {
	observation, err := e.source.fetchWindow(ctx, work.jobURL, oldestExactRunCursor(work.demands))
	if err == nil {
		return observation, true, nil
	}
	if ctx.Err() != nil {
		return Observation{}, false, err
	}
	return Observation{}, false, nil
}

func oldestExactRunCursor(demands []watchDemand) BuildNumber {
	var oldest BuildNumber
	for _, demand := range demands {
		if demand.Target.Kind != TargetExactRun {
			continue
		}
		number := demand.Target.BuildNumber
		if oldest == 0 || number < oldest {
			oldest = number
		}
	}
	return oldest
}

func (m *Manager) windowRequest(jobURL string) (BuildNumber, bool) {
	m.windowsMu.Lock()
	defer m.windowsMu.Unlock()
	window, ok := m.windows[jobURL]
	if !ok || window.needsFullReconciliation() {
		return 0, true
	}
	return window.cursor(), false
}

func (m *Manager) applyWindow(ctx context.Context, demand watchDemand, remote Observation, full bool) error {
	if !m.watches.current(demand) {
		return nil
	}
	if m.repo != nil {
		changed, err := m.repo.apply(ctx, remote)
		if err != nil {
			return err
		}
		if changed {
			if err := m.syncActiveRunDemand(ctx); err != nil {
				return err
			}
			m.markRunsChanged()
		}
	}
	m.windowsMu.Lock()
	window := m.windows[demand.Target.JobURL]
	window.apply(remote.Runs, full)
	m.windows[demand.Target.JobURL] = window
	canonical := window.snapshot()
	m.windowsMu.Unlock()
	m.publishSnapshot(Snapshot{Target: demand.Target, Generation: demand.Generation, ObservedAt: remote.ObservedAt, Runs: canonical})
	return nil
}

func (m *Manager) applyExact(ctx context.Context, demand watchDemand, observation Observation) error {
	if !m.watches.current(demand) {
		return nil
	}
	if m.repo != nil {
		changed, err := m.repo.apply(ctx, observation)
		if err != nil {
			return err
		}
		if changed {
			if err := m.syncActiveRunDemand(ctx); err != nil {
				return err
			}
			m.markRunsChanged()
		}
	}
	m.windowsMu.Lock()
	if window, ok := m.windows[demand.Target.JobURL]; ok && len(observation.Runs) == 1 {
		if window.mergeExact(observation.Runs[0]) {
			m.windows[demand.Target.JobURL] = window
		}
	}
	m.windowsMu.Unlock()
	m.publishSnapshot(Snapshot{Target: demand.Target, Generation: demand.Generation, ObservedAt: observation.ObservedAt, Runs: observation.Runs})
	if len(observation.Runs) == 1 && observation.Runs[0].Activity == buildstatus.ActivityFinished {
		m.releaseInternal(demand.Target)
	}
	return nil
}

func findRun(runs []RunSnapshot, number BuildNumber) (RunSnapshot, bool) {
	for i := range runs {
		if runs[i].BuildNumber == number {
			return cloneRun(runs[i]), true
		}
	}
	return RunSnapshot{}, false
}

func (m *Manager) syncActiveRunDemand(ctx context.Context) error {
	active, err := m.repo.activeRuns(ctx)
	if err != nil {
		return err
	}
	activeSet := make(map[Target]struct{}, len(active))
	for _, target := range active {
		activeSet[target] = struct{}{}
		m.ensureInternal(target)
	}

	m.internalMu.Lock()
	var ended []Target
	for target := range m.internalWatches {
		if target.Kind != TargetExactRun {
			continue
		}
		if _, ok := activeSet[target]; !ok {
			ended = append(ended, target)
		}
	}
	m.internalMu.Unlock()
	for _, target := range ended {
		m.releaseInternal(target)
	}
	return nil
}

func (m *Manager) cleanupInactiveWindows(scheduled map[Target]scheduledTarget, failureAttempts map[string]int) {
	activeJobs := make(map[string]struct{}, len(scheduled))
	for target := range scheduled {
		activeJobs[target.JobURL] = struct{}{}
	}
	for jobURL := range failureAttempts {
		if _, ok := activeJobs[jobURL]; !ok {
			delete(failureAttempts, jobURL)
		}
	}
	m.windowsMu.Lock()
	for jobURL := range m.windows {
		if _, ok := activeJobs[jobURL]; !ok {
			delete(m.windows, jobURL)
		}
	}
	m.windowsMu.Unlock()
}

func observedNow(observation Observation) time.Time {
	if observation.ObservedAt.IsZero() {
		return time.Now()
	}
	return observation.ObservedAt
}
