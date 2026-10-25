package build

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

var (
	nextStageKey = tea.KeyPressMsg{Code: 'l', Text: "l"}
	prevStageKey = tea.KeyPressMsg{Code: 'h', Text: "h"}
	stepLogKey   = tea.KeyPressMsg{Code: 'l', Mod: tea.ModCtrl}
)

func pipelineStage(id, name, stepID string) gojenkins.PipelineNode {
	return gojenkins.PipelineNode{
		ID: id, Name: name, Status: "SUCCESS",
		StageFlowNodes: []gojenkins.PipelineNode{{ID: stepID, Name: name + " step"}},
	}
}

// newStageSwitchPage returns a loaded build page with three stages whose
// details pane shows the first one, Checkout.
func newStageSwitchPage(t *testing.T) *Model {
	t.Helper()
	cfg := config.GetDefaultConfig()
	keyMap := UserKeyMap(cfg.KeyMapConfig.Build)
	stageView := viewport.New()
	m := &Model{
		ctx:       &appcontext.ProgramContext{Config: cfg},
		build:     &gojenkins.Build{Raw: &gojenkins.BuildResponse{}},
		keyMap:    keyMap,
		stageView: &stageView,
		pipelineRun: gojenkins.PipelineRun{
			ID: "558",
			Job: &gojenkins.Job{
				Base: "/job/mobile-app",
				Raw:  &gojenkins.JobResponse{URL: "http://jenkins/job/mobile-app/"},
			},
			Stages: []gojenkins.PipelineNode{
				pipelineStage("6", "Checkout", "checkout-step"),
				pipelineStage("12", "Dependencies", "dependencies-step"),
				pipelineStage("18", "Lint", "lint-step"),
			},
		},
		stageDetails: NewStageDetails(160, 45, keyMap),
	}
	t.Cleanup(m.cancelNodeFetch)
	m.handleNodeDownloaded(NodeDownloadedMsg{model: m, Generation: m.nodeGeneration, PipelineNode: m.pipelineRun.Stages[0]})
	return m
}

// press sends a key through the page's public Update and returns the command.
// The command is not run: a stage switch's command fetches from Jenkins.
func press(m *Model, key tea.KeyPressMsg) tea.Cmd {
	_, cmd := m.Update(key)
	return cmd
}

// messages runs cmd and every command it batches, returning what they emit.
func messages(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, messages(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

func stepLogRequests(msgs []tea.Msg) []string {
	var ids []string
	for _, msg := range msgs {
		if req, ok := msg.(FetchNodeLogMsg); ok {
			ids = append(ids, req.NodeID)
		}
	}
	return ids
}

func TestStepLogDuringStageSwitchDoesNotOpenPreviousStage(t *testing.T) {
	m := newStageSwitchPage(t)

	press(m, nextStageKey)
	got := stepLogRequests(messages(press(m, stepLogKey)))

	if len(got) != 0 {
		t.Fatalf("step log requested %v while Dependencies was still loading; want no request (Checkout's step is stale)", got)
	}
}

// deliverDetails hands the page the details its latest stage fetch would
// return, as the Jenkins round trip would.
func deliverDetails(m *Model, stage gojenkins.PipelineNode) {
	m.Update(NodeDownloadedMsg{model: m, Generation: m.nodeGeneration, PipelineNode: stage})
}

func TestStepLogAfterStageSwitchOpensHighlightedStage(t *testing.T) {
	tests := []struct {
		name   string
		key    tea.KeyPressMsg
		stage  int
		stepID string
	}{
		{"next stage", nextStageKey, 1, "dependencies-step"},
		{"previous stage wraps to last", prevStageKey, 2, "lint-step"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := newStageSwitchPage(t)

			press(m, tt.key)
			deliverDetails(m, m.pipelineRun.Stages[tt.stage])
			got := stepLogRequests(messages(press(m, stepLogKey)))

			if len(got) != 1 || got[0] != tt.stepID {
				t.Fatalf("step log requested %v, want [%s]", got, tt.stepID)
			}
		})
	}
}

func TestStepLogDuringStageSwitchSaysStageIsLoading(t *testing.T) {
	m := newStageSwitchPage(t)

	press(m, nextStageKey)
	msgs := messages(press(m, stepLogKey))

	for _, msg := range msgs {
		if noti, ok := msg.(util.FooterNotiMsg); ok && strings.Contains(noti.Message, "Dependencies") {
			return
		}
	}
	t.Fatalf("step log during the switch emitted %#v; want a footer notice that Dependencies is still loading", msgs)
}

func TestLateDetailsForSkippedStageDoNotOpenItsStepLog(t *testing.T) {
	m := newStageSwitchPage(t)

	press(m, nextStageKey)
	skipped := m.nodeGeneration
	press(m, nextStageKey) // on to Lint before Dependencies' details arrive
	m.Update(NodeDownloadedMsg{model: m, Generation: skipped, PipelineNode: m.pipelineRun.Stages[1]})
	got := stepLogRequests(messages(press(m, stepLogKey)))

	if len(got) != 0 {
		t.Fatalf("step log requested %v after late Dependencies details; want none until Lint's details arrive", got)
	}
	deliverDetails(m, m.pipelineRun.Stages[2])
	if got := stepLogRequests(messages(press(m, stepLogKey))); len(got) != 1 || got[0] != "lint-step" {
		t.Fatalf("step log requested %v, want [lint-step]", got)
	}
}

func TestStepLogRetriesStageWhoseDetailsFailedToLoad(t *testing.T) {
	m := newStageSwitchPage(t)
	jenkins := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/job/mobile-app/558/execution/node/12/wfapi/describe/api/json" {
			http.NotFound(w, r)
			return
		}
		// The wfapi describe shape for a stage.
		_, _ = w.Write([]byte(`{"id":"12","name":"Dependencies","status":"SUCCESS",` +
			`"stageFlowNodes":[{"id":"dependencies-step","name":"Dependencies step","status":"SUCCESS"}]}`))
	}))
	defer jenkins.Close()
	m.pipelineRun.Base = "/job/mobile-app/558"
	m.pipelineRun.Job.Jenkins = gojenkins.CreateJenkins(nil, jenkins.URL)

	press(m, nextStageKey)
	m.Update(NodeDownloadedMsg{model: m, Generation: m.nodeGeneration, Error: errors.New("request timed out")})
	for _, msg := range messages(press(m, stepLogKey)) {
		if _, ok := msg.(NodeDownloadedMsg); ok {
			m.Update(msg)
		}
	}
	got := stepLogRequests(messages(press(m, stepLogKey)))

	if len(got) != 1 || got[0] != "dependencies-step" {
		t.Fatalf("after a failed details load, step log requested %v; want the key to retry the load and then open [dependencies-step]", got)
	}
}
