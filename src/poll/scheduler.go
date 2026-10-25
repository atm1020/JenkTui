package poll

import (
	"context"
	"sort"
	"sync"
	"time"
)

// jobExecutor is the scheduler's only work seam. Implementations must return
// when ctx is canceled.
type jobExecutor interface {
	execute(ctx context.Context, work jobWork) error
}

// jobWork groups every current target for one Jenkins job. One worker executes
// a job at a time, so a window and exact runs for the same job cannot overlap.
type jobWork struct {
	jobURL  string
	demands []watchDemand
}

type workResult struct {
	work        jobWork
	completedAt time.Time
	err         error
}

type scheduledTarget struct {
	generation uint64
	due        time.Time
}

func (m *Manager) signalWake() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

func (m *Manager) runScheduler() {
	scheduled := make(map[Target]scheduledTarget)
	inFlight := make(map[string]bool)
	failureAttempts := make(map[string]int)
	var workers sync.WaitGroup

	timer := time.NewTimer(time.Hour)
	stopTimer(timer)
	var timerC <-chan time.Time

	for {
		if m.ctx.Err() != nil {
			stopTimer(timer)
			workers.Wait()
			return
		}

		now := time.Now()
		reconcileScheduledTargets(scheduled, m.watches.demandsSnapshot(), now)
		m.cleanupInactiveWindows(scheduled, failureAttempts)
		m.dispatchDueWork(scheduled, inFlight, now, &workers)
		timerC = resetSchedulerTimer(timer, scheduled, inFlight, m.config.MaxParallelJobs, now)

		select {
		case <-m.ctx.Done(): // shutdown
			stopTimer(timer)
			workers.Wait()
			return
		case <-m.wake: // target added (m.Start()) or released (m.Released())
		// worker completed.
		// The possible faster reqest between tow group call is handled by the global rate limiter TODO
		case result := <-m.results:
			delete(inFlight, result.work.jobURL)
			m.applyWorkResult(scheduled, failureAttempts, result)
		case <-timerC: // next schedule
		}
	}
}

func reconcileScheduledTargets(
	scheduled map[Target]scheduledTarget,
	demands []watchDemand,
	now time.Time,
) {
	current := make(map[Target]uint64, len(demands))
	for _, demand := range demands {
		current[demand.Target] = demand.Generation
	}

	for target, state := range scheduled {
		generation, exists := current[target]
		if !exists || generation != state.generation {
			delete(scheduled, target)
		}
	}
	for _, demand := range demands {
		if _, exists := scheduled[demand.Target]; !exists {
			scheduled[demand.Target] = scheduledTarget{generation: demand.Generation, due: now}
		}
	}
}

func (m *Manager) dispatchDueWork(
	scheduled map[Target]scheduledTarget,
	inFlight map[string]bool,
	now time.Time,
	workers *sync.WaitGroup,
) {
	jobs := dueJobs(scheduled, inFlight, now)
	for _, jobURL := range jobs {
		if len(inFlight) >= m.config.MaxParallelJobs {
			return
		}
		work := workForJob(jobURL, scheduled)
		inFlight[jobURL] = true
		workers.Add(1)
		go m.executeWork(work, workers)
	}
}

func dueJobs(scheduled map[Target]scheduledTarget, inFlight map[string]bool, now time.Time) []string {
	seen := make(map[string]struct{})
	for target, state := range scheduled {
		if !inFlight[target.JobURL] && !state.due.After(now) {
			seen[target.JobURL] = struct{}{}
		}
	}
	jobs := make([]string, 0, len(seen))
	for jobURL := range seen {
		jobs = append(jobs, jobURL)
	}
	sort.Strings(jobs)
	return jobs
}

func workForJob(jobURL string, scheduled map[Target]scheduledTarget) jobWork {
	work := jobWork{jobURL: jobURL}
	for target, state := range scheduled {
		if target.JobURL == jobURL {
			work.demands = append(work.demands, watchDemand{
				Target:     target,
				Generation: state.generation,
			})
		}
	}
	sort.Slice(work.demands, func(i, j int) bool {
		left, right := work.demands[i].Target, work.demands[j].Target
		if left.Kind != right.Kind {
			return left.Kind < right.Kind
		}
		return left.BuildNumber < right.BuildNumber
	})
	return work
}

func (m *Manager) executeWork(work jobWork, workers *sync.WaitGroup) {
	defer workers.Done()

	ctx, cancel := context.WithTimeout(m.ctx, m.config.RequestTimeout)
	err := m.executor.execute(ctx, work)
	cancel()
	result := workResult{work: work, completedAt: time.Now(), err: err}

	select {
	case m.results <- result:
	case <-m.ctx.Done():
	}
}

func (m *Manager) applyWorkResult(
	scheduled map[Target]scheduledTarget,
	failureAttempts map[string]int,
	result workResult,
) {
	matched := make([]watchDemand, 0, len(result.work.demands))
	for _, demand := range result.work.demands {
		state, exists := scheduled[demand.Target]
		if exists && state.generation == demand.Generation {
			matched = append(matched, demand)
		}
	}
	if len(matched) == 0 {
		return
	}

	delay := m.config.PollInterval
	if result.err != nil {
		failureAttempts[result.work.jobURL]++
		delay = failureRetryDelay(m.config, failureAttempts[result.work.jobURL])
	} else {
		delete(failureAttempts, result.work.jobURL)
	}
	nextDue := result.completedAt.Add(delay)
	for _, demand := range matched {
		state := scheduled[demand.Target]
		state.due = nextDue
		scheduled[demand.Target] = state
	}
}

func failureRetryDelay(config Config, attempt int) time.Duration {
	delay := config.RetryBase
	for i := 1; i < attempt && delay < config.RetryMax; i++ {
		if delay > config.RetryMax/2 {
			return config.RetryMax
		}
		delay *= 2
	}
	if delay > config.RetryMax {
		return config.RetryMax
	}
	return delay
}

func resetSchedulerTimer(
	timer *time.Timer,
	scheduled map[Target]scheduledTarget,
	inFlight map[string]bool,
	maxParallel int,
	now time.Time,
) <-chan time.Time {
	stopTimer(timer)
	if len(inFlight) >= maxParallel {
		return nil
	}

	var next time.Time
	for target, state := range scheduled {
		if inFlight[target.JobURL] || (!next.IsZero() && !state.due.Before(next)) {
			continue
		}
		next = state.due
	}
	if next.IsZero() {
		return nil
	}
	delay := next.Sub(now)
	if delay < 0 {
		delay = 0
	}
	timer.Reset(delay)
	return timer.C
}

func stopTimer(timer *time.Timer) {
	if timer.Stop() {
		return
	}
	select {
	case <-timer.C:
	default:
	}
}
