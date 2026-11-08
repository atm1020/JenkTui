package job

import (
	stdctx "context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/atm1020/jenktui/src/poll"
)

func TestJobPollFailureOrderingAndRecovery(t *testing.T) {
	for _, order := range []string{"failure-first", "job-first"} {
		t.Run(order, func(t *testing.T) {
			ctx, snapshot := primedJobPoll(t)
			model := NewModelFromUrl(ctx, 160, 45, snapshot.Target.JobURL)
			defer func() { _ = model.CleanUp() }()
			watch, _, err := ctx.PollManager.Watch(poll.WatchSpec{Target: snapshot.Target})
			if err != nil {
				t.Fatal(err)
			}
			model.pollWatch = watch
			job, err := ctx.Jenkins.GetJob(stdctx.Background(), "demo")
			if err != nil {
				t.Fatal(err)
			}
			loaded := JobLoadedMsg{Job: job}
			failure := poll.PollFailure{Target: snapshot.Target, Generation: snapshot.Generation, At: snapshot.ObservedAt.Add(time.Second), Err: errors.New("permission denied")}
			// Neither another target nor another watch generation may strand this page.
			stale := failure
			stale.Generation++
			model.ApplyPollFailure(stale)
			stale = failure
			stale.Target.JobURL += "other/"
			model.ApplyPollFailure(stale)
			if model.runsError != "" {
				t.Fatal("unrelated failure applied")
			}
			if order == "job-first" {
				model.handleJobLoadedMsg(loaded)
			}
			model.ApplyPollFailure(failure)
			if order == "failure-first" {
				model.handleJobLoadedMsg(loaded)
			}
			if model.loading || !strings.Contains(model.View(), "permission denied") {
				t.Fatal("job/failure ordering lost visible failure state")
			}
			// Replayed state older than the failure cannot clear its feedback.
			model.ApplyPollSnapshot(snapshot)
			if model.runsLoaded || model.runsError == "" {
				t.Fatal("older snapshot cleared current failure")
			}
			stale = failure
			stale.At = failure.At.Add(-time.Second)
			stale.Err = errors.New("old failure")
			model.ApplyPollFailure(stale)
			if strings.Contains(model.View(), "old failure") {
				t.Fatal("older failure replaced current feedback")
			}
			snapshot.ObservedAt = failure.At.Add(time.Second)
			model.ApplyPollSnapshot(snapshot)
			if model.loading || model.runsError != "" || !strings.Contains(model.View(), "Compile") {
				t.Fatal("successful retry failed to restore usable stages")
			}
			// Once cached runs exist, a failed refresh must not replace usable content.
			failure.At = snapshot.ObservedAt.Add(time.Second)
			model.ApplyPollFailure(failure)
			if model.runsError != "" {
				t.Fatal("refresh failure discarded usable runs")
			}
			model.CleanUp()
			model.CleanUp()
			model.runsLoaded = false
			model.ApplyPollFailure(failure)
			if model.runsError != "" {
				t.Fatal("failure survived watch cleanup")
			}
		})
	}
}
