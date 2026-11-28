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
	"github.com/atm1020/jenktui/src/stagegraph"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/buildaction"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/artifactselector"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/confirm"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/nodeselector"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/paramsview"
	"github.com/atm1020/jenktui/src/ui/components/logviewer"
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

	buildInfoPanelHeight = 5 // 2 border lines + 3 compact metadata lines
	narrowSplitThreshold = 56
)

type focusPane int

const (
	focusBuild focusPane = iota
	focusLog
)

type activeBorder int

const (
	borderStages activeBorder = iota
	borderSteps
	borderLog
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
	// spinners, the stage pulse, the stage-details step spinner, and the
	// inline log loading indicator all advance on its frames.
	clock animation.FrameClock
	// entered is true between Init and CleanUp. Only an entered page is on
	// screen, so late messages cannot start the clock on any other.
	entered bool
	// sections caches the page by section; a frame re-renders only the
	// sections that show a spinner or pulse.
	sections  pageSections
	stageView *viewport.Model

	stageDetails *StageDetails
	logPane      *logPane

	focusPane      focusPane
	activeBorder   activeBorder
	startInFullLog bool

	loading  bool
	jobURL   string
	buildURL string

	pluginTree         *stagegraph.PluginTree
	stageGraph         stagegraph.Graph
	navStages          []stagegraph.StageNode
	focusedStageIdx    int
	userSelectedStage  bool
	pollWatch          *poll.Watch
	fetchGeneration    int
	cancelRequest      cc.CancelFunc
	nodeGeneration     int
	cancelNodeRequest  cc.CancelFunc
	graphGeneration    int
	cancelGraphRequest cc.CancelFunc
	paramsRequest      buildaction.ParamsRequest
}

func NewModel(ctx *context.ProgramContext, pipelineRun gojenkins.PipelineRun, width, height int) *Model {
	vp := viewport.New()
	vp.SetWidth(max(1, width-2))
	vp.SetHeight(timelineMinHeight)

	jobURL := ""
	if pipelineRun.Job != nil && pipelineRun.Job.GetDetails() != nil {
		jobURL = pipelineRun.Job.GetDetails().URL
	}
	buildURL := pipelineRun.Base
	if jobURL != "" {
		buildURL = strings.TrimRight(jobURL, "/") + "/" + pipelineRun.ID
	}
	if ctx != nil && ctx.Jenkins != nil {
		buildURL = jobref.ToAbsolute(ctx.Jenkins.Server, buildURL)
	}
	// The source Job page can retain this run in history; own the stage slice.
	pipelineRun.Stages = slices.Clone(pipelineRun.Stages)
	var keyMap *KeyMap
	if ctx != nil && ctx.Config != nil {
		keyMap = UserKeyMap(ctx.Config.KeyMapConfig.Build)
	} else {
		keyMap = &KeyMap{}
	}
	m := &Model{
		loading:      true,
		jobURL:       jobURL,
		buildURL:     strings.TrimRight(buildURL, "/"),
		ctx:          ctx,
		wWidth:       width,
		wHeight:      height,
		pipelineRun:  pipelineRun,
		keyMap:       keyMap,
		spinnerModel: newSpinnerModel(),
		pulseBar:     newPulseBar(),
		stageDetails: NewStageDetails(width, height, keyMap),
		logPane:      newLogPane(width, height),
		stageView:    &vp,
		focusPane:    focusBuild,
		activeBorder: borderStages,
	}
	m.syncPaneFocus()
	return m
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

// NewModelForLog opens the unified Build page directly in zoomed full console
// log mode (used when pressing 'L' from Job or My Runs). Pressing 'z' unzooms
// into the full Build + Stages + Steps + Log view.
func NewModelForLog(ctx *context.ProgramContext, pipelineRun gojenkins.PipelineRun, width, height int) *Model {
	m := NewModel(ctx, pipelineRun, width, height)
	m.startInFullLog = true
	m.focusPane = focusLog
	m.activeBorder = borderLog
	m.logPane.mode = logModeFull
	m.logPane.zoomed = true
	m.syncPaneFocus()
	m.syncPaneSizes()
	return m
}

// NewModelForLogFromURL opens a stored run reference directly in zoomed full
// console log mode without contacting Jenkins before navigation.
func NewModelForLogFromURL(ctx *context.ProgramContext, jobURL, buildID string, width, height int) *Model {
	absURL := jobref.ToAbsolute(ctx.Jenkins.Server, jobURL)
	jobID, parentIDs := jobref.ToIDAndParents(absURL)
	job := ctx.Jenkins.JobRef(jobID, parentIDs...)
	job.Raw.URL = absURL
	run := gojenkins.PipelineRun{
		ID:   buildID,
		Base: strings.TrimRight(job.Base, "/") + "/" + buildID,
		Job:  job,
	}
	m := NewModelForLog(ctx, run, width, height)
	m.jobURL = absURL
	return m
}

func (m *Model) Init() tea.Cmd {
	m.entered = true
	var cmds []tea.Cmd
	if anim := m.ensureAnimation(); anim != nil {
		cmds = append(cmds, anim)
	}
	if m.loading {
		cmds = append(cmds, m.fetchBuild())
	} else if !m.isTerminal() {
		cmds = append(cmds, m.watchExactRun())
	}
	if m.startInFullLog && m.logPane != nil && m.logPane.viewer == nil && !m.logPane.loading {
		if cmd := m.loadFullLog(false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// Update ends by syncing the animation clock, so any message that starts or
// ends running work also starts or stops the page animation. Crush's UI does
// the same with EnsureAnimating; see animation.FrameClock.Ensure.
func (m *Model) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	return m, tea.Batch(m.handleMsg(msg), m.ensureAnimation())
}

func (m *Model) View() string {
	if m.logPane != nil && m.logPane.zoomed {
		// In zoomed log mode (including when opened directly via 'L'), render
		// the log panel immediately so full-log download does not wait on build metadata.
		return m.renderSections()
	}
	if m.loading {
		return m.renderLoading()
	}
	return m.renderSections()
}

func (m *Model) ID() string {
	return "build:" + m.buildURL
}

func (m *Model) UpdateFooter() tea.Cmd {
	if m.logPane != nil && m.logPane.mode == logModeFull && (m.startInFullLog || m.logPane.zoomed || m.focusPane == focusLog) {
		return util.CmdFooterUrl(m.buildURL + "/console")
	}
	return util.CmdFooterUrl(m.buildURL)
}

func (m *Model) CleanUp() tea.Cmd {
	m.cancelInFlightRequest()
	m.cancelNodeFetch()
	m.cancelGraphFetch()
	m.entered = false
	m.clock.Stop()
	m.sections.reset()
	m.spinnerModel = newSpinnerModel()
	// Invalidate even a response that finished just before cancellation.
	m.fetchGeneration++
	m.nodeGeneration++
	m.graphGeneration++
	m.paramsRequest.Invalidate()
	if m.pollWatch != nil {
		m.pollWatch.Release()
		m.pollWatch = nil
		log.Debug("build: CleanUp released poll watch", "buildID", m.ID())
	}
	if m.logPane != nil {
		return m.logPane.cleanUp()
	}
	return nil
}

func (m *Model) KeyMap() help.KeyMap {
	if m.focusPane == focusLog && m.logPane != nil && m.logPane.viewer != nil {
		return &logFocusedKeyMap{build: m.keyMap, log: m.logPane.viewer.KeyMap()}
	}
	return m.keyMap
}

// IsCapturingInput reports whether the inline log viewer is actively capturing
// text (search input or visual selection) so the app shell withholds global
// letter shortcuts like Back ("b") and My Runs ("m").
func (m *Model) IsCapturingInput() bool {
	return m.logPane != nil && m.logPane.isCapturingInput()
}

type logFocusedKeyMap struct {
	build *KeyMap
	log   help.KeyMap
}

func (k *logFocusedKeyMap) ShortHelp() []key.Binding {
	out := append([]key.Binding(nil), k.log.ShortHelp()...)
	if k.build != nil {
		out = append(out, k.build.SwitchPane, k.build.FocusBuild, k.build.ZoomLog, k.build.OpenFullLog)
	}
	return out
}

func (k *logFocusedKeyMap) FullHelp() [][]key.Binding {
	groups := append([][]key.Binding(nil), k.log.FullHelp()...)
	if k.build != nil {
		groups = append(groups, []key.Binding{k.build.SwitchPane, k.build.FocusBuild, k.build.ZoomLog, k.build.OpenFullLog})
	}
	return groups
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
	case GraphTreeFetchedMsg:
		return m.handleGraphTreeFetched(msg)
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
		return m.handleFetchNodeLog(msg)
	case StepLogFetchedMsg:
		return m.handleStepLogFetched(msg)
	case FullLogDownloadedMsg:
		return m.handleFullLogDownloaded(msg)
	case ScopedLogExtractedMsg:
		return m.handleScopedLogExtracted(msg)
	case logviewer.ChunkLoadedMsg:
		cmd := m.updateLogViewer(msg)
		if m.logPane != nil && m.logPane.pendingJumpLine > 0 && m.logPane.viewer != nil && m.logPane.viewer.TotalLines() > 0 {
			jumpLine := m.logPane.pendingJumpLine
			m.logPane.pendingJumpLine = 0
			return tea.Batch(cmd, m.logPane.viewer.JumpToLine(jumpLine))
		}
		return cmd
	}

	var cmds []tea.Cmd
	if cmd := m.updateLogViewer(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if cmd := m.updateStageDetails(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
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

// renderBuildInfo draws the compact Omaci-inspired framed build summary bar
// (5 lines tall including borders), keeping all build metadata and artifacts
// readable while leaving generous vertical space for the timeline, steps, and log.
func (m *Model) renderBuildInfo(spinnerView string) string {
	run := m.pipelineRun
	t := styles.GetTheme()
	width := m.wWidth
	if width <= 0 {
		width = 100
	}
	width = max(20, width)
	innerW := width - 2

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

	sep := t.S().Base.Foreground(t.FaintBorder).Render("  ·  ")
	label := func(name string) string { return t.S().Base.Bold(true).Render(name) }

	row1 := " " + t.S().Title.Render(fmt.Sprintf("%sBuild %s %s", spinnerStr, run.Name, statusIcon)) +
		sep + label("Started: ") + util.FormatStartTime(run.StartTime) +
		sep + label("Duration: ") + util.FormatDuration(run.Duration) +
		sep + label("Status: ") + statusText

	startedBy := m.startedBy
	if startedBy == "" {
		startedBy = "—"
	}
	row2 := " " + label("Started by: ") + startedBy
	if m.startedByDetails != "" && m.startedByDetails != m.startedBy {
		row2 += sep + label("Details: ") + m.startedByDetails
	}
	row2 += sep + label("Node: ") + nodeName

	row3 := " " + m.renderArtifactsInline(innerW-1)

	body := strings.Join([]string{
		ansi.Truncate(row1, innerW, "…"),
		ansi.Truncate(row2, innerW, "…"),
		ansi.Truncate(row3, innerW, "…"),
	}, "\n")

	note := ""
	if m.jobURL != "" {
		note = jobref.ToDisplayName(m.jobURL)
	}
	right := fmt.Sprintf("%s%s %s", spinnerStr, statusIcon, statusText)
	return renderPanelFrame(panelFrameOpts{
		Title:  "build",
		Note:   note,
		Right:  right,
		Width:  width,
		Height: buildInfoPanelHeight,
		Active: false,
	}, body)
}

func (m *Model) renderArtifactsInline(maxW int) string {
	t := styles.GetTheme()
	prefix := t.S().Subtitle.Render("Artifacts") + t.S().Base.Bold(true).Render(": ")
	if m.build == nil {
		return prefix + t.S().Base.Foreground(t.SubtleText).Render("No artifacts created")
	}
	artifacts := m.build.GetArtifacts()
	if len(artifacts) == 0 {
		return prefix + t.S().Base.Foreground(t.SubtleText).Render("No artifacts created")
	}
	names := make([]string, 0, len(artifacts))
	for i, artifact := range artifacts {
		names = append(names, fmt.Sprintf("%d. %s", i+1, artifact.FileName))
	}
	return ansi.Truncate(prefix+strings.Join(names, ", "), max(1, maxW), "…")
}

func (m *Model) renderStagesPanel() string {
	m.updateStageViewLayout()
	note, count := m.stagesPanelMeta()
	t := styles.GetTheme()
	right := ""
	if count != "" {
		right = t.S().Base.Foreground(t.SubtleText).Render(count)
	}
	return renderPanelFrame(panelFrameOpts{
		Title:  "stages",
		Note:   note,
		Right:  right,
		Width:  max(4, m.wWidth),
		Height: m.stageView.Height() + 2,
		Active: m.focusPane == focusBuild && m.activeBorder == borderStages,
	}, m.stageView.View())
}

func (m *Model) renderLogPanel(spinnerView string) string {
	if m.logPane == nil {
		return ""
	}
	return m.logPane.render(
		spinnerView,
		m.focusPane == focusLog,
		m.pipelineRun.ID,
		len(m.pipelineRun.Stages) > 0,
		m.isTerminal(),
	)
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

	m.rebuildStageGraph(m.focusedStageID(), m.userSelectedStage)
	m.ensureFocusedStageVisible()
	m.syncPaneSizes()

	var cmds []tea.Cmd
	if animationCmd != nil {
		cmds = append(cmds, animationCmd)
	}
	if !m.isTerminal() && m.isActive() && len(m.pipelineRun.Stages) > 0 {
		if graphCmd := m.refreshGraphTree(); graphCmd != nil {
			cmds = append(cmds, graphCmd)
		}
	}
	if m.stageDetails == nil {
		return tea.Batch(cmds...)
	}
	cmds = append(cmds, m.getNode())
	return tea.Batch(cmds...)
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

func (m *Model) renderBuildFlow() string {
	t := styles.GetTheme()
	run := m.pipelineRun

	switch {
	case len(run.Stages) > 0:
		return m.renderStages()
	case m.isTerminal():
		// wfapi reports the stage graph, not the build result. A run that never
		// reaches a stage (e.g. a Jenkinsfile parse error) has no graph to report
		// on, so it shows up here with zero stages regardless of pass/fail.
		return m.renderNoStageData()
	default:
		return t.S().Base.Foreground(t.Grey).
			Bold(true).
			Italic(true).
			Render("Build in queue...")
	}
}

// renderNoStageData renders a placeholder for a run with no stage graph,
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
	return m.renderTimeline()
}

func (m *Model) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	if m.loading && (m.logPane == nil || !m.logPane.zoomed) {
		return nil
	}

	// While the inline log viewer is in search input or visual selection mode,
	// route every keypress directly to it without intercepting page shortcuts.
	if m.logPane != nil && m.logPane.isCapturingInput() {
		return m.updateLogViewer(msg)
	}

	if key.Matches(msg, m.keyMap.SwitchPane) {
		return m.togglePaneFocus()
	}
	if key.Matches(msg, m.keyMap.ZoomLog) {
		return m.toggleZoomLog()
	}
	if key.Matches(msg, m.keyMap.OpenFullLog) {
		return m.toggleFullLog()
	}
	if key.Matches(msg, m.keyMap.LogScopeFull) {
		return m.setLogScope(logModeFull, false)
	}
	if key.Matches(msg, m.keyMap.LogScopeStage) {
		return m.setLogScope(logModeStage, false)
	}
	if key.Matches(msg, m.keyMap.LogScopeStep) {
		return m.setLogScope(logModeStep, false)
	}
	if key.Matches(msg, m.keyMap.LogScopeOutside) {
		return m.setLogScope(logModeOutside, false)
	}

	if m.focusPane == focusLog {
		return m.handleKeyPressMsgLogPane(msg)
	}
	return m.handleKeyPressMsgBuildPane(msg)
}

func (m *Model) togglePaneFocus() tea.Cmd {
	if m.logPane != nil && m.logPane.zoomed {
		m.logPane.zoomed = false
		m.focusPane = focusBuild
		m.activeBorder = borderSteps
		m.syncPaneFocus()
		m.syncPaneSizes()
		return nil
	}
	if m.focusPane == focusLog {
		m.focusPane = focusBuild
		m.activeBorder = borderSteps
	} else {
		m.focusPane = focusLog
		m.activeBorder = borderLog
	}
	m.syncPaneFocus()
	m.syncPaneSizes()
	return nil
}

func (m *Model) toggleZoomLog() tea.Cmd {
	if m.logPane == nil {
		return nil
	}
	m.logPane.zoomed = !m.logPane.zoomed
	if m.logPane.zoomed {
		m.focusPane = focusLog
		m.activeBorder = borderLog
	}
	m.syncPaneFocus()
	m.syncPaneSizes()
	return nil
}

func (m *Model) toggleFullLog() tea.Cmd {
	if m.logPane == nil {
		return nil
	}
	if m.logPane.mode == logModeFull {
		if m.hasSelectedStep() && !m.stageDetailsStale() {
			return m.setLogScope(logModeStep, false)
		}
		if _, ok := m.focusedNavStage(); ok {
			return m.setLogScope(logModeStage, false)
		}
	}
	m.focusPane = focusLog
	m.activeBorder = borderLog
	m.syncPaneFocus()
	m.syncPaneSizes()
	return m.setLogScope(logModeFull, false)
}

func (m *Model) handleKeyPressMsgLogPane(msg tea.KeyPressMsg) tea.Cmd {
	if key.Matches(msg, m.keyMap.FocusBuild) {
		if m.logPane != nil {
			m.logPane.zoomed = false
		}
		m.focusPane = focusBuild
		m.activeBorder = borderSteps
		m.syncPaneFocus()
		m.syncPaneSizes()
		return nil
	}

	if msg.String() == "esc" {
		if m.logPane != nil && m.logPane.zoomed {
			m.logPane.zoomed = false
			m.syncPaneSizes()
			return nil
		}
		m.focusPane = focusBuild
		m.activeBorder = borderSteps
		m.syncPaneFocus()
		m.syncPaneSizes()
		return nil
	}

	if key.Matches(msg, m.keyMap.OpenStepLog) {
		return m.applyCurrentLogScope(true)
	}

	// Allow non-colliding build actions while viewing the log.
	if cmd, handled := m.handleSharedBuildActions(msg); handled {
		return cmd
	}

	return m.updateLogViewer(msg)
}

func (m *Model) handleKeyPressMsgBuildPane(msg tea.KeyPressMsg) tea.Cmd {
	// Quick ergonomic bridges from Steps/Stages straight into Log actions:
	// '/' opens log search, 'w' toggles wrap.
	if m.logPane != nil && m.logPane.viewer != nil {
		if msg.String() == "/" {
			m.focusPane = focusLog
			m.activeBorder = borderLog
			m.syncPaneFocus()
			m.syncPaneSizes()
			return m.logPane.viewer.FocusSearch()
		}
		if msg.String() == "w" {
			m.logPane.viewer.ToggleWrap()
			return nil
		}
	}

	var cmds []tea.Cmd
	prevStageID := m.focusedStageID()
	cmds = append(cmds, m.handleKeyPressMsgStageView(msg))
	stageChanged := m.focusedStageID() != prevStageID

	prevStepID := ""
	if step, ok := m.selectedStep(); ok {
		prevStepID = step.ID
	}

	if m.stageDetailsStale() {
		cmds = append(cmds, m.handleKeyPressMsgStaleDetails(msg))
	} else {
		if key.Matches(msg, m.keyMap.Up) || key.Matches(msg, m.keyMap.Down) {
			m.activeBorder = borderSteps
			m.syncPaneFocus()
		}
		cmds = append(cmds, m.handleKeyPressMsgDetailsView(msg))
	}

	if m.logPane != nil {
		if stageChanged {
			// Immediately jump in full-log mode or re-scope stage mode without
			// waiting for wfapi stage details.
			switch m.logPane.mode {
			case logModeFull:
				if cmd := m.jumpFullLogToCurrentSelection(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			case logModeStage:
				if cmd := m.applyCurrentLogScope(false); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		} else if !m.stageDetailsStale() {
			if step, ok := m.selectedStep(); ok && step.ID != "" && step.ID != prevStepID {
				switch m.logPane.mode {
				case logModeFull:
					if cmd := m.jumpFullLogToCurrentSelection(); cmd != nil {
						cmds = append(cmds, cmd)
					}
				case logModeStep:
					if cmd := m.applyCurrentLogScope(false); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			}
		}
	}

	if !key.Matches(msg, m.keyMap.Up) && !key.Matches(msg, m.keyMap.Down) && m.stageView != nil {
		stage, cmd := m.stageView.Update(msg)
		m.stageView = &stage
		cmds = append(cmds, cmd)
	}
	return tea.Batch(cmds...)
}

func (m *Model) handleSharedBuildActions(msg tea.KeyPressMsg) (tea.Cmd, bool) {
	if m.build == nil {
		return nil, false
	}
	switch {
	case key.Matches(msg, m.keyMap.OpenJob):
		return buildaction.OpenJob(m.pipelineRun.Job), true
	case key.Matches(msg, m.keyMap.Replay):
		return util.CmdHandler(ReplayMsg{
			Job:         m.pipelineRun.Job,
			PipelineRun: m.pipelineRun,
		}), true
	case key.Matches(msg, m.keyMap.TriggerBuild):
		return m.paramsRequest.Fetch(m.pipelineRun.Job), true
	case key.Matches(msg, m.keyMap.CancelBuild):
		return buildaction.ConfirmAbort(m.ctx, m.pipelineRun.Job, m.pipelineRun.ID, m.isTerminal()), true
	case key.Matches(msg, m.keyMap.OpenBuildBrowser):
		return buildaction.OpenBuildInBrowser(m.pipelineRun.Job.Raw.URL, m.pipelineRun.ID), true
	case key.Matches(msg, m.keyMap.OpenJobBrowser):
		return buildaction.OpenJobInBrowser(m.pipelineRun.Job.Raw.URL), true
	case key.Matches(msg, m.keyMap.DownloadAllArtifacts):
		return m.startArtifactDownload(m.build.GetArtifacts()), true
	case key.Matches(msg, m.keyMap.DownloadArtifacts):
		artifacts := m.build.GetArtifacts()
		if len(artifacts) <= 1 {
			return m.startArtifactDownload(artifacts), true
		}
		dialog := artifactselector.New(
			artifacts,
			m.wWidth,
			m.wHeight,
			m.startArtifactDownload,
			m.ctx.Config.KeyMapConfig.Dialog.ArtifactSelector,
		)
		return util.CmdHandler(util.OpenDialogMsg{Dialog: dialog}), true
	}
	return nil, false
}

func (m *Model) handleKeyPressMsgStageView(msg tea.KeyPressMsg) tea.Cmd {
	if m.build == nil {
		return nil
	}

	if cmd, handled := m.handleSharedBuildActions(msg); handled {
		return cmd
	}

	if key.Matches(msg, m.keyMap.ShowParams) {
		return m.fetchBuildParams()
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

	navStages := m.navigableStages()
	if len(navStages) > 0 {
		if key.Matches(msg, m.keyMap.NextStage) {
			m.focusedStageIdx = (m.focusedStageIdx + 1) % len(navStages)
			m.userSelectedStage = true
			m.activeBorder = borderStages
			m.syncPaneFocus()
			if m.stageDetails != nil {
				m.stageDetails.init()
			}
			m.ensureFocusedStageVisible()
			return m.getNode()
		}
		if key.Matches(msg, m.keyMap.PrevStage) {
			m.focusedStageIdx--
			if m.focusedStageIdx < 0 {
				m.focusedStageIdx = len(navStages) - 1
			}
			m.userSelectedStage = true
			m.activeBorder = borderStages
			m.syncPaneFocus()
			if m.stageDetails != nil {
				m.stageDetails.init()
			}
			m.ensureFocusedStageVisible()
			return m.getNode()
		}
	}
	return nil
}

// stageDetailsStale reports whether the details pane still shows a stage other
// than the highlighted one: switching stages moves the highlight at once, but
// the new stage's details arrive asynchronously.
func (m *Model) stageDetailsStale() bool {
	if m.stageDetails == nil || m.stageDetails.pipelineNode == nil {
		return false
	}
	stage, ok := m.focusedNavStage()
	if !ok {
		return false
	}
	return m.stageDetails.pipelineNode.ID != stage.ID
}

// handleKeyPressMsgStaleDetails withholds keys from a details pane that still
// shows the previous stage, so they cannot act on that stage's steps. With no
// fetch in flight the last load failed, so the step-log key retries it.
func (m *Model) handleKeyPressMsgStaleDetails(msg tea.KeyPressMsg) tea.Cmd {
	if !key.Matches(msg, m.keyMap.OpenStepLog) {
		return nil
	}
	stage, ok := m.focusedNavStage()
	if !ok {
		return nil
	}
	notice := util.CmdHandler(util.FooterNotiMsg{
		Message: fmt.Sprintf("Loading %s stage…", stage.Name),
	})
	if m.cancelNodeRequest != nil {
		return notice
	}
	return tea.Batch(notice, m.getNode())
}

func (m *Model) handleKeyPressMsgDetailsView(msg tea.KeyPressMsg) tea.Cmd {
	if m.stageDetails == nil {
		return nil
	}
	stageDetails, cmd := m.stageDetails.Update(msg)
	if sd, ok := stageDetails.(*StageDetails); ok {
		m.stageDetails = sd
	}
	return cmd
}

func (m *Model) handleFetchNodeLog(msg FetchNodeLogMsg) tea.Cmd {
	if m.logPane == nil {
		return nil
	}
	m.focusPane = focusLog
	m.activeBorder = borderLog
	m.syncPaneFocus()
	m.syncPaneSizes()

	// When in full-log mode and the step is indexed, jump straight to it;
	// otherwise show the step's scoped log.
	if m.logPane.mode == logModeFull && m.logPane.fullLogIndex != nil {
		if line, ok := m.logPane.fullLogIndex.JumpLineForStep(msg.NodeID); ok && m.logPane.viewer != nil && m.logPane.viewer.TotalLines() > 0 {
			return tea.Batch(m.logPane.viewer.JumpToLine(line), m.UpdateFooter())
		}
	}

	m.logPane.mode = logModeStep
	stageName := ""
	if m.stageDetails != nil {
		stageName = m.stageDetails.stageName()
	}
	if step, ok := m.findStepByID(msg.NodeID); ok {
		if m.logPane.fullLogPath != "" && m.logPane.fullLogIndex != nil && m.logPane.fullLogIndex.HasNodeTags {
			if len(m.logPane.fullLogIndex.StepRanges[step.ID]) > 0 {
				return tea.Batch(m.extractScopedView(logModeStep, step.ID), m.UpdateFooter())
			}
		}
		force := m.logPane.loadErr != nil || m.logPane.nodeID != step.ID
		return tea.Batch(m.loadStepLog(stageName, step, force), m.UpdateFooter())
	}
	return tea.Batch(m.loadStepLog(stageName, gojenkins.PipelineNode{ID: msg.NodeID}, true), m.UpdateFooter())
}

func (m *Model) selectedStep() (gojenkins.PipelineNode, bool) {
	if m.stageDetails == nil {
		return gojenkins.PipelineNode{}, false
	}
	return m.stageDetails.selectedStep()
}

func (m *Model) hasSelectedStep() bool {
	_, ok := m.selectedStep()
	return ok
}

func (m *Model) findStepByID(id string) (gojenkins.PipelineNode, bool) {
	if m.stageDetails == nil || m.stageDetails.pipelineNode == nil {
		return gojenkins.PipelineNode{}, false
	}
	for _, step := range m.stageDetails.pipelineNode.StageFlowNodes {
		if step.ID == id {
			return step, true
		}
	}
	return gojenkins.PipelineNode{}, false
}

func (m *Model) syncPaneFocus() {
	if m.stageDetails != nil {
		m.stageDetails.isFocused = m.focusPane == focusBuild && m.activeBorder == borderSteps
	}
}

func (m *Model) updateLogViewer(msg tea.Msg) tea.Cmd {
	if m.logPane == nil || m.logPane.viewer == nil {
		return nil
	}
	var cmd tea.Cmd
	m.logPane.viewer, cmd = m.logPane.viewer.Update(msg)
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
		if m.startInFullLog && m.logPane != nil && (m.logPane.viewer != nil || m.logPane.loading) {
			// Full log mode can still show the console log even if pipeline metadata failed.
			m.loading = false
			return nil
		}
		return util.CmdHandler(LoadFailedMsg{Model: m, Generation: msg.Generation, Error: msg.Error, ErrorTitle: msg.ErrorTitle})
	}

	m.pipelineRun = msg.PipelineRun
	m.pluginTree = msg.PluginTree
	m.startedBy = msg.StartedBy
	m.startedByDetails = msg.StartedByDetails
	m.build = &msg.Build

	m.loading = false
	m.userSelectedStage = false
	if msg.StageGraph != nil {
		m.stageGraph = *msg.StageGraph
		m.syncNavStages(msg.FocusedStageID, msg.FocusedStageID != "")
	} else {
		m.rebuildStageGraph(msg.FocusedStageID, msg.FocusedStageID != "")
	}

	var cmds []tea.Cmd
	if msg.Node != nil {
		m.mergeDownloadedNode(*msg.Node)
		cmds = append(cmds, m.updateStageDetails(NodeDownloadedMsg{PipelineNode: *msg.Node}))
	}
	m.syncPaneSizes()
	m.ensureFocusedStageVisible()

	if m.logPane != nil {
		m.syncLogSelectionMetadata()
		if cmd := m.loadFullLog(false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}

	if !m.isTerminal() {
		cmds = append(cmds, m.watchExactRun())
	}
	return tea.Batch(cmds...)
}

func (m *Model) getNode() tea.Cmd {
	m.cancelNodeFetch()
	m.nodeGeneration++
	stage, ok := m.focusedNavStage()
	if !ok || stage.ID == "" {
		return nil
	}
	ctx, cancel := m.ctx.RequestContext()
	m.cancelNodeRequest = cancel
	generation := m.nodeGeneration
	run := m.pipelineRun
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
	var cmds []tea.Cmd
	if cmd := m.updateStageDetails(msg); cmd != nil {
		cmds = append(cmds, cmd)
	}
	m.syncPaneSizes()

	// Sync the inline log pane with the newly loaded stage/step details.
	if m.logPane != nil {
		m.syncLogSelectionMetadata()
		switch m.logPane.mode {
		case logModeFull:
			if cmd := m.jumpFullLogToCurrentSelection(); cmd != nil {
				cmds = append(cmds, cmd)
			}
		case logModeStage:
			if cmd := m.applyCurrentLogScope(false); cmd != nil {
				cmds = append(cmds, cmd)
			}
		case logModeStep:
			if step, ok := m.selectedStep(); ok && step.ID != "" {
				if m.logPane.nodeID != step.ID || (m.logPane.viewer == nil && !m.logPane.loading && !m.logPane.empty) {
					if cmd := m.applyCurrentLogScope(false); cmd != nil {
						cmds = append(cmds, cmd)
					}
				}
			} else {
				m.logPane.cancelInFlight()
				m.logPane.loading = false
				m.logPane.loadErr = nil
				m.logPane.empty = true
				m.logPane.stageName = msg.PipelineNode.Name
				m.logPane.stepName = ""
				m.logPane.nodeID = ""
				if cmd := m.logPane.resetViewer(); cmd != nil {
					cmds = append(cmds, cmd)
				}
			}
		}
	}
	return tea.Batch(cmds...)
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
	stageDetails, cmd := m.stageDetails.Update(msg)
	if sd, ok := stageDetails.(*StageDetails); ok {
		m.stageDetails = sd
	}
	return cmd
}

func (m *Model) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	m.wWidth = msg.Width
	m.wHeight = msg.Height
	if m.stageView != nil {
		m.stageView.SetWidth(m.timelineContentWidth())
	}
	m.syncPaneSizes()
	return nil
}

// bottomPaneWidths computes the responsive horizontal split for the Steps (left)
// and Log (right) framed panels. Ultra-narrow terminals show only the active pane.
func (m *Model) bottomPaneWidths() (stepsW, logW int) {
	if m.logPane != nil && m.logPane.zoomed {
		return 0, max(4, m.wWidth)
	}
	if m.wWidth < narrowSplitThreshold {
		if m.focusPane == focusLog {
			return 0, max(4, m.wWidth)
		}
		return max(4, m.wWidth), 0
	}
	if m.wWidth >= 80 {
		stepsW = clampInt(m.wWidth*30/100, 26, 40)
	} else {
		stepsW = 22
	}
	logW = max(16, m.wWidth-stepsW)
	stepsW = max(4, m.wWidth-logW)
	return stepsW, logW
}

func (m *Model) syncPaneSizes() {
	if m.logPane != nil && m.logPane.zoomed {
		m.logPane.setSize(m.wWidth, m.wHeight)
		return
	}
	bottomH := m.availableBottomHeight()
	stepsW, logW := m.bottomPaneWidths()
	if m.stageDetails != nil && stepsW > 0 {
		_ = m.updateStageDetails(tea.WindowSizeMsg{Width: stepsW, Height: bottomH})
	}
	if m.logPane != nil && logW > 0 {
		m.logPane.setSize(logW, bottomH)
	}
}

func (m *Model) availableBottomHeight() int {
	m.updateStageViewLayout()
	stagesPanelH := 0
	if m.stageView != nil {
		stagesPanelH = m.stageView.Height() + 2
	}
	height := m.wHeight - buildInfoPanelHeight - stagesPanelH
	if height < 4 {
		return 4
	}
	return height
}

func (m *Model) updateStageViewLayout() {
	if m.stageView == nil {
		return
	}
	flow := m.renderBuildFlow()
	m.stageView.SetWidth(m.timelineContentWidth())
	m.stageView.SetHeight(m.timelineViewportHeight(flow))
	y := m.stageView.YOffset()
	m.stageView.SetContent(flow)
	m.stageView.SetYOffset(y)
	m.stageView.Style = lipgloss.NewStyle()
}

func (m *Model) currentStageGraph() stagegraph.Graph {
	if len(m.stageGraph.Nodes) > 0 {
		return m.stageGraph
	}
	if len(m.pipelineRun.Stages) == 0 {
		return stagegraph.Graph{Source: stagegraph.SourceWFAPI}
	}
	return stagegraph.FromWFAPI(m.pipelineRun)
}

func (m *Model) navigableStages() []stagegraph.StageNode {
	if len(m.navStages) > 0 {
		return m.navStages
	}
	if len(m.pipelineRun.Stages) == 0 {
		return nil
	}
	return extractNavigableStages(stagegraph.Flatten(m.currentStageGraph()))
}

func (m *Model) focusedNavStage() (stagegraph.StageNode, bool) {
	nav := m.navigableStages()
	if m.focusedStageIdx < 0 || m.focusedStageIdx >= len(nav) {
		return stagegraph.StageNode{}, false
	}
	return nav[m.focusedStageIdx], true
}

func (m *Model) focusedStageID() string {
	if stage, ok := m.focusedNavStage(); ok {
		return stage.ID
	}
	return ""
}

func (m *Model) rebuildStageGraph(preserveID string, forcePreserve bool) {
	if m.pluginTree != nil {
		m.stageGraph = stagegraph.FromPluginTree(m.pluginTree, m.pipelineRun)
	} else {
		m.stageGraph = stagegraph.FromWFAPI(m.pipelineRun)
	}
	m.syncNavStages(preserveID, forcePreserve)
}

func (m *Model) syncNavStages(preserveID string, forcePreserve bool) {
	rows := stagegraph.Flatten(m.stageGraph)
	m.navStages = extractNavigableStages(rows)
	if len(m.navStages) == 0 {
		m.focusedStageIdx = 0
		return
	}
	if forcePreserve && preserveID != "" {
		if idx := indexOfNavStage(m.navStages, preserveID); idx >= 0 {
			m.focusedStageIdx = idx
			return
		}
	}
	if !m.userSelectedStage {
		if focusIdx := stagegraph.InitialFocusIndex(rows); focusIdx >= 0 {
			m.focusedStageIdx = focusIdx
			return
		}
	}
	if preserveID != "" {
		if idx := indexOfNavStage(m.navStages, preserveID); idx >= 0 {
			m.focusedStageIdx = idx
			return
		}
	}
	m.focusedStageIdx = clampInt(m.focusedStageIdx, 0, len(m.navStages)-1)
}

func extractNavigableStages(rows []stagegraph.TimelineRow) []stagegraph.StageNode {
	var nav []stagegraph.StageNode
	for i := range rows {
		if rows[i].Navigable {
			nav = append(nav, rows[i].Node)
		}
	}
	return nav
}

func indexOfNavStage(stages []stagegraph.StageNode, id string) int {
	for i := range stages {
		if stages[i].ID == id {
			return i
		}
	}
	return -1
}

func (m *Model) refreshGraphTree() tea.Cmd {
	if m.ctx == nil || m.ctx.Jenkins == nil || m.ctx.Jenkins.Requester == nil || len(m.pipelineRun.Stages) == 0 {
		return nil
	}
	cache := m.ctx.GraphCapabilityCache()
	serverKey := m.ctx.GraphServerKey()
	if cache.Get(serverKey) == stagegraph.CapabilityUnavailable {
		return nil
	}
	m.cancelGraphFetch()
	ctx, cancel := m.ctx.RequestContext()
	m.cancelGraphRequest = cancel
	m.graphGeneration++
	generation := m.graphGeneration
	jenkins := m.ctx.Jenkins
	run := m.pipelineRun
	return func() tea.Msg {
		defer cancel()
		tree, ok, err := stagegraph.FetchTree(ctx, jenkins, cache, serverKey, run)
		if !ok {
			return GraphTreeFetchedMsg{model: m, Generation: generation, Error: err}
		}
		return GraphTreeFetchedMsg{model: m, Generation: generation, Tree: tree}
	}
}

func (m *Model) cancelGraphFetch() {
	if m.cancelGraphRequest != nil {
		m.cancelGraphRequest()
		m.cancelGraphRequest = nil
	}
}

func (m *Model) handleGraphTreeFetched(msg GraphTreeFetchedMsg) tea.Cmd {
	if msg.model != m || msg.Generation != m.graphGeneration {
		return nil
	}
	m.cancelGraphFetch()
	if msg.Error != nil || msg.Tree == nil {
		return nil
	}
	preserveID := m.focusedStageID()
	m.pluginTree = msg.Tree
	m.rebuildStageGraph(preserveID, m.userSelectedStage)
	m.ensureFocusedStageVisible()
	m.syncPaneSizes()
	return nil
}

// shouldAnimate covers the loading spinner, a running build, and an inline log
// fetch. A paused or finished build with an idle/loaded log pane is static, and
// a page outside Init/CleanUp is not on screen, so neither keeps the clock alive.
func (m *Model) shouldAnimate() bool {
	if !m.entered {
		return false
	}
	if m.loading || m.isRunning() {
		return true
	}
	return m.logPane != nil && m.logPane.isAnimating()
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
	if m.loading && (m.logPane == nil || !m.logPane.zoomed) {
		return m.clock.Next()
	}
	m.pulseBar.Step()
	if m.stageDetails != nil {
		m.stageDetails.advanceAnimation()
	}
	return m.clock.Next()
}

// hasRunningStage reports whether any stage in the timeline is actively
// executing and animates its bar.
func (m *Model) hasRunningStage() bool {
	if len(m.stageGraph.Nodes) > 0 {
		return m.stageGraph.HasRunningStage()
	}
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

// Ensure logviewer stays referenced for embedded help composition.
var _ *logviewer.Model
