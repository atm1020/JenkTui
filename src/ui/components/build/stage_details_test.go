package build

import (
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

func TestStageDetailsViewSetsViewportBorderWidthToFullComponentWidth(t *testing.T) {
	const width = 80

	details := NewStageDetails(width, 40, &KeyMap{})
	details.pipelineNode = &gojenkins.PipelineNode{
		ID:     "1",
		Name:   "Build",
		Status: string(buildstatus.StatusSuccess),
	}

	details.View()

	if got := details.viewport.Style.GetWidth(); got != width {
		t.Fatalf("viewport style width = %d, want %d", got, width)
	}
}

func TestStageDetailsHeaderRendersNode(t *testing.T) {
	details := NewStageDetails(80, 40, &KeyMap{})
	details.pipelineNode = &gojenkins.PipelineNode{Name: "Build", ExecNode: "linux-1"}
	if got := ansi.Strip(details.renderNodeDetailsHeader()); !strings.Contains(got, "Node: linux-1") {
		t.Fatalf("stage details header = %q, want node name", got)
	}

	details.pipelineNode.ExecNode = ""
	if got := ansi.Strip(details.renderNodeDetailsHeader()); !strings.Contains(got, "Node: built-in") {
		t.Fatalf("stage details header = %q, want built-in node name", got)
	}
}

func TestStageDetailsRendersPausedInputAsWaitingForApproval(t *testing.T) {
	details := NewStageDetails(80, 40, &KeyMap{})

	got := details.renderFlowNode(1, gojenkins.PipelineNode{
		ID:     "10",
		Name:   "Deploy",
		Status: "PAUSED_PENDING_INPUT",
	}, false, details.spinnerModel.View())

	theme := styles.GetTheme()
	orangeApprovalText := theme.S().Base.Foreground(theme.Warning).Render("Waiting for approval")
	if !strings.Contains(got, orangeApprovalText) {
		t.Fatalf("paused input flow node does not render approval status in warning color: %q", got)
	}

	details.pipelineNode = &gojenkins.PipelineNode{
		ID:     "10",
		Name:   "Deploy",
		Status: "PAUSED_PENDING_INPUT",
	}
	if got := details.renderNodeDetailsHeader(); !strings.Contains(got, orangeApprovalText) {
		t.Fatalf("paused stage header does not render approval status in warning color: %q", got)
	}
}

func TestStageDetailsOpenStepLogAfterRefreshClampsStaleSelection(t *testing.T) {
	details := NewStageDetails(80, 40, &KeyMap{
		OpenStepLog: key.NewBinding(key.WithKeys("enter")),
	})

	details.pipelineNode = &gojenkins.PipelineNode{
		StageFlowNodes: []gojenkins.PipelineNode{
			{ID: "old-first"},
			{ID: "old-second"},
		},
	}
	details.selectedFlowNodeIdx = 1

	_, _ = details.Update(NodeDownloadedMsg{
		PipelineNode: gojenkins.PipelineNode{
			StageFlowNodes: []gojenkins.PipelineNode{{ID: "new-only"}},
		},
	})

	cmd := details.handleKeyPressMsg(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("handleKeyPressMsg returned nil command")
	}

	msg := cmd()
	fetchMsg, ok := msg.(FetchNodeLogMsg)
	if !ok {
		t.Fatalf("command returned %T, want FetchNodeLogMsg", msg)
	}
	if fetchMsg.NodeID != "new-only" {
		t.Fatalf("FetchNodeLogMsg.NodeID = %q, want %q", fetchMsg.NodeID, "new-only")
	}
}
