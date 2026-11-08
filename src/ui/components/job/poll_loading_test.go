package job

import (
	stdctx "context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/atm1020/gojenkins"
	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/poll"
)

func primedJobPoll(t *testing.T) (*appcontext.ProgramContext, poll.Snapshot) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/job/demo/api/json" {
			_, _ = w.Write([]byte(`{"name":"demo","url":"http://` + r.Host + `/job/demo/","_class":"org.jenkinsci.plugins.workflow.job.WorkflowJob"}`))
		} else {
			if r.URL.Query().Get("fullStages") != "true" {
				t.Error("runs request omitted fullStages=true")
			}
			_, _ = w.Write([]byte(`[{"id":"42","status":"SUCCESS","endTimeMillis":2,"stages":[{"id":"1","name":"Compile","status":"SUCCESS"}]}]`))
		}
	}))
	t.Cleanup(server.Close)
	api := gojenkins.CreateJenkins(nil, server.URL)
	manager := poll.NewManager(api, nil, poll.Config{PollInterval: time.Hour})
	manager.Start()
	t.Cleanup(manager.Stop)
	watch, _, err := manager.Watch(poll.WatchSpec{Target: poll.Target{Kind: poll.TargetJobWindow, JobURL: server.URL + "/job/demo/"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(watch.Release)
	select {
	case delivery := <-manager.Results():
		if len(delivery.Batch.Snapshots) != 1 {
			t.Fatalf("delivery=%+v", delivery.Batch)
		}
		return &appcontext.ProgramContext{Jenkins: api, Config: config.GetDefaultConfig(), PollManager: manager}, delivery.Batch.Snapshots[0]
	case <-time.After(time.Second):
		t.Fatal("poll timed out")
	}
	return nil, poll.Snapshot{}
}

func TestJobPollLoadingOrder(t *testing.T) {
	for _, order := range []string{"job-first", "snapshot-first", "empty-snapshot", "replay"} {
		t.Run(order, func(t *testing.T) {
			ctx, snapshot := primedJobPoll(t)
			model := NewModelFromUrl(ctx, 160, 45, snapshot.Target.JobURL)
			defer func() { _ = model.CleanUp() }()
			if order == "replay" {
				_ = model.Init()
				if !model.runsLoaded || !model.loading || model.jobLoaded {
					t.Fatal("replay must supply runs without completing the job request")
				}
			} else {
				// Acquire the same capability but deliberately withhold its replay, to
				// exercise each delivery ordering independently of Init's replay path.
				watch, _, err := ctx.PollManager.Watch(poll.WatchSpec{Target: snapshot.Target})
				if err != nil {
					t.Fatal(err)
				}
				model.pollWatch = watch
			}
			job, err := ctx.Jenkins.GetJob(stdctx.Background(), "demo")
			if err != nil {
				t.Fatal(err)
			}
			loaded := JobLoadedMsg{Generation: model.requestGeneration, Job: job}
			if order == "empty-snapshot" {
				snapshot.Runs = nil
			}
			if order == "job-first" {
				model.handleJobLoadedMsg(loaded)
				if !model.loading {
					t.Fatal("job alone completed loading")
				}
				model.ApplyPollSnapshot(snapshot)
			} else {
				model.ApplyPollSnapshot(snapshot)
				if !model.loading {
					t.Fatal("snapshot alone completed loading")
				}
				model.handleJobLoadedMsg(loaded)
			}
			if model.loading {
				t.Fatal("both prerequisites available but still loading")
			}
			if order != "empty-snapshot" {
				run := model.stageView.pipelineRuns[0]
				if run.Job != job || run.Base != job.Base+"/42" || len(run.Stages) != 1 {
					t.Fatalf("unbound/incomplete run: %+v", run)
				}
			}
			// Older observations in the same generation and other watch generations
			// must not replace the accepted state.
			stale := snapshot
			stale.ObservedAt = snapshot.ObservedAt.Add(-time.Second)
			stale.Runs = nil
			model.ApplyPollSnapshot(stale)
			stale.ObservedAt = snapshot.ObservedAt.Add(time.Second)
			stale.Generation++
			model.ApplyPollSnapshot(stale)
			if len(model.pipelineRuns) != len(snapshot.Runs) {
				t.Fatal("stale snapshot overwrote state")
			}
			generation := model.requestGeneration
			model.CleanUp()
			model.CleanUp()
			model.ApplyPollSnapshot(snapshot)
			model.handleJobLoadedMsg(JobLoadedMsg{Generation: generation, Job: &gojenkins.Job{}})
			if model.job != job || model.pollWatch != nil {
				t.Fatal("late result survived cleanup")
			}
			// Reacquiring after our double cleanup must retain the priming owner's
			// generation; a double release would have removed that owner's demand.
			watch, _, err := ctx.PollManager.Watch(poll.WatchSpec{Target: snapshot.Target})
			if err != nil {
				t.Fatal(err)
			}
			defer watch.Release()
			if !watch.Matches(snapshot) {
				t.Fatal("cleanup released another owner's watch")
			}
		})
	}
}

func TestJobStandaloneLoadsFullStages(t *testing.T) {
	ctx, snapshot := primedJobPoll(t)
	ctx.PollManager = nil
	model := NewModelFromUrl(ctx, 160, 45, snapshot.Target.JobURL)
	defer func() { _ = model.CleanUp() }()
	loaded := model.fetchJob()().(JobLoadedMsg)
	if loaded.Error != nil {
		t.Fatal(loaded.Error)
	}
	model.handleJobLoadedMsg(loaded)
	if model.loading || len(model.stageView.pipelineRuns) != 1 || len(model.stageView.pipelineRuns[0].Stages) != 1 {
		t.Fatal("standalone load failed")
	}
}
