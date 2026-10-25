package buildaction

import (
	"testing"

	"github.com/atm1020/jenktui/src/ui/util"

	"github.com/atm1020/gojenkins"
)

func TestOpenJobUsesResolvedJobWithoutARequest(t *testing.T) {
	job := &gojenkins.Job{Raw: &gojenkins.JobResponse{Name: "demo", URL: "http://jenkins/job/demo/"}}

	msg := OpenJob(job)()
	open, ok := msg.(util.OpenJobComponentMsg)
	if !ok {
		t.Fatalf("OpenJob result = %T, want util.OpenJobComponentMsg", msg)
	}
	if open.Job != job {
		t.Fatalf("OpenJob job = %p, want %p", open.Job, job)
	}
}

func TestOpenJobByURLUsesCentralJobRouter(t *testing.T) {
	msg := OpenJobByURL("http://jenkins/job/demo/", "demo")()
	open, ok := msg.(util.OpenJobTargetMsg)
	if !ok {
		t.Fatalf("OpenJobByURL result = %T, want util.OpenJobTargetMsg", msg)
	}
	if open.Target.URL != "http://jenkins/job/demo/" || open.Target.Name != "demo" {
		t.Fatalf("OpenJobByURL target = %#v, want stored job identity", open.Target)
	}
}

func TestOpenLogSupportsLiveAndStoredRuns(t *testing.T) {
	run := gojenkins.PipelineRun{ID: "42", Job: &gojenkins.Job{}}
	live, ok := OpenLog(run)().(OpenLogMsg)
	if !ok || live.PipelineRun == nil || live.PipelineRun.ID != "42" {
		t.Fatalf("OpenLog result = %#v, want live run", live)
	}

	stored, ok := OpenLogByURL("http://jenkins/job/demo/", "42")().(OpenLogMsg)
	if !ok || stored.PipelineRun != nil || stored.JobURL != "http://jenkins/job/demo/" || stored.BuildID != "42" {
		t.Fatalf("OpenLogByURL result = %#v, want stored run identity", stored)
	}
}

func TestOpenActionsRejectMissingIdentities(t *testing.T) {
	tests := []struct {
		name string
		cmd  func() any
	}{
		{name: "job", cmd: func() any { return OpenJob(nil)() }},
		{name: "job URL", cmd: func() any { return OpenJobByURL("", "")() }},
		{name: "live log", cmd: func() any { return OpenLog(gojenkins.PipelineRun{})() }},
		{name: "stored log", cmd: func() any { return OpenLogByURL("", "")() }},
		{name: "job browser", cmd: func() any { return OpenJobInBrowser("")() }},
		{name: "build browser", cmd: func() any { return OpenBuildInBrowser("", "")() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if msg := tt.cmd(); msg == nil {
				t.Fatal("missing identity returned no feedback")
			} else if _, ok := msg.(util.ErrorMsg); !ok {
				t.Fatalf("missing identity result = %T, want util.ErrorMsg", msg)
			}
		})
	}
}

func TestBrowserActionsEmitNormalizedURLs(t *testing.T) {
	job, ok := OpenJobInBrowser("http://jenkins/job/demo/")().(util.BrowserOpenMsg)
	if !ok || job.Url != "http://jenkins/job/demo/" {
		t.Fatalf("OpenJobInBrowser result = %#v, want job URL", job)
	}

	build, ok := OpenBuildInBrowser("http://jenkins/job/demo/", "42")().(util.BrowserOpenMsg)
	if !ok || build.Url != "http://jenkins/job/demo/42" {
		t.Fatalf("OpenBuildInBrowser result = %#v, want normalized build URL", build)
	}
}
