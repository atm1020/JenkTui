package poll

import (
	"context"
	"errors"
	"testing"
	"time"
)

type controlledExecution struct {
	work   jobWork
	finish chan error
}

type controlledExecutor struct {
	calls chan *controlledExecution
}

func newControlledExecutor() *controlledExecutor {
	return &controlledExecutor{calls: make(chan *controlledExecution)}
}

func (e *controlledExecutor) execute(ctx context.Context, work jobWork) error {
	call := &controlledExecution{
		work:   work,
		finish: make(chan error, 1),
	}

	select {
	case e.calls <- call:
	case <-ctx.Done():
		return ctx.Err()
	}

	select {
	case err := <-call.finish:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func nextExecution(t *testing.T, executor *controlledExecutor) *controlledExecution {
	t.Helper()
	select {
	case call := <-executor.calls:
		return call
	case <-time.After(time.Second):
		t.Fatal("executor did not receive work")
		return nil
	}
}

// finishWork completes one execution and returns once the scheduler has
// applied the result, so a following advance measures from the recorded
// completion instead of racing it.
func finishWork(t *testing.T, clock *fakeClock, call *controlledExecution, err error) {
	t.Helper()
	mark := clock.iteration()
	call.finish <- err
	clock.waitIteration(t, mark+1)
}

// assertNoExecution fails if work reaches the executor within wait. Under a
// fakeClock the wait is only a grace period for a wrongly dispatched worker to
// surface, because frozen time cannot make a poll due on its own.
func assertNoExecution(t *testing.T, executor *controlledExecutor, wait time.Duration) {
	t.Helper()
	select {
	case call := <-executor.calls:
		t.Fatalf("unexpected execution for %s", call.work.jobURL)
	case <-time.After(wait):
	}
}

type timeoutExecutor struct {
	result chan error
}

func (e *timeoutExecutor) execute(ctx context.Context, _ jobWork) error {
	<-ctx.Done()
	e.result <- ctx.Err()
	return ctx.Err()
}

type joinedCancellationExecutor struct {
	started     chan struct{}
	canceled    chan struct{}
	allowReturn chan struct{}
}

func (e *joinedCancellationExecutor) execute(ctx context.Context, _ jobWork) error {
	close(e.started)
	<-ctx.Done()
	close(e.canceled)
	<-e.allowReturn
	return ctx.Err()
}

func TestSchedulerStartsNewDemandAndHonorsGlobalBound(t *testing.T) {
	executor := newControlledExecutor()
	manager := newManager(Config{
		PollInterval:    time.Hour,
		MaxParallelJobs: 2,
		RequestTimeout:  time.Hour,
	}, executor)
	manager.Start()
	t.Cleanup(manager.Stop)

	for _, jobURL := range []string{"https://jenkins/job/a/", "https://jenkins/job/b/", "https://jenkins/job/c/"} {
		watch, _, err := manager.Watch(WatchSpec{Target: Target{Kind: TargetJobWindow, JobURL: jobURL}})
		if err != nil {
			t.Fatalf("Watch(%s): %v", jobURL, err)
		}
		t.Cleanup(watch.Release)
	}

	first := nextExecution(t, executor)
	second := nextExecution(t, executor)
	if first.work.jobURL == second.work.jobURL {
		t.Fatalf("first two executions both used %s, want two different jobs", first.work.jobURL)
	}
	assertNoExecution(t, executor, 50*time.Millisecond)

	first.finish <- nil
	third := nextExecution(t, executor)
	if third.work.jobURL == first.work.jobURL || third.work.jobURL == second.work.jobURL {
		t.Fatalf("third execution used %s, want remaining job", third.work.jobURL)
	}

	second.finish <- nil
	third.finish <- nil
}

func TestSchedulerDoesNotOverlapWorkForOneJob(t *testing.T) {
	executor := newControlledExecutor()
	manager := newManager(Config{
		PollInterval:    time.Hour,
		MaxParallelJobs: 2,
		RequestTimeout:  time.Hour,
	}, executor)
	manager.Start()
	t.Cleanup(manager.Stop)

	jobURL := "https://jenkins/job/demo/"
	window, _, err := manager.Watch(WatchSpec{Target: Target{Kind: TargetJobWindow, JobURL: jobURL}})
	if err != nil {
		t.Fatalf("watch window: %v", err)
	}
	t.Cleanup(window.Release)
	first := nextExecution(t, executor)

	exact, _, err := manager.Watch(WatchSpec{Target: Target{
		Kind: TargetExactRun, JobURL: jobURL, BuildNumber: 42,
	}})
	if err != nil {
		t.Fatalf("watch exact run: %v", err)
	}
	t.Cleanup(exact.Release)
	assertNoExecution(t, executor, 50*time.Millisecond)

	first.finish <- nil
	second := nextExecution(t, executor)
	if len(second.work.demands) != 2 {
		t.Fatalf("second execution has %d targets, want window and exact run", len(second.work.demands))
	}
	second.finish <- nil
}

func TestReacquiredTargetIsNotDelayedByStaleWork(t *testing.T) {
	executor := newControlledExecutor()
	manager := newManager(Config{
		PollInterval:    time.Hour,
		MaxParallelJobs: 1,
		RequestTimeout:  time.Hour,
	}, executor)
	manager.Start()
	t.Cleanup(manager.Stop)

	target := Target{Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/"}
	firstWatch, _, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("first Watch: %v", err)
	}
	firstCall := nextExecution(t, executor)

	firstWatch.Release()
	secondWatch, _, err := manager.Watch(WatchSpec{Target: target})
	if err != nil {
		t.Fatalf("second Watch: %v", err)
	}
	t.Cleanup(secondWatch.Release)
	assertNoExecution(t, executor, 50*time.Millisecond)

	firstCall.finish <- nil
	secondCall := nextExecution(t, executor)
	secondCall.finish <- nil
}

func TestSchedulerCadenceStartsAfterWorkCompletes(t *testing.T) {
	executor := newControlledExecutor()
	clock := newFakeClock()
	manager := newManager(Config{
		PollInterval:    120 * time.Millisecond,
		MaxParallelJobs: 1,
		RequestTimeout:  time.Second,
	}, executor)
	manager.clock = clock
	manager.Start()
	t.Cleanup(manager.Stop)

	watch, _, err := manager.Watch(WatchSpec{Target: Target{
		Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/",
	}})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(watch.Release)

	first := nextExecution(t, executor)
	// The poll outlives one interval, so cadence measured from dispatch would
	// make the next one due the moment this one completes.
	clock.advance(t, 150*time.Millisecond)
	finishWork(t, clock, first, nil)

	clock.advance(t, 119*time.Millisecond)
	assertNoExecution(t, executor, 20*time.Millisecond)

	clock.advance(t, time.Millisecond)
	second := nextExecution(t, executor)
	second.finish <- nil
}

func TestSchedulerRemovesReleasedDemand(t *testing.T) {
	executor := newControlledExecutor()
	manager := newManager(Config{
		PollInterval:    30 * time.Millisecond,
		MaxParallelJobs: 1,
		RequestTimeout:  time.Second,
	}, executor)
	manager.Start()
	t.Cleanup(manager.Stop)

	watch, _, err := manager.Watch(WatchSpec{Target: Target{
		Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/",
	}})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	call := nextExecution(t, executor)
	watch.Release()
	call.finish <- nil

	assertNoExecution(t, executor, 100*time.Millisecond)
}

func TestSchedulerUsesBoundedFailureBackoff(t *testing.T) {
	executor := newControlledExecutor()
	clock := newFakeClock()
	manager := newManager(Config{
		PollInterval:    time.Hour,
		MaxParallelJobs: 1,
		RequestTimeout:  time.Second,
		RetryBase:       20 * time.Millisecond,
		RetryMax:        60 * time.Millisecond,
	}, executor)
	manager.clock = clock
	manager.Start()
	t.Cleanup(manager.Stop)

	watch, _, err := manager.Watch(WatchSpec{Target: Target{
		Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/",
	}})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(watch.Release)

	// Retries are due after RetryBase, then double to 40ms, then stay capped
	// at RetryMax for every later attempt.
	for _, delay := range []time.Duration{
		20 * time.Millisecond,
		40 * time.Millisecond,
		60 * time.Millisecond,
		60 * time.Millisecond,
	} {
		call := nextExecution(t, executor)
		finishWork(t, clock, call, errors.New("poll failure"))

		clock.advance(t, delay-time.Millisecond)
		assertNoExecution(t, executor, 20*time.Millisecond)
		clock.advance(t, time.Millisecond)
	}

	last := nextExecution(t, executor)
	last.finish <- nil
}

func TestSchedulerAppliesRequestTimeout(t *testing.T) {
	executor := &timeoutExecutor{result: make(chan error, 1)}
	manager := newManager(Config{
		PollInterval:    time.Hour,
		MaxParallelJobs: 1,
		RequestTimeout:  30 * time.Millisecond,
		RetryBase:       time.Hour,
		RetryMax:        time.Hour,
	}, executor)
	manager.Start()
	t.Cleanup(manager.Stop)

	watch, _, err := manager.Watch(WatchSpec{Target: Target{
		Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/",
	}})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	t.Cleanup(watch.Release)

	select {
	case err := <-executor.result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("executor context error = %v, want deadline exceeded", err)
		}
	case <-time.After(time.Second):
		t.Fatal("executor did not receive request timeout")
	}
}

func TestManagerStopCancelsAndJoinsRunningWork(t *testing.T) {
	executor := &joinedCancellationExecutor{
		started:     make(chan struct{}),
		canceled:    make(chan struct{}),
		allowReturn: make(chan struct{}),
	}
	manager := newManager(Config{
		PollInterval:    time.Hour,
		MaxParallelJobs: 1,
		RequestTimeout:  time.Hour,
	}, executor)
	manager.Start()

	watch, _, err := manager.Watch(WatchSpec{Target: Target{
		Kind: TargetJobWindow, JobURL: "https://jenkins/job/demo/",
	}})
	if err != nil {
		t.Fatalf("Watch: %v", err)
	}
	defer watch.Release()

	select {
	case <-executor.started:
	case <-time.After(time.Second):
		t.Fatal("executor did not start")
	}

	stopped := make(chan struct{})
	go func() {
		manager.Stop()
		close(stopped)
	}()

	select {
	case <-executor.canceled:
	case <-time.After(time.Second):
		t.Fatal("running work did not receive cancellation")
	}
	select {
	case <-stopped:
		t.Fatal("Stop returned before running work exited")
	case <-time.After(50 * time.Millisecond):
	}

	close(executor.allowReturn)
	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not join canceled work")
	}
}
