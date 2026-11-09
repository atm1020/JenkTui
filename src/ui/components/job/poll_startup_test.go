package job

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/ui/util"
)

// Exercise concurrent destination commands and the shell's sole delivery path.
// Fixed latency also makes this a repeatable navigation measurement.
func TestJobStartupSharesPollRuns(t *testing.T) {
	var mu sync.Mutex
	type hit struct {
		path       string
		start, end time.Duration
	}
	var hits []hit
	press := time.Now()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Since(press)
		time.Sleep(150 * time.Millisecond)
		path := r.URL.RequestURI()
		mu.Lock()
		hits = append(hits, hit{path, start, time.Since(press)})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(path, "wfapi/runs") {
			stages := `[]`
			if r.URL.Query().Get("fullStages") == "true" {
				stages = `[{"id":"1","name":"Compile","status":"SUCCESS"}]`
			}
			_, _ = w.Write([]byte(`[{"id":"12","name":"#12","status":"SUCCESS","endTimeMillis":2,"stages":` + stages + `}]`))
		} else {
			if r.URL.Query().Get("tree") != "" {
				t.Error("job record must remain unfiltered for trigger parameters")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "pipe", "url": "http://" + r.Host + "/job/pipe/", "_class": "org.jenkinsci.plugins.workflow.job.WorkflowJob", "builds": []map[string]any{{"number": 12}}, "property": []map[string]any{{"parameterDefinitions": []map[string]string{{"name": "BRANCH", "type": "StringParameterDefinition"}}}}})
		}
	}))
	defer server.Close()
	api := gojenkins.CreateJenkins(nil, server.URL)
	manager := poll.NewManager(api, nil, poll.Config{PollInterval: time.Hour})
	manager.Start()
	defer manager.Stop()
	ctx := &appcontext.ProgramContext{Jenkins: api, Config: config.GetDefaultConfig(), PollManager: manager}
	press = time.Now()
	open := util.HandleJobTarget(jobref.Target{URL: server.URL + "/job/pipe/", Kind: jobref.KindPipeline}, ctx)().(util.OpenJobComponentMsg)
	model := NewModelFromUrl(ctx, 160, 45, open.URL)
	defer func() { _ = model.CleanUp() }()
	init := model.Init()
	t.Logf("first loading paint %.3fms: %v", float64(time.Since(press).Microseconds())/1000, strings.Contains(model.View(), "Loading job"))
	results := make(chan tea.Msg, 10)
	var execute func(tea.Cmd)
	execute = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		msg := cmd()
		if batch, ok := msg.(tea.BatchMsg); ok {
			for _, c := range batch {
				execute(c)
			}
			return
		}
		if _, ok := msg.(JobLoadedMsg); ok {
			results <- msg
		}
	}
	go execute(init)
	loaded, delivered := false, false
	firstReady, detail := time.Duration(0), time.Duration(0)
	timeout := time.After(3 * time.Second)
	for !loaded || !delivered {
		select {
		case msg := <-results:
			model.Update(msg)
			loaded = true
		case delivery := <-manager.Results():
			for _, s := range delivery.Batch.Snapshots {
				model.ApplyPollSnapshot(s)
				delivered = true
			}
		case <-timeout:
			t.Fatal("startup timed out")
		}
		if !model.loading && firstReady == 0 {
			firstReady = time.Since(press)
			if !strings.Contains(model.View(), "Compile") {
				t.Error("first ready paint lacks full stage detail")
			}
		}
		if strings.Contains(model.View(), "Compile") && detail == 0 {
			detail = time.Since(press)
		}
	}
	t.Logf("first ready %.3fms, stage detail %.3fms", float64(firstReady.Microseconds())/1000, float64(detail.Microseconds())/1000)
	mu.Lock()
	defer mu.Unlock()
	runs := 0
	for _, h := range hits {
		t.Logf("%.3f -> %.3fms %s", float64(h.start.Microseconds())/1000, float64(h.end.Microseconds())/1000, h.path)
		if strings.Contains(h.path, "wfapi/runs") {
			runs++
		}
	}
	t.Logf("total requests: %d", len(hits))
	if len(hits) != 3 || runs != 1 {
		t.Errorf("requests=%d runs=%d, want 3 and 1", len(hits), runs)
	}
	if model.job == nil || len(model.job.Raw.Property) != 1 || len(model.job.Raw.Property[0].ParameterDefinitions) != 1 || model.job.Raw.Property[0].ParameterDefinitions[0].Name != "BRANCH" {
		t.Fatal("full job parameters lost")
	}
}
