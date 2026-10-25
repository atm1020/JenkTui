package build

import (
	"fmt"
	"image/color"
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
	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
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
	wHeight = getStageViewHeight(wHeight)
	vp := viewport.New()

	t := styles.GetTheme()
	return &StageDetails{
		wWidth:              wWidth,
		wHeight:             wHeight,
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
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return s, s.handleWindowSizeMsg(msg)
	case tea.KeyPressMsg:
		return s, s.handleKeyPressMsg(msg)
	case NodeDownloadedMsg:
		return s, s.handleNodeDownloaded(msg)
	}

	// Let viewport handle scrolling
	viewport, cmd := s.viewport.Update(msg)
	s.viewport = &viewport
	return s, cmd
}

func (s *StageDetails) View() string {
	return s.render(s.spinnerModel.View())
}

// render draws the pane straight through lipgloss with spinnerView as the
// running-step icon. The page's section cache falls back to it and is tested
// against it.
func (s *StageDetails) render(spinnerView string) string {
	if s.pipelineNode == nil {
		return lipgloss.Place(
			s.wWidth,
			s.wHeight,
			lipgloss.Center,
			lipgloss.Center,
			"",
		)
	}

	var content string
	if s.pipelineNode != nil {
		content = s.renderNodeDetails(spinnerView)
	}
	s.viewport.SetContent(content)

	t := styles.GetTheme()

	// Change border color based on focus state
	borderStyle := t.S().SelectedBorder.
		BorderTop(false).
		BorderBottom(true).
		BorderLeft(false).
		BorderRight(false).
		Width(s.wWidth)

	if s.isFocused {
		// Active view - use focused border color
		borderStyle = borderStyle.BorderForeground(t.StageFocusedBorder)
	}

	s.viewport.Style = borderStyle

	return lipgloss.Place(
		s.wWidth,
		s.wHeight,
		lipgloss.Left,
		lipgloss.Top,
		s.viewport.View(),
	)
}

// advanceAnimation moves the running-step spinner one frame. The page's
// clock calls it; StageDetails never schedules its own timer.
func (s *StageDetails) advanceAnimation() {
	s.spinnerModel = animation.AdvanceSpinner(s.spinnerModel)
}

func (s *StageDetails) handleNodeDownloaded(msg NodeDownloadedMsg) tea.Cmd {
	s.pipelineNode = &msg.PipelineNode
	if len(s.pipelineNode.StageFlowNodes) == 0 {
		s.selectedFlowNodeIdx = 0
	} else if s.selectedFlowNodeIdx >= len(s.pipelineNode.StageFlowNodes) {
		s.selectedFlowNodeIdx = len(s.pipelineNode.StageFlowNodes) - 1
	} else if s.selectedFlowNodeIdx < 0 {
		s.selectedFlowNodeIdx = 0
	}
	return nil
}

func (s *StageDetails) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	if s.pipelineNode == nil {
		return nil
	}

	if len(s.pipelineNode.StageFlowNodes) == 0 {
		return nil
	}

	switch {
	case key.Matches(msg, s.keymap.Down):
		// Move to next flow node
		s.selectedFlowNodeIdx++
		if s.selectedFlowNodeIdx >= len(s.pipelineNode.StageFlowNodes) {
			s.selectedFlowNodeIdx = len(s.pipelineNode.StageFlowNodes) - 1
		}
		s.viewport.ScrollDown(4)
		return nil
	case key.Matches(msg, s.keymap.Up):
		// Move to previous flow node
		s.selectedFlowNodeIdx--
		if s.selectedFlowNodeIdx < 0 {
			s.selectedFlowNodeIdx = 0
		}
		s.viewport.ScrollUp(4)
		return nil
	case key.Matches(msg, s.keymap.OpenStepLog):
		selected := s.pipelineNode.StageFlowNodes[s.selectedFlowNodeIdx]
		return util.CmdHandler(FetchNodeLogMsg{
			NodeID: selected.ID,
		})
	}

	return nil
}

func (s StageDetails) renderNodeDetailsHeader() string {
	node := s.pipelineNode
	t := styles.GetTheme()

	var sections []string

	// Header with node name and status icon
	status := buildstatus.PipelineStatus(s.pipelineNode.Status)
	statusIcon := s.getStatusIcon(status)
	sections = append(sections,
		t.S().Title.Render(fmt.Sprintf("%s %s", node.Name, statusIcon)))

	// Basic Info
	sections = append(sections,
		t.S().Base.Bold(true).Render("Stage ID: ")+s.pipelineNode.ID)
	statusText := status.Label()
	if status.Activity(0) == buildstatus.ActivityPaused {
		statusText = t.S().Base.Foreground(t.Warning).Render(statusText)
	}
	sections = append(sections,
		t.S().Base.Bold(true).Render("Status: ")+statusText)
	sections = append(sections,
		t.S().Base.Bold(true).Render("Started: ")+util.FormatStartTime(s.pipelineNode.StartTime))
	sections = append(sections,
		t.S().Base.Bold(true).Render("Duration: ")+util.FormatDuration(s.pipelineNode.Duration))
	nodeWidth := max(0, s.wWidth-lipgloss.Width("Node: "))
	sections = append(sections,
		t.S().Base.Bold(true).Render("Node: ")+middleTruncate((NodeUsage{Name: s.pipelineNode.ExecNode}).DisplayName(), nodeWidth))

	// Parent Nodes
	if len(node.ParentNodes) > 0 {
		sections = append(sections,
			t.S().Base.Bold(true).Render("Parent Nodes:"))
		for _, parentID := range node.ParentNodes {
			sections = append(sections,
				t.S().Base.Render(fmt.Sprintf("  • %s", parentID)))
		}
	}
	return lipgloss.JoinVertical(lipgloss.Left, sections...)
}

func (s StageDetails) renderNodeDetails(spinnerView string) string {
	header := s.renderNodeDetailsHeader()
	t := styles.GetTheme()
	var content string
	// Stage Flow Nodes
	if len(s.pipelineNode.StageFlowNodes) > 0 {
		var builder strings.Builder
		for i, flowNode := range s.pipelineNode.StageFlowNodes {
			isSelected := i == s.selectedFlowNodeIdx
			flowNodeContent := s.renderFlowNode(i+1, flowNode, isSelected, spinnerView)
			builder.WriteString(flowNodeContent)
			builder.WriteString("\n")
		}
		content = builder.String()
	} else {
		content = lipgloss.Place(
			s.wWidth,
			s.wHeight-lipgloss.Height(header),
			lipgloss.Center,
			lipgloss.Center,
			t.S().Base.Foreground(t.Grey).Italic(true).Render("Pipeline Steps not available"),
		)
	}

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		content,
	)
}

func (s StageDetails) renderFlowNode(index int, flowNode gojenkins.PipelineNode, isSelected bool, spinnerView string) string {
	t := styles.GetTheme()

	// Create a box style for each flow node
	flowNodeStyle := t.S().Base.
		Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Grey).
		PaddingLeft(1).
		PaddingRight(1).
		Width(s.wWidth - 8)

	status := buildstatus.PipelineStatus(flowNode.Status)
	isInProgress := status.IsExecuting()
	isWaitingForApproval := status.Activity(0) == buildstatus.ActivityPaused

	var flowIcon string
	if isInProgress {
		flowIcon = spinnerView
		flowNodeStyle = flowNodeStyle.BorderForeground(t.Warning)
	} else {
		flowIcon = s.getStatusIcon(status)
		if isWaitingForApproval {
			flowNodeStyle = flowNodeStyle.BorderForeground(t.Warning)
		}
	}

	// Live elapsed time for in-progress steps; static duration otherwise
	// var durationStr string
	// if isInProgress && flowNode.StartTime > 0 {
	// 	elapsed := time.Now().UnixMilli() - flowNode.StartTime
	// 	durationStr = util.FormatDuration(elapsed)
	// } else {
	durationStr := util.FormatDuration(flowNode.Duration)
	// }

	// Get status color based on semantic state.
	var statusColor color.Color
	switch {
	case status.Outcome() == buildstatus.OutcomeSuccess:
		statusColor = t.Success
	case status.Outcome() == buildstatus.OutcomeFailure:
		statusColor = t.Error
		flowNodeStyle = flowNodeStyle.BorderForeground(t.Error)
	case status.IsActive(0):
		statusColor = t.Warning
	default:
		statusColor = t.Grey
	}

	// Header: index, name, icon, duration
	nameStyle := t.S().Base.Bold(true)
	if isSelected {
		nameStyle = nameStyle.Foreground(t.Primary)
	}

	header := nameStyle.Render(fmt.Sprintf("%d. %s", index, flowNode.Name)) +
		" " + flowIcon +
		" " + t.S().Base.Foreground(statusColor).Render(status.Label()) +
		" " + t.S().Base.Foreground(t.Grey).Render(fmt.Sprintf("(%s)", durationStr))

	// Details section
	var details []string
	details = append(details, header)
	details = append(details,
		t.S().Base.Foreground(t.Grey).Render(fmt.Sprintf("ID: %s", flowNode.ID)))

	// Display log status hint below the box if selected
	content := lipgloss.JoinVertical(lipgloss.Left, details...)

	// Highlight selected node (override the in-progress or failed color)
	if isSelected {
		flowNodeStyle = flowNodeStyle.BorderForeground(t.Primary)
	}
	renderedBox := flowNodeStyle.Render(content)

	return renderedBox
}

func (s StageDetails) getStatusIcon(status buildstatus.PipelineStatus) string {
	t := styles.GetTheme()

	if status.IsActive(0) {
		return t.S().StyledIcons.Running
	}
	switch status.Outcome() {
	case buildstatus.OutcomeSuccess:
		return t.S().StyledIcons.Success
	case buildstatus.OutcomeFailure:
		return t.S().StyledIcons.Failure
	case buildstatus.OutcomeAborted:
		return t.S().StyledIcons.Pending
	default:
		return t.S().StyledIcons.NotBuilt
	}
}

func getStageViewHeight(height int) int {
	return int(float64(height) * 0.6)
}

// autoSelectInProgressFlowNode finds the first in-progress flow node,
// selects it, and scrolls the viewport to make it visible.
func (s *StageDetails) autoSelectInProgressFlowNode() {
	if s.pipelineNode == nil {
		return
	}
	for i, flowNode := range s.pipelineNode.StageFlowNodes {
		status := buildstatus.PipelineStatus(flowNode.Status)
		if status.IsExecuting() || status.Activity(0) == buildstatus.ActivityPaused {
			log.Error("Selected Status", "status", status)
			s.selectedFlowNodeIdx = i
			// renderNodeDetailsHeader renders 6 fixed lines (title, ID, status, started, duration, node).
			// Each flow node box renders as 4 lines (2 border + 2 content) plus 1 newline separator.
			const headerHeight = 6
			const nodeHeight = 5
			// s.viewport.EnsureVisible(headerHeight+i*nodeHeight, 1, 1)
			// s.viewport.ScrollDown(i * 4)
			s.viewport.PageDown()
			return
		}
	}
}

func (s *StageDetails) init() {
	s.selectedFlowNodeIdx = 0
	s.viewport.SetYOffset(0)
}

func (s *StageDetails) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	s.wWidth = msg.Width
	s.wHeight = msg.Height // getStageViewHeight(msg.Height)
	s.viewport.SetWidth(s.wWidth)
	s.viewport.SetHeight(s.wHeight)
	return nil
}
