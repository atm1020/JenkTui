package context

import (
	stdctx "context"
	"testing"
	"time"
)

func TestRequestContextUsesConfiguredTimeout(t *testing.T) {
	program := &ProgramContext{Config: &Config{JenkinsRequestTimeoutSeconds: 7}}
	ctx, cancel := program.RequestContext()
	defer cancel()
	assertDeadlineNear(t, ctx, 7*time.Second)
}

func TestRequestContextFallsBackToDefault(t *testing.T) {
	for name, program := range map[string]*ProgramContext{
		"nil config":   {Config: nil},
		"zero":         {Config: &Config{}},
		"negative":     {Config: &Config{JenkinsRequestTimeoutSeconds: -1}},
		"nil receiver": nil,
	} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := program.RequestContext()
			defer cancel()
			assertDeadlineNear(t, ctx, defaultRequestTimeout)
		})
	}
}

func TestDownloadContextUsesLongerBudget(t *testing.T) {
	ctx, cancel := (&ProgramContext{}).DownloadContext()
	defer cancel()
	assertDeadlineNear(t, ctx, defaultDownloadTimeout)
}

func TestRequestContextsCanBeCancelledIdempotently(t *testing.T) {
	ctx, cancel := (&ProgramContext{}).RequestContext()
	cancel()
	cancel()
	if err := ctx.Err(); err != stdctx.Canceled {
		t.Fatalf("context error = %v, want context.Canceled", err)
	}
}

func assertDeadlineNear(t *testing.T, ctx stdctx.Context, want time.Duration) {
	t.Helper()
	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining < want-time.Second || remaining > want {
		t.Fatalf("deadline remaining = %v, want approximately %v", remaining, want)
	}
}
