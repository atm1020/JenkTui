package job

import (
	"fmt"

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
)

const (
	StageBoxWidth  = 22
	StageBoxHeight = 8
	queuedRunID    = "queued"
)

// StageRenderCache is one run's rendered row. Static rows are reused across
// animation frames. A row is rebuilt only when its selection, status, stage
// data, or animated spinner/pulse content changes.
type StageRenderCache struct {
	Content string

	lines  []string
	widths []int
	ready  bool

	selected     bool
	status       string
	start        int64
	end          int64
	duration     int64
	hasArtifacts bool
	stages       []stageStamp
	showsSpinner bool
	showsPulse   bool
	spinner      string
	pulse        string
}

type StageViewModel struct {
	wWithh, wHeight int
	pipelineRuns    []*gojenkins.PipelineRun
	runIndex        map[string]int
	viewport        *viewport.Model

	selectedIndex int
	keyMap        StageViewKeyMap

	spinnerModel  spinner.Model
	hasInProgress bool

	currentStyle lipgloss.Style

	pulseBar   *animation.PulseBar
	pulseState bool

	seenRuns map[string]struct{}

	baseBuildInfoStyle lipgloss.Style
	stageRenderCache   map[string]*StageRenderCache
	cachedBoxWidth     int
	cachedTheme        string
	lines              []string
	longest            int
	syncedCount        int
	syncedLongest      int
	scrollDirty        bool
	rowRenders         int
	window             windowCache
	plainKnown         bool
	plainStyle         bool
	// renderRevision changes when output changes for a reason other than the
	// spinner or pulse. The job page uses it to reuse this view across frames
	// that only animate the header.
	renderRevision uint64
}

func NewStageViewModel(width, height int, keyMap StageViewKeyMap) *StageViewModel {
	v := viewport.New()
	return &StageViewModel{
		wWithh:   width,
		wHeight:  height,
		viewport: &v,
		keyMap:   keyMap,
		pulseBar: animation.New(animation.Settings{Width: 14, FPS: 10}),
		spinnerModel: spinner.New(
			spinner.WithSpinner(spinner.Dot),
			spinner.WithStyle(styles.GetTheme().S().Spinner),
		),
		baseBuildInfoStyle: lipgloss.NewStyle().
			Width(24).
			Height(StageBoxHeight).
			Border(lipgloss.HiddenBorder()).
			Align(lipgloss.Center).
			AlignVertical(lipgloss.Center),
		stageRenderCache: make(map[string]*StageRenderCache),
	}
}

func (svm *StageViewModel) SetStyle(s lipgloss.Style) {
	svm.currentStyle = s
	svm.viewport.Style = s
	svm.plainKnown = false
	svm.window = windowCache{}
}

// Init does not start tick chains. The page frame scheduler owns header,
// stage, and pulse timing so this view cannot schedule a second chain.
func (svm StageViewModel) Init() tea.Cmd {
	return nil
}

func (svm *StageViewModel) advanceAnimation() {
	if svm == nil {
		return
	}
	svm.spinnerModel = animation.AdvanceSpinner(svm.spinnerModel)
	if svm.pulseBar != nil {
		svm.pulseBar.Step()
	}
}

func (svm *StageViewModel) ClearCache() {
	svm.stageRenderCache = make(map[string]*StageRenderCache)
	svm.window = windowCache{}
	svm.scrollDirty = true
	svm.renderRevision++
}

func (svm *StageViewModel) Update(msg tea.Msg) (*StageViewModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return svm, svm.onWindowSizeChanged(msg)
	case tea.KeyPressMsg:
		return svm, svm.handleKeyPressMsg(msg)
	}
	return svm, nil
}

func (svm *StageViewModel) View(width, height int) string {
	svm.viewport.SetWidth(width)
	svm.viewport.SetHeight(height)
	if len(svm.pipelineRuns) == 0 {
		svm.viewport.SetContent(
			lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center,
				"No pipeline runs found",
			),
		)
		return svm.viewport.View()
	}
	return svm.renderStageRows(width, height)
}

func (svm *StageViewModel) SetPipelineRun(pr []gojenkins.PipelineRun) {
	if len(pr) == 0 {
		return
	}

	// Polling, reordering, and membership changes invalidate only the rows
	// whose visible stamp no longer matches. Unchanged static rows stay cached.
	svm.renderRevision++
	svm.window = windowCache{}

	var selectedID string
	if svm.selectedIndex < len(svm.pipelineRuns) {
		selectedID = svm.pipelineRuns[svm.selectedIndex].ID
	}

	data := make([]*gojenkins.PipelineRun, 0, len(pr)+len(svm.pipelineRuns))
	svm.runIndex = make(map[string]int, len(pr)+len(svm.pipelineRuns))

	seen := make(map[string]struct{}, len(pr))
	for i := range pr {
		data = append(data, &pr[i])
		seen[pr[i].ID] = struct{}{}
	}

	for i := range svm.pipelineRuns {
		prev := svm.pipelineRuns[i]
		if prev == nil {
			continue
		}
		// Remove the  'queued' mock-ed elment from the list
		if prev.ID == queuedRunID {
			continue
		}
		if _, ok := seen[prev.ID]; ok {
			continue
		}
		data = append(data, prev)
	}

	for i := range data {
		svm.runIndex[data[i].ID] = i
	}
	svm.pipelineRuns = data

	// Keep the user's selection on the same run (by ID) when the list reorders.
	if newIdx, ok := svm.runIndex[selectedID]; ok {
		svm.selectedIndex = newIdx
	}
	svm.pruneRunCache()
}

func (svm *StageViewModel) GetCurrentSelectedRun() gojenkins.PipelineRun {
	return *svm.pipelineRuns[svm.selectedIndex]
}

func (svm *StageViewModel) SetBorderStyle(style lipgloss.Style) {
	svm.viewport.Style = style
	svm.plainKnown = false
	svm.window = windowCache{}
}

func (svm StageViewModel) renderBuildInfo(run *gojenkins.PipelineRun, idx int) string {
	if run == nil {
		return svm.baseBuildInfoStyle.Render("Pending...")
	}

	t := styles.GetTheme()
	buildNumStyle := t.S().BuildNumber.
		Width(3 + len(run.ID))

	// Status-based badge colour; selected row overrides to Primary.
	status := buildstatus.PipelineStatus(run.Status).
		NormalizeQueueState(len(run.Stages), run.EndTime)
	switch {
	case status.Outcome() == buildstatus.OutcomeSuccess:
		buildNumStyle = buildNumStyle.Background(t.Success)
	case status.Outcome() == buildstatus.OutcomeFailure:
		buildNumStyle = buildNumStyle.Background(t.Error)
	case status.IsActive(run.EndTime):
		buildNumStyle = buildNumStyle.Background(t.Warning)
	case status.Outcome() == buildstatus.OutcomeAborted:
		buildNumStyle = buildNumStyle.Background(t.Grey)
	}
	// if idx == svm.selectedIndex {
	// 	buildNumStyle = buildNumStyle.Background(t.Primary)
	// }

	dateStyle := t.S().BuildDate
	buildInfoStyle := svm.baseBuildInfoStyle

	if idx == svm.selectedIndex {
		// A single-line thick bar in the accent color marks the selected run. The
		// previous DoubleBorder (║) rendered a double-line glyph that clashed with
		// the single-line rounded stage boxes beside it.
		buildInfoStyle = buildInfoStyle.
			BorderForeground(t.Primary).
			Border(lipgloss.ThickBorder(), false, false, false, true)

		// dateStyle = dateStyle.
		// 	Background(t.Primary)
	}

	stateLabel := ""
	switch status.Activity(run.EndTime) {
	case buildstatus.ActivityQueued:
		stateLabel = lipgloss.NewStyle().Foreground(t.Grey).
			Bold(true).
			Italic(true).
			Render("In queue")
	case buildstatus.ActivityPaused:
		stateLabel = lipgloss.NewStyle().Foreground(t.Warning).
			Bold(true).
			Render(status.Label())
	}

	var attachmentIcon string
	if run.URLs != nil && run.URLs["artifacts"] != nil {
		attachmentIcon = t.S().StyledIcons.Attachment
	}

	buildInfo := buildInfoStyle.Render(lipgloss.JoinVertical(lipgloss.Left,
		buildNumStyle.Render(fmt.Sprintf("#%s", run.ID))+attachmentIcon+svm.runningAnim(run),
		stateLabel,
		dateStyle.Bold(true).Render(" "+util.FormatStartTime(run.StartTime)),
		dateStyle.Bold(true).Render(" "+util.FormatDuration(run.Duration)),
	))

	return buildInfo
}

func (svm StageViewModel) renderRun(run *gojenkins.PipelineRun, idx int, boxWidth int) string {
	t := styles.GetTheme()

	var row []string
	row = append(row, svm.renderBuildInfo(run, idx))

	if run == nil {
		return lipgloss.JoinHorizontal(lipgloss.Top, row...)
	}

	// wfapi reports the stage graph, not the build result. A run that never
	// reaches a stage (e.g. a Jenkinsfile parse error) has no graph to report
	// on, so it shows up here with zero stages regardless of pass/fail — a
	// still-queued run also has zero stages, but is excluded since its stages
	// simply haven't arrived yet.
	status := buildstatus.PipelineStatus(run.Status).
		NormalizeQueueState(len(run.Stages), run.EndTime)
	if len(run.Stages) == 0 && !status.IsActive(run.EndTime) {
		row = append(row, svm.renderNoStageData(boxWidth))
		return lipgloss.JoinHorizontal(lipgloss.Top, row...)
	}
	for _, stage := range run.Stages {
		var cell, cellStatus string
		var cellStyle lipgloss.Style

		duration := util.FormatDuration(stage.Duration)
		stageStatus := buildstatus.PipelineStatus(stage.Status)
		activity, outcome := stageStatus.Activity(0), stageStatus.Outcome()
		switch {
		case activity == buildstatus.ActivityQueued,
			activity == buildstatus.ActivityPending,
			activity == buildstatus.ActivityPaused,
			outcome == buildstatus.OutcomeUnstable:
			cellStyle = t.S().StageBox.Pending
			cellStatus = stageStatus.Label()
		case activity == buildstatus.ActivityRunning:
			cellStyle = t.S().StageBox.Running
			cellStatus = svm.pulseBar.View()
		case outcome == buildstatus.OutcomeSuccess:
			cellStyle = t.S().StageBox.Success
		case outcome == buildstatus.OutcomeFailure:
			cellStyle = t.S().StageBox.Failed
			cellStatus = stageStatus.Label()
		case outcome == buildstatus.OutcomeAborted:
			cellStyle = t.S().StageBox.Aborted
			cellStatus = stageStatus.Label()
		case outcome == buildstatus.OutcomeNotBuilt:
			cellStyle = t.S().StageBox.NotExecuted
			cellStatus = stageStatus.Label()
			duration = "-"
		}

		cell = lipgloss.JoinVertical(lipgloss.Center,
			util.TurncateString(stage.Name, boxWidth-4),
			duration,
			cellStatus,
		)
		cell = cellStyle.Width(boxWidth).Render(cell)
		row = append(row, cell)
	}

	return lipgloss.JoinHorizontal(lipgloss.Top, row...)
}

// renderNoStageData renders a placeholder box for a run with no stage graph,
// telling the user where to look instead of leaving the row blank.
func (svm StageViewModel) renderNoStageData(boxWidth int) string {
	t := styles.GetTheme()
	cell := lipgloss.JoinVertical(lipgloss.Center,
		util.TurncateString("No stage data", boxWidth-4),
		util.TurncateString("check console log", boxWidth-4),
	)
	return t.S().StageBox.NotExecuted.Width(boxWidth).Render(cell)
}

func (svm *StageViewModel) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	svm.renderRevision++
	var cmds []tea.Cmd
	if key.Matches(msg, svm.keyMap.Up) {
		if svm.selectedIndex > 0 {
			svm.selectedIndex--
		}
		svm.handleSelectedStageVisible()
		return nil
	}
	if key.Matches(msg, svm.keyMap.Down) {
		if svm.pipelineRuns != nil && svm.selectedIndex < len(svm.pipelineRuns)-1 {
			svm.selectedIndex++
		}
		svm.handleSelectedStageVisible()
		return nil
	}

	cmds = append(cmds, svm.updateViewPort(msg))
	return tea.Batch(cmds...)
}

func (svm *StageViewModel) handleSelectedStageVisible() {
	// svm.viewport.SetYOffset(StageBoxHeight*svm.selectedIndex) // TODO make a little smoother
	svm.viewport.EnsureVisible(StageBoxHeight*svm.selectedIndex, 0, 0)
}

// runningAnim returns the pulse-bar animation string when the run is active,
// empty string for all other statuses.
func (svm *StageViewModel) runningAnim(run *gojenkins.PipelineRun) string {
	if run == nil {
		return ""
	}
	if buildstatus.PipelineStatus(run.Status).IsExecuting() {
		return " " + svm.spinnerModel.View()
	}
	return ""
}

func (svm *StageViewModel) scroll(num int) {
	if num < 0 {
		svm.viewport.ScrollUp(-num)
	}
	svm.viewport.ScrollDown(num)
}

func (svm *StageViewModel) onWindowSizeChanged(msg tea.WindowSizeMsg) tea.Cmd {
	svm.renderRevision++
	svm.wWithh = msg.Width
	svm.wHeight = msg.Height
	svm.viewport.SetWidth(msg.Width)
	svm.viewport.SetHeight(msg.Height)
	return svm.updateViewPort(msg)
}

func (svm *StageViewModel) updateViewPort(msg tea.Msg) tea.Cmd {
	viewport, cmd := svm.viewport.Update(msg)
	svm.viewport = &viewport
	return cmd
}
