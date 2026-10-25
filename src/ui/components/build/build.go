package build

import (
	cc "context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/buildaction"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/artifactselector"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/confirm"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/nodeselector"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/paramsview"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

const (
	StageBoxWidth  = 22
	StageBoxHeight = 8
)

type Model struct {
	ctx              *context.ProgramContext
	wWidth, wHeight  int
	keyMap           *KeyMap
	pipelineRun      gojenkins.PipelineRun
	build            *gojenkins.Build
	startedBy        string
	startedByDetails string
	spinnerModel     spinner.Model
	// pulseBar fills the running stage box, as on the job page.
	pulseBar *animation.PulseBar
	// clock is the page's one animation timer: the loading and header
	// spinners, the stage pulse, and the stage-details step spinner all
	// advance on its frames.
	clock animation.FrameClock
	// entered is true between Init and CleanUp. Only an entered page is on
	// screen, so late messages cannot start the clock on any other.
	entered bool
	// sections caches the page by section; a frame re-renders only the
	// sections that show a spinner.
	sections  pageSections
	stageView *viewport.Model

	stageDetails *StageDetails

	loading  bool
	jobURL   string
	buildURL string

	focusedStageIdx   int
	pollWatch         *poll.Watch
	fetchGeneration   int
	cancelRequest     cc.CancelFunc
	nodeGeneration    int
	cancelNodeRequest cc.CancelFunc
	paramsRequest     buildaction.ParamsRequest
}

func NewModel(ctx *context.ProgramContext, pipelineRun gojenkins.PipelineRun, width, height int) *Model {
	vp := viewport.New()
	vp.SetWidth(width)
	vp.SetHeight(10)

	jobURL := ""
	if pipelineRun.Job != nil && pipelineRun.Job.GetDetails() != nil {
		jobURL = pipelineRun.Job.GetDetails().URL
	}
	buildURL := pipelineRun.Base
	if jobURL != "" {
		buildURL = strings.TrimRight(jobURL, "/") + "/" + pipelineRun.ID
	}
	if ctx.Jenkins != nil {
		buildURL = jobref.ToAbsolute(ctx.Jenkins.Server, buildURL)
	}
	// The source Job page can retain this run in history; own the stage slice.
	pipelineRun.Stages = slices.Clone(pipelineRun.Stages)
	keyMap := UserKeyMap(ctx.Config.KeyMapConfig.Build)
	return &Model{
		loading:      true,
		jobURL:       jobURL,
		buildURL:     strings.TrimRight(buildURL, "/"),
		ctx:          ctx,
		wWidth:       width,
		wHeight:      height,
		pipelineRun:  pipelineRun,
		keyMap:       UserKeyMap(ctx.Config.KeyMapConfig.Build),
		spinnerModel: newSpinnerModel(),
		pulseBar:     newPulseBar(),
		stageDetails: NewStageDetails(width, height, keyMap),
		stageView:    &vp,
	}
}

// NewModelFromURL opens a stored build reference without contacting Jenkins.
func NewModelFromURL(ctx *context.ProgramContext, jobURL, buildID string, width, height int) *Model {
	jobURL = jobref.ToAbsolute(ctx.Jenkins.Server, jobURL)
	m := NewModel(ctx, gojenkins.PipelineRun{
		ID: buildID, Base: strings.TrimRight(jobURL, "/") + "/" + buildID,
	}, width, height)
	m.jobURL = jobURL
	return m
}

func (m *Model) Init() tea.Cmd {
	m.entered = true
	if m.loading {
		return tea.Batch(m.ensureAnimation(), m.fetchBuild())
	}
	if !m.isTerminal() {
		return tea.Batch(m.ensureAnimation(), m.watchExactRun())
	}
	return nil
}

// Update ends by syncing the animation clock, so any message that starts or
// ends running work also starts or stops the page animation. Crush's UI does
// the same with EnsureAnimating; see animation.FrameClock.Ensure.
func (m *Model) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	return m, tea.Batch(m.handleMsg(msg), m.ensureAnimation())
}

func (m *Model) View() string {
	if m.loading {
		return m.renderLoading()
	}
	return m.renderSections()
}

func (m *Model) ID() string {
	return "build:" + m.buildURL
}

func (m *Model) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl(m.buildURL)
}

func (m *Model) CleanUp() tea.Cmd {
	m.cancelInFlightRequest()
	m.cancelNodeFetch()
	m.entered = false
	m.clock.Stop()
	m.sections.reset()
	m.spinnerModel = newSpinnerModel()
	// Invalidate even a response that finished just before cancellation.
	m.fetchGeneration++
	m.nodeGeneration++
	m.paramsRequest.Invalidate()
	if m.pollWatch != nil {
		m.pollWatch.Release()
		m.pollWatch = nil
		log.Debug("build: CleanUp released poll watch", "buildID", m.ID())
	}
	return nil
}

func (m *Model) KeyMap() help.KeyMap {
	return m.keyMap
}

func (m *Model) handleMsg(msg tea.Msg) tea.Cmd {
	if _, ok := msg.(animation.FrameMsg); !ok {
		m.sections.invalidate()
	}
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKeyPressMsg(msg)
	case tea.WindowSizeMsg:
		return m.handleWindowSizeMsg(msg)
	case BuildFetchedMsg:
		return m.handleBuildFetchMsg(msg)
	case animation.FrameMsg:
		return m.handleAnimationFrame(msg)
	case BuildParamsFetchedMsg:
		return m.handleBuildParamsFetchMsg(msg)
	case buildaction.ParamsFetchedMsg:
		return m.paramsRequest.HandleWithTrigger(m.ctx, m.wWidth, msg, m.triggerBuildAndNavigate)
	case util.RunTriggeredMsg:
		return m.navigateAfterTrigger(msg)
	case ArtifactsDownloadedMsg:
		return m.handleArtifactDownloaded(msg)
	case ArtifactsDownloadFailedMsg:
		return util.CmdHandler(util.ErrorMsg{
			Title: "Failed to download artifacts",
			Error: msg.Error,
		})
	case NodeDownloadedMsg:
		return m.handleNodeDownloaded(msg)
	case FetchNodeLogMsg:
		return m.downloadFlowNodeLog(msg.NodeID)
	case FlowNodeLogFetched:
		return util.CmdHandler(OpenLogViewerMsg{
			FilePath: msg.FileName,
		})
	}

	return m.updateStageDetails(msg)
}

func (m *Model) renderLoading() string {
	label := "build"
	if m.jobURL != "" {
		label = jobref.ToDisplayName(m.jobURL)
	}
	content := fmt.Sprintf("%s Loading %s #%s...", m.spinnerModel.View(), label, m.pipelineRun.ID)
	content = styles.GetTheme().S().Base.Render(ansi.Truncate(content, max(0, m.wWidth), "…"))
	return lipgloss.Place(m.wWidth, m.wHeight, lipgloss.Center, lipgloss.Center, content)
}

// mergeDownloadedNode copies the executor from the detailed stage response into
// the run summary used by the build header and Copy Node action.
func (m *Model) mergeDownloadedNode(node gojenkins.PipelineNode) {
	if node.ID != "" {
		for i := range m.pipelineRun.Stages {
			if m.pipelineRun.Stages[i].ID == node.ID {
				m.pipelineRun.Stages[i].ExecNode = node.ExecNode
				return
			}
		}
	}
	if m.focusedStageIdx >= 0 && m.focusedStageIdx < len(m.pipelineRun.Stages) {
		m.pipelineRun.Stages[m.focusedStageIdx].ExecNode = node.ExecNode
	}
}

func (m *Model) isRunning() bool {
	return buildstatus.PipelineStatus(m.pipelineRun.Status).IsExecuting()
}

// isActive reports whether Jenkins still has work or user input pending.
func (m *Model) isActive() bool {
	status := buildstatus.PipelineStatus(m.pipelineRun.Status).
		NormalizeQueueState(len(m.pipelineRun.Stages), m.pipelineRun.EndTime)
	return status.IsActive(m.pipelineRun.EndTime)
}

func (m *Model) isTerminal() bool {
	return buildstatus.PipelineStatus(m.pipelineRun.Status).IsTerminal(m.pipelineRun.EndTime)
}

func (m *Model) isStageDetailsReady() bool {
	if len(m.pipelineRun.Stages) == 0 {
		return true
	}
	return m.stageDetails != nil
}

func (m *Model) renderBuildInfo(spinnerView string) string {
	run := m.pipelineRun
	t := styles.GetTheme()
	buildInfoStyle := t.S().Base.
		Width(40).
		Padding(2).
		// Border(lipgloss.RoundedBorder()).
		BorderForeground(t.Primary)

	status := buildstatus.PipelineStatus(run.Status).
		NormalizeQueueState(len(run.Stages), run.EndTime)
	statusIcon := m.getStatusIcon(status)
	statusText := status.Label()
	if status.Activity(run.EndTime) == buildstatus.ActivityPaused {
		statusText = t.S().Base.Foreground(t.Warning).Render(statusText)
	}
	var spinnerStr string
	if m.isRunning() {
		spinnerStr = spinnerView + " "
	}
	nodes := collectNodes(run.Stages)
	nodeName := "—"
	if len(nodes) == 1 {
		nodeName = middleTruncate(nodes[0].DisplayName(), 30)
	} else if len(nodes) > 1 {
		nodeName = fmt.Sprintf("multiple (%d)", len(nodes))
	}

	buildInfo := lipgloss.JoinVertical(lipgloss.Left,
		t.S().Title.Render(fmt.Sprintf("%sBuild %s %s", spinnerStr, run.Name, statusIcon)),
		"",
		t.S().Base.Bold(true).Render("Started: ")+util.FormatStartTime(run.StartTime),
		t.S().Base.Bold(true).Render("Duration: ")+util.FormatDuration(run.Duration),
		t.S().Base.Bold(true).Render("Status: ")+statusText,
		t.S().Base.Bold(true).Render("Started by: ")+m.startedBy,
		t.S().Base.Bold(true).Render("Details: ")+m.startedByDetails,
		t.S().Base.Bold(true).Render("Node: ")+nodeName,
	)
	return buildInfoStyle.Render(buildInfo)
}

func (m *Model) getStatusIcon(status buildstatus.PipelineStatus) string {
	t := styles.GetTheme()

	if status.IsExecuting() || status.Activity(0) == buildstatus.ActivityPaused {
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

func (m *Model) watchExactRun() tea.Cmd {
	if m.pollWatch != nil || m.ctx == nil || m.ctx.PollManager == nil || m.pipelineRun.Job == nil || m.pipelineRun.Job.GetDetails() == nil {
		return nil
	}
	buildNumber, err := strconv.ParseInt(m.pipelineRun.ID, 10, 64)
	if err != nil || buildNumber <= 0 {
		log.Error("build: unsupported Jenkins build ID", "buildID", m.pipelineRun.ID, "err", err)
		return nil
	}
	target := poll.Target{Kind: poll.TargetExactRun, JobURL: m.pipelineRun.Job.GetDetails().URL, BuildNumber: poll.BuildNumber(buildNumber)}
	watch, replay, err := m.ctx.PollManager.Watch(poll.WatchSpec{Target: target})
	if err != nil {
		log.Error("build: failed to watch exact run", "err", err, "jobURL", target.JobURL, "buildID", target.BuildNumber.BuildID())
		return nil
	}
	m.pollWatch = watch
	if replay.OK {
		return m.ApplyPollSnapshot(replay.Snapshot)
	}
	return nil
}

// ApplyPollSnapshot applies exact-run state routed by the app shell.
func (m *Model) ApplyPollSnapshot(snapshot poll.Snapshot) tea.Cmd {
	if m.loading {
		return nil
	}
	if snapshot.Target.Kind != poll.TargetExactRun || snapshot.Target.BuildNumber.BuildID() != m.pipelineRun.ID || len(snapshot.Runs) != 1 {
		return nil
	}
	if m.pollWatch != nil && !m.pollWatch.Matches(snapshot) {
		return nil
	}
	if m.pipelineRun.Job == nil || m.pipelineRun.Job.GetDetails() == nil || snapshot.Target.JobURL != m.pipelineRun.Job.GetDetails().URL {
		return nil
	}
	m.sections.invalidate()

	observed := snapshot.Runs[0]
	run := m.pipelineRun
	run.ID = observed.BuildNumber.BuildID()
	run.Name = observed.Name
	run.Status = string(observed.Status)
	run.StartTime = observed.StartTimeMillis
	run.EndTime = observed.EndTimeMillis
	run.Duration = observed.DurationMillis

	existingStages := make(map[string]gojenkins.PipelineNode, len(run.Stages))
	for i := range run.Stages {
		existingStages[run.Stages[i].ID] = run.Stages[i]
	}
	run.Stages = make([]gojenkins.PipelineNode, len(observed.Stages))
	for i := range observed.Stages {
		stageSnapshot := &observed.Stages[i]
		stage := existingStages[stageSnapshot.ID]
		stage.ID = stageSnapshot.ID
		stage.Name = stageSnapshot.Name
		stage.Status = string(stageSnapshot.Status)
		stage.ExecNode = stageSnapshot.ExecNode
		stage.StartTime = stageSnapshot.StartTimeMillis
		stage.Duration = stageSnapshot.DurationMillis
		run.Stages[i] = stage
	}
	m.pipelineRun = run
	// Pausing stops the clock and invalidates its pending frame; resuming
	// starts a new run of it.
	animationCmd := m.ensureAnimation()
	for i := range m.pipelineRun.Stages {
		m.pipelineRun.Stages[i].Run = &m.pipelineRun
	}

	if observed.Activity == buildstatus.ActivityFinished && m.pollWatch != nil {
		m.pollWatch.Release()
		m.pollWatch = nil
	}
	if m.stageDetails == nil {
		return animationCmd
	}
	m.updateFocusedStageIdx()
	return tea.Batch(animationCmd, m.getNode())
}

func (m *Model) cancelInFlightRequest() {
	if m.cancelRequest != nil {
		m.cancelRequest()
		m.cancelRequest = nil
	}
}

func (m *Model) triggerBuildAndNavigate(job *gojenkins.Job, params map[string]string) tea.Cmd {
	trigger := buildaction.Trigger(m.ctx, job, params)
	sourceViewID := m.ID()
	return func() tea.Msg {
		result := trigger()
		if msg, ok := result.(util.RunTriggeredMsg); ok {
			msg.SourceViewID = sourceViewID
			return msg
		}
		return result
	}
}

// navigateAfterTrigger opens the job page for a trigger this view started.
// The job page already shows the queue and later builds, so waiting on a
// queue ID to open the new run is unnecessary.
func (m *Model) navigateAfterTrigger(msg util.RunTriggeredMsg) tea.Cmd {
	if msg.SourceViewID == "" || msg.SourceViewID != m.ID() {
		return nil
	}
	job := msg.Job
	if job == nil {
		job = m.pipelineRun.Job
	}
	if job == nil {
		return nil
	}
	return util.CmdHandler(util.OpenJobComponentMsg{Job: job})
}

func (m *Model) renderArtifacts() string {
	t := styles.GetTheme()
	var artifactLines []string
	artifactLines = append(artifactLines, t.S().Subtitle.Render("Artifacts"))
	if m.build != nil {
		artifacts := m.build.GetArtifacts()
		if len(artifacts) == 0 {
			artifactLines = append(artifactLines, "No artifacts created")
			return lipgloss.JoinVertical(lipgloss.Left, artifactLines...)
		}

		artifactStyle := t.S().Base

		for i, artifact := range artifacts {
			artifactInfo := lipgloss.JoinVertical(lipgloss.Left,
				t.S().Base.Bold(true).Render(fmt.Sprintf("%d. %s", i+1, artifact.FileName)),
			)
			artifactLines = append(artifactLines, artifactStyle.Render(artifactInfo))
		}
	}

	return lipgloss.JoinVertical(lipgloss.Left, artifactLines...)
}

func (m *Model) renderBuildFlow() string {
	t := styles.GetTheme()
	run := m.pipelineRun

	var sections []string
	// Stages Section
	switch {
	case len(run.Stages) > 0:
		sections = append(sections, m.renderStages())
	case m.isTerminal():
		// wfapi reports the stage graph, not the build result. A run that never
		// reaches a stage (e.g. a Jenkinsfile parse error) has no graph to report
		// on, so it shows up here with zero stages regardless of pass/fail.
		sections = append(sections, m.renderNoStageData())
	default:
		queue := t.S().Base.Foreground(t.Grey).
			Bold(true).
			Italic(true).
			Render("Build in queue...")
		sections = append(sections, queue)
	}

	return lipgloss.JoinVertical(lipgloss.Center, sections...)
}

// renderNoStageData renders a placeholder box for a run with no stage graph,
// telling the user where to look instead of leaving the view blank.
func (m *Model) renderNoStageData() string {
	t := styles.GetTheme()
	boxWidth := styles.StageBoxContentWidth(m.wWidth)
	cell := lipgloss.JoinVertical(lipgloss.Center,
		util.TurncateString("No stage data", boxWidth-4),
		util.TurncateString("check console log", boxWidth-4),
	)
	return t.S().StageBox.NotExecuted.Width(boxWidth).Render(cell)
}

func (m *Model) renderStages() string {
	t := styles.GetTheme()
	boxWidth := styles.StageBoxContentWidth(m.wWidth)
	var stageBoxes []string

	for i, stage := range m.pipelineRun.Stages {
		var cellContent, cellStatus string
		var cellStyle lipgloss.Style

		duration := util.FormatDuration(stage.Duration)
		status := buildstatus.PipelineStatus(stage.Status)
		activity, outcome := status.Activity(0), status.Outcome()
		cellStatus = status.Label()

		switch {
		case activity == buildstatus.ActivityQueued,
			activity == buildstatus.ActivityPending,
			outcome == buildstatus.OutcomeUnstable:
			cellStyle = t.S().StageBox.Pending
		case activity == buildstatus.ActivityPaused:
			cellStyle = t.S().StageBox.Pending
			cellStatus = t.S().Base.Foreground(t.Warning).Render(status.Label())
		case activity == buildstatus.ActivityRunning:
			cellStyle = t.S().StageBox.Running
			cellStatus = m.pulseBar.View()
		case outcome == buildstatus.OutcomeSuccess:
			cellStyle = t.S().StageBox.Success
		case outcome == buildstatus.OutcomeFailure:
			cellStyle = t.S().StageBox.Failed
		case outcome == buildstatus.OutcomeAborted:
			cellStyle = t.S().StageBox.Aborted
		case outcome == buildstatus.OutcomeNotBuilt:
			cellStyle = t.S().StageBox.NotExecuted
			duration = "-"
		}

		// Apply focused border if this stage is focused and stage view is active
		if i == m.focusedStageIdx {
			cellStyle = cellStyle.BorderForeground(t.StageFocusedBorder).Border(lipgloss.DoubleBorder())
		}

		cellContent = lipgloss.JoinVertical(lipgloss.Center,
			util.TurncateString(stage.Name, boxWidth-4),
			duration,
			cellStatus,
		)

		stageBoxes = append(stageBoxes, cellStyle.Width(boxWidth).Render(cellContent))
	}

	return lipgloss.JoinHorizontal(lipgloss.Center, stageBoxes...)
}

func (m *Model) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	if m.loading {
		return nil
	}
	var cmds []tea.Cmd
	cmds = append(cmds, m.handleKeyPressMsgStageView(msg))
	if m.stageDetailsStale() {
		cmds = append(cmds, m.handleKeyPressMsgStaleDetails(msg))
	} else {
		cmds = append(cmds, m.handleKeyPressMsgDetailsView(msg))
	}

	stage, cmd := m.stageView.Update(msg)
	m.stageView = &stage
	cmds = append(cmds, cmd)
	return tea.Batch(cmds...)
}

func (m *Model) handleKeyPressMsgStageView(msg tea.KeyPressMsg) tea.Cmd {
	if m.build == nil {
		return nil
	}

	if key.Matches(msg, m.keyMap.OpenFullLog) {
		return buildaction.OpenLog(m.pipelineRun)
	}

	if key.Matches(msg, m.keyMap.OpenJob) {
		return buildaction.OpenJob(m.pipelineRun.Job)
	}

	if key.Matches(msg, m.keyMap.Replay) {
		return util.CmdHandler(ReplayMsg{
			Job:         m.pipelineRun.Job,
			PipelineRun: m.pipelineRun,
		})
	}

	if key.Matches(msg, m.keyMap.ShowParams) {
		return m.fetchBuildParams()
	}

	if key.Matches(msg, m.keyMap.TriggerBuild) {
		return m.paramsRequest.Fetch(m.ctx, m.pipelineRun.Job)
	}

	if key.Matches(msg, m.keyMap.CancelBuild) {
		return buildaction.ConfirmAbort(m.ctx, m.pipelineRun.Job, m.pipelineRun.ID, m.isTerminal())
	}

	if key.Matches(msg, m.keyMap.OpenBuildBrowser) {
		return buildaction.OpenBuildInBrowser(m.pipelineRun.Job.Raw.URL, m.pipelineRun.ID)
	}

	if key.Matches(msg, m.keyMap.OpenJobBrowser) {
		return buildaction.OpenJobInBrowser(m.pipelineRun.Job.Raw.URL)
	}

	if key.Matches(msg, m.keyMap.DownloadAllArtifacts) {
		return m.startArtifactDownload(m.build.GetArtifacts())
	}

	if key.Matches(msg, m.keyMap.DownloadArtifacts) {
		artifacts := m.build.GetArtifacts()
		if len(artifacts) <= 1 {
			return m.startArtifactDownload(artifacts)
		}
		dialog := artifactselector.New(
			artifacts,
			m.wWidth,
			m.wHeight,
			m.startArtifactDownload,
			m.ctx.Config.KeyMapConfig.Dialog.ArtifactSelector,
		)
		return util.CmdHandler(util.OpenDialogMsg{Dialog: dialog})
	}

	if key.Matches(msg, m.keyMap.CopyNode) {
		nodes := collectNodes(m.pipelineRun.Stages)
		switch len(nodes) {
		case 0:
			return util.CmdHandler(util.FooterNotiMsg{
				Message: "No node information available for this build",
				Type:    util.NotiWarning,
			})
		case 1:
			return tea.Batch(
				tea.SetClipboard(nodes[0].Name),
				util.CmdHandler(util.FooterNotiMsg{Message: copyNoti(nodes[0]), Type: util.NotiInfo}),
			)
		default:
			items := make([]nodeselector.NodeItem, len(nodes))
			for i := range nodes {
				items[i] = nodeselector.NewNodeItem(nodes[i].Name, nodes[i].Stages)
			}
			return util.CmdHandler(util.OpenDialogMsg{
				Dialog: nodeselector.New(m.ctx, items, m.wWidth, m.wHeight),
			})
		}
	}

	if len(m.pipelineRun.Stages) > 0 {
		if key.Matches(msg, m.keyMap.NextStage) {
			m.focusedStageIdx = (m.focusedStageIdx + 1) % len(m.pipelineRun.Stages)
			// m.stageDeatils.pipelineNode = &m.pipelineRun.Stages[m.focusedStageIdx]
			// return nil

			m.stageDetails.init()
			return m.getNode()
		}
		if key.Matches(msg, m.keyMap.PrevStage) {
			m.focusedStageIdx--
			if m.focusedStageIdx < 0 {
				m.focusedStageIdx = len(m.pipelineRun.Stages) - 1
			}
			// m.stageDeatils.pipelineNode = &m.pipelineRun.Stages[m.focusedStageIdx]
			// return nil

			m.stageDetails.init()
			return m.getNode()
		}
	}
	return nil
}

// stageDetailsStale reports whether the details pane still shows a stage other
// than the highlighted one: switching stages moves the box at once, but the
// new stage's details arrive asynchronously.
func (m *Model) stageDetailsStale() bool {
	if m.stageDetails == nil || m.stageDetails.pipelineNode == nil {
		return false
	}
	if m.focusedStageIdx < 0 || m.focusedStageIdx >= len(m.pipelineRun.Stages) {
		return false
	}
	return m.stageDetails.pipelineNode.ID != m.pipelineRun.Stages[m.focusedStageIdx].ID
}

// handleKeyPressMsgStaleDetails withholds keys from a details pane that still
// shows the previous stage, so they cannot act on that stage's steps. With no
// fetch in flight the last load failed, so the step-log key retries it.
func (m *Model) handleKeyPressMsgStaleDetails(msg tea.KeyPressMsg) tea.Cmd {
	if !key.Matches(msg, m.keyMap.OpenStepLog) {
		return nil
	}
	notice := util.CmdHandler(util.FooterNotiMsg{
		Message: fmt.Sprintf("Loading %s stage…", m.pipelineRun.Stages[m.focusedStageIdx].Name),
	})
	if m.cancelNodeRequest != nil {
		return notice
	}
	return tea.Batch(notice, m.getNode())
}

func (m *Model) handleKeyPressMsgDetailsView(msg tea.KeyPressMsg) tea.Cmd {
	stageDeatils, cmd := m.stageDetails.Update(msg)
	if stageDeatils, ok := stageDeatils.(*StageDetails); ok {
		m.stageDetails = stageDeatils
	}
	return cmd
}

func (m *Model) fetchBuildParams() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := m.ctx.RequestContext()
		// Release the deadline timer as soon as the request finishes instead of
		// retaining it until the timeout expires.
		defer cancel()
		params, err := m.build.GetBuildParameters(ctx)
		if err != nil {
			return BuildParamsFetchedMsg{Error: err}
		}
		return BuildParamsFetchedMsg{Params: params}
	}
}

func (m *Model) handleBuildParamsFetchMsg(msg BuildParamsFetchedMsg) tea.Cmd {
	if msg.Error != nil {
		return util.CmdHandler(util.ErrorMsg{
			Title: "Failed to fetch build parameters",
			Error: msg.Error,
		})
	}
	dialog := paramsview.NewParamsDialog(msg.Params, m.wWidth, m.ctx.Config.KeyMapConfig.Dialog.ParamsView)
	return util.CmdHandler(util.OpenDialogMsg{Dialog: dialog})
}

func (m *Model) startArtifactDownload(artifacts []gojenkins.Artifact) tea.Cmd {
	if len(artifacts) == 0 {
		return util.CmdHandler(util.FooterNotiMsg{
			Message: "No artifacts available for this build",
			Type:    util.NotiWarning,
		})
	}
	return tea.Sequence(
		util.CmdHandler(util.FooterNotiMsg{
			Message: fmt.Sprintf("Downloading %d artifact(s)...", len(artifacts)),
			Type:    util.NotiInfo,
		}),
		m.downloadArtifacts(artifacts),
	)
}

func (m *Model) downloadArtifacts(artifacts []gojenkins.Artifact) tea.Cmd {
	artifacts = append([]gojenkins.Artifact(nil), artifacts...)
	serverID := "jenkins"
	if m.ctx.ServerConfig != nil && m.ctx.ServerConfig.ID != "" {
		serverID = m.ctx.ServerConfig.ID
	}
	jobID := m.pipelineRun.Job.Base
	if jobID == "" {
		jobID = m.pipelineRun.Job.GetName()
	}
	dir := filepath.Join(
		m.ctx.DataArtifactsDir,
		safePathComponent(serverID),
		safePathComponent(jobID),
		safePathComponent(m.pipelineRun.ID),
	)

	return func() tea.Msg {
		if len(artifacts) == 0 {
			return ArtifactsDownloadFailedMsg{Error: fmt.Errorf("no artifacts available")}
		}
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return ArtifactsDownloadFailedMsg{Error: err}
		}
		for _, artifact := range artifacts {
			name := artifact.FileName
			if name == "" || name == "." || name == ".." || filepath.Base(name) != name || strings.ContainsAny(name, `/\\`) {
				return ArtifactsDownloadFailedMsg{Error: fmt.Errorf("unsafe artifact filename %q", name)}
			}

			fileName := filepath.Join(dir, name)
			file, err := os.OpenFile(fileName, os.O_CREATE|os.O_WRONLY, 0o600)
			if err != nil {
				return ArtifactsDownloadFailedMsg{Error: fmt.Errorf("failed to prepare %s: %w", name, err)}
			}
			if err := file.Close(); err != nil {
				return ArtifactsDownloadFailedMsg{Error: fmt.Errorf("failed to prepare %s: %w", name, err)}
			}

			ctx, cancel := m.ctx.DownloadContext()
			saved, err := artifact.SaveToDir(ctx, dir)
			cancel()
			if err != nil {
				_ = os.Remove(fileName)
				return ArtifactsDownloadFailedMsg{Error: fmt.Errorf("failed to save %s: %w", name, err)}
			}
			if !saved {
				_ = os.Remove(fileName)
				return ArtifactsDownloadFailedMsg{Error: fmt.Errorf("failed to save %s: Jenkins client reported an unsuccessful download", name)}
			}
		}

		return ArtifactsDownloadedMsg{Dir: dir, Count: len(artifacts)}
	}
}

func safePathComponent(value string) string {
	value = strings.TrimSpace(value)
	value = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, value)
	value = strings.Trim(value, ".")
	if value == "" {
		return "unknown"
	}
	return value
}

func (m *Model) handleArtifactDownloaded(msg ArtifactsDownloadedMsg) tea.Cmd {
	dialog := confirm.NewConfirmDialog(
		fmt.Sprintf("Downloaded %d artifact(s). Open folder?", msg.Count),
		util.Open(msg.Dir),
		nil,
		m.ctx.Config.KeyMapConfig.Dialog.Confirm,
	)
	return tea.Batch(
		util.CmdHandler(util.FooterNotiMsg{
			Message: fmt.Sprintf("Artifacts saved to %s", msg.Dir),
			Type:    util.NotiInfo,
		}),
		util.CmdHandler(util.OpenDialogMsg{Dialog: dialog}),
	)
}

func (m *Model) handleBuildFetchMsg(msg BuildFetchedMsg) tea.Cmd {
	if msg.model != m || msg.Generation != m.fetchGeneration {
		return nil
	}
	m.cancelInFlightRequest()
	if msg.Error != nil {
		if errors.Is(msg.Error, cc.Canceled) {
			log.Debug("build: request cancelled", "buildID", m.pipelineRun.ID)
			return nil
		}
		return util.CmdHandler(LoadFailedMsg{Model: m, Generation: msg.Generation, Error: msg.Error, ErrorTitle: msg.ErrorTitle})
	}

	m.pipelineRun = msg.PipelineRun
	m.startedBy = msg.StartedBy
	m.startedByDetails = msg.StartedByDetails
	m.build = &msg.Build

	m.loading = false
	m.focusedStageIdx = 0
	m.updateFocusedStageIdx()

	// Update's ensureAnimation keeps the loading clock running into a live
	// build, so the page never has two chains.
	var cmds []tea.Cmd
	if msg.Node != nil {
		m.mergeDownloadedNode(*msg.Node)
		cmds = append(cmds, m.updateStageDetails(NodeDownloadedMsg{PipelineNode: *msg.Node}))
	}
	cmds = append(cmds, m.sendWindowSizeMsgToStageDetails())
	if !m.isTerminal() {
		cmds = append(cmds, m.watchExactRun())
	}
	return tea.Batch(cmds...)
}

func (m *Model) getNode() tea.Cmd {
	m.cancelNodeFetch()
	m.nodeGeneration++
	if len(m.pipelineRun.Stages) == 0 || m.focusedStageIdx >= len(m.pipelineRun.Stages) {
		return nil
	}
	ctx, cancel := m.ctx.RequestContext()
	m.cancelNodeRequest = cancel
	generation := m.nodeGeneration
	run := m.pipelineRun
	stage := run.Stages[m.focusedStageIdx]
	return func() tea.Msg {
		defer cancel()
		n, err := run.GetNode(ctx, stage.ID)
		msg := NodeDownloadedMsg{
			model: m, Generation: generation, Error: err,
			ErrorTitle: fmt.Sprintf("Could not get '%s' Build stage details", stage.Name),
		}
		if n != nil {
			msg.PipelineNode = *n
		}
		return msg
	}
}

func (m *Model) cancelNodeFetch() {
	if m.cancelNodeRequest != nil {
		m.cancelNodeRequest()
		m.cancelNodeRequest = nil
	}
}

func (m *Model) handleNodeDownloaded(msg NodeDownloadedMsg) tea.Cmd {
	if msg.model != m || msg.Generation != m.nodeGeneration {
		return nil
	}
	m.cancelNodeFetch()
	if msg.Error != nil {
		if errors.Is(msg.Error, cc.Canceled) {
			return nil
		}
		return util.CmdHandler(util.ErrorMsg{Error: msg.Error, Title: msg.ErrorTitle})
	}
	m.mergeDownloadedNode(msg.PipelineNode)
	return tea.Batch(m.updateStageDetails(msg), m.sendWindowSizeMsgToStageDetails())
}

// MatchesLoadFailure lets the shell reject a failure delivered after Back,
// a retry, another build, or a server switch.
func (m *Model) MatchesLoadFailure(msg LoadFailedMsg) bool {
	return msg.Model == m && msg.Generation == m.fetchGeneration && m.loading
}

func (m *Model) updateStageDetails(msg tea.Msg) tea.Cmd {
	if m.stageDetails == nil {
		return nil
	}
	var cmd tea.Cmd
	stageDetails, cmd := m.stageDetails.Update(msg)
	if stageDetails, ok := stageDetails.(*StageDetails); ok {
		m.stageDetails = stageDetails
	}
	return cmd
}

func (m *Model) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	m.wWidth = msg.Width
	m.wHeight = msg.Height
	m.stageView.SetWidth(m.wWidth)
	return m.sendWindowSizeMsgToStageDetails()
}

func (m *Model) sendWindowSizeMsgToStageDetails() tea.Cmd {
	return m.updateStageDetails(tea.WindowSizeMsg{
		Width: m.wWidth, Height: m.availableStageDetailsHeight(),
	})
}

func (m *Model) availableStageDetailsHeight() int {
	m.updateStageViewLayout()
	topHeight := lipgloss.Height(lipgloss.JoinHorizontal(
		lipgloss.Center,
		m.renderBuildInfo(m.spinnerModel.View()),
		m.renderArtifacts(),
	))
	height := m.wHeight - topHeight - lipgloss.Height(m.stageView.View())
	if height < 0 {
		return 0
	}
	return height
}

func (m *Model) updateStageViewLayout() {
	m.stageView.SetContent(m.renderBuildFlow())
	m.stageView.Style = styles.GetTheme().S().SelectedBorder.
		BorderTop(true).
		BorderBottom(true).
		BorderLeft(false).
		BorderRight(false)
}

// shouldAnimate covers the loading spinner and a running build. A paused or
// finished build is static, and a page outside Init/CleanUp is not on
// screen, so neither keeps the clock alive.
func (m *Model) shouldAnimate() bool {
	return m.entered && (m.loading || m.isRunning())
}

func (m *Model) ensureAnimation() tea.Cmd {
	return m.clock.Ensure(m.shouldAnimate())
}

func (m *Model) handleAnimationFrame(msg animation.FrameMsg) tea.Cmd {
	if !m.clock.Accept(msg) {
		return nil
	}
	if !m.shouldAnimate() {
		return nil
	}
	m.spinnerModel = animation.AdvanceSpinner(m.spinnerModel)
	if m.loading {
		return m.clock.Next()
	}
	m.pulseBar.Step()
	if m.stageDetails != nil {
		m.stageDetails.advanceAnimation()
	}
	return m.clock.Next()
}

// hasRunningStage reports whether a stage box shows the pulse bar.
func (m *Model) hasRunningStage() bool {
	for _, stage := range m.pipelineRun.Stages {
		if buildstatus.PipelineStatus(stage.Status).Activity(0) == buildstatus.ActivityRunning {
			return true
		}
	}
	return false
}

func newPulseBar() *animation.PulseBar {
	return animation.New(animation.Settings{Width: 14, FPS: 10})
}

func newSpinnerModel() spinner.Model {
	return spinner.New(
		spinner.WithSpinner(spinner.Dot),
		spinner.WithStyle(styles.GetTheme().S().Spinner),
	)
}

func (m *Model) updateFocusedStageIdx() {
	m.focusedStageIdx = focusedStageIndex(m.pipelineRun.Stages, m.focusedStageIdx)
}

func focusedStageIndex(stages []gojenkins.PipelineNode, current int) int {
	for i, stage := range stages {
		status := buildstatus.PipelineStatus(stage.Status)
		if status.IsExecuting() || status.Activity(0) == buildstatus.ActivityPaused {
			return i
		}
	}
	return min(max(0, current), max(0, len(stages)-1))
}

func (m *Model) downloadFlowNodeLog(nodeID string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := m.ctx.RequestContext()
		defer cancel()
		nodeLog, err := m.pipelineRun.GetNodeLog(ctx, nodeID)
		if err != nil {
			return util.ErrorMsg{
				Error: err,
				Title: "Failed to get node log",
			}
		}

		if nodeLog == nil || nodeLog.Text == "" {
			return util.ErrorMsg{
				Error: fmt.Errorf("log is empty"),
				Title: "Failed to get node log",
			}
		}

		// Create a private directory for flow node logs.
		dir := filepath.Join(m.ctx.DataLogsDir, "flow_nodes", safePathComponent(m.pipelineRun.ID))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return util.ErrorMsg{Error: err, Title: "Failed to create directory"}
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return util.ErrorMsg{Error: err, Title: "Failed to secure directory"}
		}

		fileName := filepath.Join(dir, fmt.Sprintf("%s.log", safePathComponent(nodeID)))
		if err := os.WriteFile(fileName, []byte(nodeLog.Text), 0o600); err != nil {
			return util.ErrorMsg{Error: err, Title: "Failed to write log"}
		}
		if err := os.Chmod(fileName, 0o600); err != nil {
			return util.ErrorMsg{Error: err, Title: "Failed to secure log"}
		}

		return FlowNodeLogFetched{
			NodeID:   nodeID,
			FileName: fileName,
		}
	}
}
