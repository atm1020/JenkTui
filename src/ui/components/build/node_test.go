package build

import (
	"reflect"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/nodeselector"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
)

func TestCollectNodes(t *testing.T) {
	tests := []struct {
		name   string
		stages []gojenkins.PipelineNode
		want   []NodeUsage
	}{
		{name: "no stages"},
		{
			name: "built-in node",
			stages: []gojenkins.PipelineNode{
				{Name: "Build", ExecNode: ""},
				{Name: "Test", ExecNode: ""},
			},
			want: []NodeUsage{{Name: "", Stages: []string{"Build", "Test"}}},
		},
		{
			name: "one named node",
			stages: []gojenkins.PipelineNode{
				{Name: "Build", ExecNode: "linux-1"},
				{Name: "Test", ExecNode: "linux-1"},
			},
			want: []NodeUsage{{Name: "linux-1", Stages: []string{"Build", "Test"}}},
		},
		{
			name: "first appearance order",
			stages: []gojenkins.PipelineNode{
				{Name: "Checkout", ExecNode: "linux-2"},
				{Name: "Build", ExecNode: "linux-1"},
				{Name: "Unit", ExecNode: "linux-2"},
				{Name: "Package", ExecNode: "k8s-pod"},
				{Name: "Integration", ExecNode: "linux-1"},
				{Name: "Deploy", ExecNode: "k8s-pod"},
			},
			want: []NodeUsage{
				{Name: "linux-2", Stages: []string{"Checkout", "Unit"}},
				{Name: "linux-1", Stages: []string{"Build", "Integration"}},
				{Name: "k8s-pod", Stages: []string{"Package", "Deploy"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := collectNodes(tt.stages); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("collectNodes() = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestMiddleTruncate(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		maxWidth int
		want     string
	}{
		{name: "fits", input: "linux-1", maxWidth: 7, want: "linux-1"},
		{name: "keeps head and tail", input: "jenkins-jnlp-agent-lz8kq", maxWidth: 19, want: "jenkins-j…ent-lz8kq"},
		{name: "ellipsis width", input: "long", maxWidth: 1, want: "…"},
		{name: "below ellipsis width", input: "long", maxWidth: 0, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := middleTruncate(tt.input, tt.maxWidth); got != tt.want {
				t.Fatalf("middleTruncate(%q, %d) = %q, want %q", tt.input, tt.maxWidth, got, tt.want)
			}
		})
	}

	got := middleTruncate("節點-alpha-終端", 10)
	if width := lipgloss.Width(got); width > 10 {
		t.Fatalf("middleTruncate multibyte width = %d, want <= 10: %q", width, got)
	}
	if !strings.Contains(got, "…") || !strings.HasSuffix(got, "終端") {
		t.Fatalf("middleTruncate multibyte result = %q, want ellipsis and preserved tail", got)
	}
}

func TestDownloadedNodeIsAvailableToCopy(t *testing.T) {
	cfg := config.GetDefaultConfig()
	ctx := &appcontext.ProgramContext{Config: cfg}
	copyKey := key.NewBinding(key.WithKeys("n"))
	stageView := viewport.New()
	stageView.SetWidth(80)
	stageView.SetHeight(10)
	model := &Model{
		ctx:       ctx,
		build:     &gojenkins.Build{Raw: &gojenkins.BuildResponse{}},
		keyMap:    &KeyMap{CopyNode: copyKey},
		stageView: &stageView,
		pipelineRun: gojenkins.PipelineRun{
			Stages: []gojenkins.PipelineNode{{ID: "7", Name: "Build"}},
		},
	}

	// This is the message produced by getNode after Jenkins returns the full
	// stage details. Copy Node should use the executor from that response.
	_, _ = model.Update(NodeDownloadedMsg{model: model, PipelineNode: gojenkins.PipelineNode{
		ID: "7", Name: "Build", ExecNode: "linux-1",
	}})

	cmd := model.handleKeyPressMsgStageView(tea.KeyPressMsg{Code: 'n', Text: "n"})
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("copy node returned %T with %#v, want two-command BatchMsg", cmd(), cmd())
	}
	if got, want := batch[0](), tea.SetClipboard("linux-1")(); !reflect.DeepEqual(got, want) {
		t.Fatalf("clipboard message = %#v, want %#v", got, want)
	}
}

func TestHandleCopyNode(t *testing.T) {
	cfg := config.GetDefaultConfig()
	ctx := &appcontext.ProgramContext{Config: cfg}
	copyKey := key.NewBinding(key.WithKeys("n"))

	t.Run("no nodes warns", func(t *testing.T) {
		model := &Model{ctx: ctx, build: &gojenkins.Build{}, keyMap: &KeyMap{CopyNode: copyKey}}
		cmd := model.handleKeyPressMsgStageView(tea.KeyPressMsg{Code: 'n', Text: "n"})
		if cmd == nil {
			t.Fatal("copy node returned nil command")
		}
		msg, ok := cmd().(util.FooterNotiMsg)
		if !ok {
			t.Fatalf("copy node returned %T, want FooterNotiMsg", cmd())
		}
		if msg.Type != util.NotiWarning || msg.Message != "No node information available for this build" {
			t.Fatalf("notification = %#v", msg)
		}
	})

	t.Run("one node copies raw name", func(t *testing.T) {
		model := &Model{
			ctx: ctx, build: &gojenkins.Build{}, keyMap: &KeyMap{CopyNode: copyKey},
			pipelineRun: gojenkins.PipelineRun{Stages: []gojenkins.PipelineNode{{Name: "Build", ExecNode: "linux-1"}}},
		}
		cmd := model.handleKeyPressMsgStageView(tea.KeyPressMsg{Code: 'n', Text: "n"})
		batch, ok := cmd().(tea.BatchMsg)
		if !ok || len(batch) != 2 {
			t.Fatalf("copy node returned %T with %#v, want two-command BatchMsg", cmd(), cmd())
		}
		if got, want := batch[0](), tea.SetClipboard("linux-1")(); !reflect.DeepEqual(got, want) {
			t.Fatalf("clipboard message = %#v, want %#v", got, want)
		}
		noti, ok := batch[1]().(util.FooterNotiMsg)
		if !ok || noti.Message != "Copied node name" || noti.Type != util.NotiInfo {
			t.Fatalf("notification = %#v", noti)
		}
	})

	t.Run("built-in copies empty raw name", func(t *testing.T) {
		model := &Model{
			ctx: ctx, build: &gojenkins.Build{}, keyMap: &KeyMap{CopyNode: copyKey},
			pipelineRun: gojenkins.PipelineRun{Stages: []gojenkins.PipelineNode{{Name: "Build"}}},
		}
		batch := model.handleKeyPressMsgStageView(tea.KeyPressMsg{Code: 'n', Text: "n"})().(tea.BatchMsg)
		if got, want := batch[0](), tea.SetClipboard("")(); !reflect.DeepEqual(got, want) {
			t.Fatalf("clipboard message = %#v, want empty clipboard message %#v", got, want)
		}
		noti := batch[1]().(util.FooterNotiMsg)
		if noti.Message != "Copied node name (built-in — empty)" {
			t.Fatalf("notification = %#v", noti)
		}
	})

	t.Run("multiple nodes opens selector", func(t *testing.T) {
		model := &Model{
			ctx: ctx, build: &gojenkins.Build{}, keyMap: &KeyMap{CopyNode: copyKey}, wWidth: 120, wHeight: 40,
			pipelineRun: gojenkins.PipelineRun{Stages: []gojenkins.PipelineNode{
				{Name: "Build", ExecNode: "linux-1"},
				{Name: "Test", ExecNode: "linux-2"},
				{Name: "Deploy", ExecNode: "k8s-pod"},
			}},
		}
		cmd := model.handleKeyPressMsgStageView(tea.KeyPressMsg{Code: 'n', Text: "n"})
		msg, ok := cmd().(util.OpenDialogMsg)
		if !ok {
			t.Fatalf("copy node returned %T, want OpenDialogMsg", cmd())
		}
		if _, ok := msg.Dialog.(*nodeselector.Dialog); !ok {
			t.Fatalf("dialog = %T, want *nodeselector.Dialog", msg.Dialog)
		}
	})
}
