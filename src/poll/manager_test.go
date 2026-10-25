package poll

import (
	"context"
	"testing"
	"time"
)

type noopExecutor struct{}

func (noopExecutor) execute(context.Context, jobWork) error { return nil }

func TestConfigDefaults(t *testing.T) {
	cfg := withDefaults(Config{})

	if cfg.PollInterval != 3*time.Second {
		t.Fatalf("PollInterval = %s, want 3s", cfg.PollInterval)
	}
	if cfg.MaxParallelJobs != 2 {
		t.Fatalf("MaxParallelJobs = %d, want 2", cfg.MaxParallelJobs)
	}
	if cfg.RequestTimeout != 15*time.Second {
		t.Fatalf("RequestTimeout = %s, want 15s", cfg.RequestTimeout)
	}
	if cfg.RetryBase != 500*time.Millisecond {
		t.Fatalf("RetryBase = %s, want 500ms", cfg.RetryBase)
	}
	if cfg.RetryMax != 30*time.Second {
		t.Fatalf("RetryMax = %s, want 30s", cfg.RetryMax)
	}
}

func TestManagerStartThenStop(t *testing.T) {
	manager := newManager(Config{}, noopExecutor{})
	manager.Start()

	stopped := make(chan struct{})
	go func() {
		manager.Stop()
		close(stopped)
	}()

	select {
	case <-stopped:
	case <-time.After(time.Second):
		t.Fatal("Stop did not wait for and join the manager loop")
	}
}
