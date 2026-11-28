package build

import (
	"fmt"
	"strings"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

const (
	stepDurationColWidth = 7
	stepHeaderLines      = 2 // metadata line + divider
)

type StageDetails struct {
	wWidth, wHeight     int
	pipelineNode        *gojenkins.PipelineNode
	viewport            *viewport.Model
	selectedFlowNodeIdx int
	isFocused           bool
	keymap              *KeyMap
	spinnerModel        spinner.Model
}

func NewStageDetails(wWidth, wHeight int, keymap *KeyMap) *StageDetails {
	vp := viewport.New()
	innerW, innerH := max(1, wWidth-2), max(1, wHeight-2)
	vp.SetWidth(innerW)
	vp.SetHeight(innerH)

	t := styles.GetTheme()
	return &StageDetails{
		wWidth:              max(4, wWidth),
		wHeight:             max(4, wHeight),
		viewport:            &vp,
		selectedFlowNodeIdx: 0,
		isFocused:           false,
		keymap:              keymap,
		spinnerModel: spinner.New(
			spinner.WithSpinner(spinner.Jump),
			spinner.WithStyle(t.S().Spinner),
		),
	}
}

func (s StageDetails) Init() tea.Cmd {
	return nil
}

func (s *StageDetails) Update(msg tea.Msg) (util.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return s, s.handleWindowSizeMsg(msg)
	case tea.KeyPressMsg:
		return s, s.handleKeyPressMsg(msg)
	case NodeDownloadedMsg:
		return s, s.handleNodeDownloaded(msg)
	}

	vp, cmd := s.viewport.Update(msg)
	s.viewport = &vp
	return s, cmd
}

func (s *StageDetails) View() string {
	return s.render(s.spinnerModel.View())
}

// render draws the compact Omaci-inspired framed steps panel straight through
// lipgloss with spinnerView as the running-step icon. The page's section cache
// falls back to it and is tested against it.
func (s *StageDetails) render(spinnerView string) string {
	opts := s.frameOpts()
	if s.pipelineNode == nil {
		iw, ih := s.innerSize()
		t := styles.GetTheme()
		empty := t.S().Base.Foreground(t.Grey).Italic(true).Render("No stage selected")
		return renderPanelFrame(opts, lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Center, empty))
	}

	content := s.renderNodeDetails(spinnerView)
	iw, ih := s.innerSize()
	s.viewport.SetWidth(iw)
	s.viewport.SetHeight(ih)
	s.viewport.Style = lipgloss.NewStyle()
	y := s.viewport.YOffset()
	s.viewport.SetContent(content)
	s.viewport.SetYOffset(y)
	return renderPanelFrame(opts, s.viewport.View())
}

func (s *StageDetails) innerSize() (w, h int) {
	return max(1, s.wWidth-2), max(1, s.wHeight-2)
}

func (s *StageDetails) frameOpts() panelFrameOpts {
	var note, right string
	if s.pipelineNode != nil {
		note = s.pipelineNode.Name
		right = styles.GetTheme().S().Base.Foreground(styles.GetTheme().SubtleText).
			Render(fmt.Sprintf("%d", len(s.pipelineNode.StageFlowNodes)))
	}
	return panelFrameOpts{
		Title:  "steps",
		Note:   note,
		Right:  right,
		Width:  s.wWidth,
		Height: s.wHeight,
		Active: s.isFocused,
	}
}

// advanceAnimation moves the running-step spinner one frame. The page's
// clock calls it; StageDetails never schedules its own timer.
func (s *StageDetails) advanceAnimation() {
	s.spinnerModel = animation.AdvanceSpinner(s.spinnerModel)
}

func (s *StageDetails) handleNodeDownloaded(msg NodeDownloadedMsg) tea.Cmd {
	prevID := ""
	if s.pipelineNode != nil {
		prevID = s.pipelineNode.ID
	}
	node := msg.PipelineNode
	s.pipelineNode = &node

	steps := s.pipelineNode.StageFlowNodes
	switch {
	case len(steps) == 0:
		s.selectedFlowNodeIdx = 0
		s.viewport.SetYOffset(0)
	case prevID != node.ID:
		// First load of a stage: Omaci-style smart focus lands on the first
		// failed step, else the first active/paused step, else step 0.
		s.selectedFlowNodeIdx = initialStepFocusIndex(steps)
		s.ensureSelectedStepVisible()
	case s.selectedFlowNodeIdx >= len(steps):
		s.selectedFlowNodeIdx = len(steps) - 1
		s.ensureSelectedStepVisible()
	case s.selectedFlowNodeIdx < 0:
		s.selectedFlowNodeIdx = 0
		s.ensureSelectedStepVisible()
	}
	return nil
}

func initialStepFocusIndex(steps []gojenkins.PipelineNode) int {
	for i, step := range steps {
		if buildstatus.PipelineStatus(step.Status).Outcome() == buildstatus.OutcomeFailure {
			return i
		}
	}
	for i, step := range steps {
		status := buildstatus.PipelineStatus(step.Status)
		if status.IsExecuting() || status.Activity(0) == buildstatus.ActivityPaused {
			return i
		}
	}
	return 0
}

func (s *StageDetails) selectedStep() (gojenkins.PipelineNode, bool) {
	if s.pipelineNode == nil || len(s.pipelineNode.StageFlowNodes) == 0 {
		return gojenkins.PipelineNode{}, false
	}
	idx := clampInt(s.selectedFlowNodeIdx, 0, len(s.pipelineNode.StageFlowNodes)-1)
	return s.pipelineNode.StageFlowNodes[idx], true
}

func (s *StageDetails) stageName() string {
	if s.pipelineNode == nil {
		return ""
	}
	return s.pipelineNode.Name
}

func (s *StageDetails) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	if s.pipelineNode == nil || len(s.pipelineNode.StageFlowNodes) == 0 {
		return nil
	}

	switch {
	case key.Matches(msg, s.keymap.Down):
		if s.selectedFlowNodeIdx < len(s.pipelineNode.StageFlowNodes)-1 {
			s.selectedFlowNodeIdx++
		}
		s.ensureSelectedStepVisible()
		return nil
	case key.Matches(msg, s.keymap.Up):
		if s.selectedFlowNodeIdx > 0 {
			s.selectedFlowNodeIdx--
		}
		s.ensureSelectedStepVisible()
		return nil
	case key.Matches(msg, s.keymap.OpenStepLog):
		selected := s.pipelineNode.StageFlowNodes[s.selectedFlowNodeIdx]
		return util.CmdHandler(FetchNodeLogMsg{
			NodeID: selected.ID,
		})
	}

	return nil
}

func (s *StageDetails) ensureSelectedStepVisible() {
	if s.viewport == nil || s.pipelineNode == nil || len(s.pipelineNode.StageFlowNodes) == 0 {
		return
	}
	_, innerH := s.innerSize()
	if innerH <= 0 {
		return
	}
	line := stepHeaderLines + s.selectedFlowNodeIdx
	y := s.viewport.YOffset()
	if line < y {
		if s.selectedFlowNodeIdx == 0 {
			s.viewport.SetYOffset(0)
		} else {
			s.viewport.SetYOffset(max(0, line))
		}
	} else if line >= y+innerH {
		s.viewport.SetYOffset(line - innerH + 1)
	}
}

func (s StageDetails) renderNodeDetailsHeader() string {
	node := s.pipelineNode
	t := styles.GetTheme()
	innerW, _ := s.innerSize()

	status := buildstatus.PipelineStatus(node.Status)
	nodeLabel := (NodeUsage{Name: node.ExecNode}).DisplayName()
	meta := fmt.Sprintf(" Stage ID: %s · Node: %s", node.ID, nodeLabel)
	metaLine := t.S().Base.Foreground(t.SubtleText).Render(ansi.Truncate(meta, innerW, "…"))

	var secondLine string
	if status.Activity(0) == buildstatus.ActivityPaused {
		approval := t.S().Base.Foreground(t.Warning).Render("Waiting for approval")
		secondLine = " " + approval
	} else {
		secondLine = t.S().Base.Foreground(t.FaintBorder).Render(strings.Repeat("─", innerW))
	}

	return metaLine + "\n" + secondLine
}

func (s StageDetails) renderNodeDetails(spinnerView string) string {
	header := s.renderNodeDetailsHeader()
	t := styles.GetTheme()
	innerW, innerH := s.innerSize()

	if len(s.pipelineNode.StageFlowNodes) == 0 {
		missing := lipgloss.Place(
			innerW,
			max(1, innerH-stepHeaderLines),
			lipgloss.Center,
			lipgloss.Center,
			t.S().Base.Foreground(t.Grey).Italic(true).Render("Pipeline Steps not available"),
		)
		return header + "\n" + missing
	}

	lines := make([]string, 0, len(s.pipelineNode.StageFlowNodes)+1)
	lines = append(lines, header)
	for i, flowNode := range s.pipelineNode.StageFlowNodes {
		lines = append(lines, s.renderFlowNode(i+1, flowNode, i == s.selectedFlowNodeIdx, spinnerView))
	}
	return strings.Join(lines, "\n")
}

// renderFlowNode draws one compact Omaci-style single-line step row:
// `▸ <icon> <step name>          <duration>` with exact innerW cell width.
func (s StageDetails) renderFlowNode(_ int, flowNode gojenkins.PipelineNode, isSelected bool, spinnerView string) string {
	t := styles.GetTheme()
	innerW, _ := s.innerSize()

	rowBase := t.S().Base
	if isSelected {
		rowBase = rowBase.Background(t.SelectedBackground)
	}

	pointer := rowBase.Render("  ")
	if isSelected {
		pointer = rowBase.Foreground(t.StageFocusedBorder).Bold(true).Render("▸ ")
	}

	status := buildstatus.PipelineStatus(flowNode.Status)
	isInProgress := status.IsExecuting()
	isPaused := status.Activity(0) == buildstatus.ActivityPaused

	var icon string
	switch {
	case isInProgress:
		icon = rowBase.Foreground(t.Warning).Render(spinnerView)
	case isPaused:
		icon = rowBase.Foreground(t.Warning).Render(styles.WaitingIcon)
	default:
		icon = s.stepStatusIcon(status, isSelected)
	}

	var rightText string
	var rightStyled string
	if isPaused {
		rightText = "Waiting for approval"
		rightStyled = rowBase.Foreground(t.Warning).Render(rightText)
	} else {
		if status.Outcome() == buildstatus.OutcomeNotBuilt {
			rightText = "-"
		} else {
			rightText = util.FormatDuration(flowNode.Duration)
		}
		durStyle := rowBase.Foreground(t.SubtleText)
		if isSelected {
			durStyle = rowBase.Foreground(t.FgBase).Bold(true)
		}
		rightStyled = durStyle.Render(rightText)
	}

	iconW := ansi.StringWidth(icon)
	rightW := ansi.StringWidth(rightText)
	// Layout: pointer(2) + icon + " "(1) + name + gap(>=1) + right + " "(1)
	availNameW := innerW - 2 - iconW - 1 - 1 - rightW - 1
	if availNameW < 4 {
		// Drop right-aligned duration when ultra-narrow so the step name remains legible.
		rightStyled = ""
		rightW = 0
		availNameW = max(1, innerW-2-iconW-1-1)
	}

	name := ansi.Truncate(flowNode.Name, availNameW, "…")
	nameStyle := rowBase
	switch {
	case isSelected:
		nameStyle = nameStyle.Foreground(t.FgBase).Bold(true)
	case status.Outcome() == buildstatus.OutcomeFailure:
		nameStyle = nameStyle.Foreground(t.Error)
	case status.Outcome() == buildstatus.OutcomeNotBuilt:
		nameStyle = nameStyle.Foreground(t.SubtleText)
	}

	used := 2 + iconW + 1 + ansi.StringWidth(name) + rightW
	if rightW > 0 {
		used++ // trailing space
	}
	gap := max(0, innerW-used)

	var b strings.Builder
	b.WriteString(pointer)
	b.WriteString(icon)
	b.WriteString(rowBase.Render(" "))
	b.WriteString(nameStyle.Render(name))
	if gap > 0 {
		b.WriteString(rowBase.Render(strings.Repeat(" ", gap)))
	}
	if rightW > 0 {
		b.WriteString(rightStyled)
		b.WriteString(rowBase.Render(" "))
	}
	return b.String()
}

func (s StageDetails) stepStatusIcon(status buildstatus.PipelineStatus, isSelected bool) string {
	t := styles.GetTheme()
	base := t.S().Base
	if isSelected {
		base = base.Background(t.SelectedBackground)
	}
	if status.IsActive(0) {
		return base.Foreground(t.Warning).Render(styles.WaitingIcon)
	}
	switch status.Outcome() {
	case buildstatus.OutcomeSuccess:
		return base.Foreground(t.Success).Render(styles.SuccessIcon)
	case buildstatus.OutcomeFailure:
		return base.Foreground(t.Error).Render(styles.FailureIcon)
	case buildstatus.OutcomeAborted:
		return base.Foreground(t.Grey).Render(styles.NotBuildedIcon)
	default:
		return base.Foreground(t.Grey).Render(styles.NotBuildedIcon)
	}
}

func (s *StageDetails) init() {
	s.selectedFlowNodeIdx = 0
	s.viewport.SetYOffset(0)
}

func (s *StageDetails) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	s.wWidth = max(4, msg.Width)
	s.wHeight = max(4, msg.Height)
	iw, ih := s.innerSize()
	s.viewport.SetWidth(iw)
	s.viewport.SetHeight(ih)
	s.ensureSelectedStepVisible()
	return nil
}
