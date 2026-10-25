package job

import (
	cc "context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/buildaction"
	"github.com/atm1020/jenktui/src/ui/components/build"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"github.com/atm1020/gojenkins"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
)

type ActiveView int

const (
	StageView ActiveView = iota
	JobListView
)

type Model struct {
	activeView        ActiveView
	wWidth, wHeight   int
	ctx               *context.ProgramContext
	keyMap            *CommonKeyMap
	job               *gojenkins.Job
	jobUrl            string
	loading           bool
	loadingSpinner    spinner.Model
	pipelineRuns      []gojenkins.PipelineRun
	isBuildInProgress bool
	isFavorite        bool
	stageView         *StageViewModel
	jobList           *BuildListModel
	frame             pageFrame
	spinnerModel      spinner.Model
	animating         bool
	clock             animation.FrameClock
	// entered is true between Init and CleanUp. Only an entered page is on
	// screen, so late messages cannot start the clock on any other.
	entered           bool
	pollWatch         *poll.Watch
	requestGeneration int
	cancelRequest     cc.CancelFunc
	paramsRequest     buildaction.ParamsRequest
}

func NewModel(ctx *context.ProgramContext, width, height int, job *gojenkins.Job) *Model {
	_, stageViewWidth := calcWidths(width)
	t := styles.GetTheme()
	return &Model{
		ctx:        ctx,
		wWidth:     width,
		wHeight:    height,
		job:        job,
		stageView:  NewStageViewModel(stageViewWidth, height-1, UserStageViewKeyMap(util.UserKeyMap(ctx.Config.KeyMapConfig), ctx.Config.KeyMapConfig.Job)),
		jobList:    NewJobListModel(UserBuildListViewKeyMap(util.UserKeyMap(ctx.Config.KeyMapConfig), ctx.Config.KeyMapConfig.Job)),
		activeView: StageView,
		keyMap:     UserCommonKeyMap(ctx.Config.KeyMapConfig.Job),
		jobUrl:     jobref.ToAbsolute(ctx.Jenkins.Server, job.Raw.URL),
		loading:    true,
		loadingSpinner: spinner.New(
			spinner.WithSpinner(spinner.Dot),
			spinner.WithStyle(t.S().Spinner),
		),
		spinnerModel: spinner.New(
			spinner.WithSpinner(spinner.Dot),
			spinner.WithStyle(t.S().Spinner),
		),
	}
}

func NewModelFromUrl(ctx *context.ProgramContext, width, height int, url string) *Model {
	_, stageViewWidth := calcWidths(width)
	t := styles.GetTheme()
	return &Model{
		ctx:     ctx,
		wWidth:  width,
		wHeight: height,
		jobUrl:  jobref.ToAbsolute(ctx.Jenkins.Server, url),
		loading: true,
		loadingSpinner: spinner.New(
			spinner.WithSpinner(spinner.Dot),
			spinner.WithStyle(t.S().Spinner),
		),
		stageView:  NewStageViewModel(stageViewWidth, height-1, UserStageViewKeyMap(util.UserKeyMap(ctx.Config.KeyMapConfig), ctx.Config.KeyMapConfig.Job)),
		jobList:    NewJobListModel(UserBuildListViewKeyMap(util.UserKeyMap(ctx.Config.KeyMapConfig), ctx.Config.KeyMapConfig.Job)),
		activeView: StageView,
		keyMap:     UserCommonKeyMap(ctx.Config.KeyMapConfig.Job),
		spinnerModel: spinner.New(
			spinner.WithSpinner(spinner.Dot),
			spinner.WithStyle(t.S().Spinner),
		),
	}
}

func (m *Model) Init() tea.Cmd {
	m.loading = true
	m.entered = true
	return tea.Batch(
		m.fetchJob(),
		m.watchJobWindow(),
		m.ensureAnimation(),
	)
}

// Update ends by syncing the animation clock, so any message that starts or
// ends running work also starts or stops the page animation. Crush's UI does
// the same with EnsureAnimating; see animation.FrameClock.Ensure.
func (m *Model) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	return m, tea.Batch(m.handleMsg(msg), m.ensureAnimation())
}

func (m *Model) View() string {
	if m.loading && m.job == nil {
		return m.renderLoading()
	}
	if page, ok := m.renderCachedJobPage(); ok {
		return page
	}
	return m.renderJobPageUncached()
}

func (m *Model) ID() string {
	id := "job:" + jobref.ToDisplayName(m.jobUrl)
	log.Debug("job: ID()", "jobUrl", m.jobUrl, "id", id)
	return id
}

func (m *Model) UpdateFooter() tea.Cmd {
	if m.job == nil {
		return util.CmdFooterUrl(m.jobUrl)
	}
	return util.CmdFooterUrl(m.job.GetDetails().URL)
}

func (m *Model) CleanUp() tea.Cmd {
	m.frame = pageFrame{}
	m.cancelInFlightRequest()
	m.paramsRequest.Invalidate()
	m.entered = false
	m.stopAnimation()
	if m.stageView != nil {
		m.stageView.hasInProgress = false
		if m.stageView.pulseBar != nil {
			m.stageView.pulseBar.Stop()
		}
	}
	if m.pollWatch != nil {
		m.pollWatch.Release()
		m.pollWatch = nil
		log.Debug("job: CleanUp released poll watch", "jobID", m.ID())
	}
	return nil
}

func (m *Model) KeyMap() help.KeyMap {
	if m.activeView == StageView {
		return m.stageView.keyMap
	}
	return m.jobList.keyMap
}

func (m *Model) handleMsg(msg tea.Msg) tea.Cmd {
	switch msg := msg.(type) {
	case JobLoadedMsg:
		return m.handleJobLoadedMsg(msg)
	case tea.WindowSizeMsg:
		return m.onWindowSizeChanged(msg)
	case tea.KeyPressMsg:
		return m.handleKeyPressMsg(msg)
	case animation.FrameMsg:
		return m.handleAnimationFrame(msg)
	case buildaction.ParamsFetchedMsg:
		return m.paramsRequest.Handle(m.ctx, m.wWidth, msg)
	case FavoriteToggledMsg:
		return m.handleFavoriteToggled(msg)
	}

	return tea.Batch(m.updateViews(msg)...)
}

// RunHistoryQuery returns the My Runs filter for this job. The app shell uses
// it when the global My Runs key is pressed while this view is active.
func (m *Model) RunHistoryQuery() store.RunQuery {
	return store.RunQuery{
		JobName:   jobref.ToDisplayName(m.jobURL()),
		SortBy:    store.RunSortDate,
		Direction: store.SortDescending,
	}
}

// IsCapturingInput reports whether the build list's filter is capturing a query, so
// the app shell withholds its global letter shortcuts rather than firing them into
// the filter.
func (m *Model) IsCapturingInput() bool {
	return m.activeView == JobListView && m.jobList != nil && m.jobList.IsFiltering()
}

func (m *Model) handleJobLoadedMsg(msg JobLoadedMsg) tea.Cmd {
	if msg.Generation != m.requestGeneration {
		log.Debug("job: dropping stale load result", "resultGeneration", msg.Generation, "pageGeneration", m.requestGeneration)
		return nil
	}
	// The request has completed. Calling its CancelFunc now releases the
	// deadline timer and clears the cleanup hook; it does not discard the result.
	m.cancelInFlightRequest()
	if msg.Error != nil {
		if errors.Is(msg.Error, cc.Canceled) {
			log.Debug("job: request cancelled", "jobURL", m.jobUrl)
			return nil
		}
		return util.CmdHandler(util.ErrorMsg{Error: msg.Error, Title: msg.ErrorTitle})
	}
	if msg.RedirectTarget != nil {
		return util.CmdHandler(util.OpenJobFolderMsg{Target: *msg.RedirectTarget, Job: msg.Job})
	}

	m.job = msg.Job
	m.stageView.SetPipelineRun(msg.PipelineRuns)
	m.jobList.setItems(m.job.GetDetails().Builds)
	m.isBuildInProgress = msg.HasInProgressBuild
	m.stageView.hasInProgress = msg.HasInProgressBuild
	m.loading = false
	m.isFavorite = msg.IsFavorite
	return nil
}

func (m *Model) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	if m.loading {
		return nil
	}

	// While the build list is filtering, hand every key to it (typing the query,
	// enter to apply, esc to cancel) instead of matching the job view's letter
	// shortcuts.
	if m.activeView == JobListView && m.jobList.IsFiltering() {
		return tea.Batch(m.updateViews(msg)...)
	}

	if key.Matches(msg, m.keyMap.Replay) {
		return m.repley()
	}

	if key.Matches(msg, m.keyMap.Logs) {
		return m.openLogs()
	}

	if key.Matches(msg, m.keyMap.CancelBuild) {
		return m.handleCancelBuild()
	}

	if key.Matches(msg, m.keyMap.TriggerBuild) {
		return m.handleTriggerBuild()
	}

	if key.Matches(msg, m.keyMap.Favorite) {
		return m.toggleFavorite()
	}

	if key.Matches(msg, m.keyMap.BuildsView) {
		m.activeView = JobListView
	}

	if key.Matches(msg, m.keyMap.StagesView) {
		m.activeView = StageView
	}

	if key.Matches(msg, m.keyMap.OpenJobBrowser) {
		return buildaction.OpenJobInBrowser(m.job.Raw.URL)
	}

	if key.Matches(msg, m.keyMap.OpenBuildBrowser) {
		return buildaction.OpenBuildInBrowser(m.job.Raw.URL, m.stageView.GetCurrentSelectedRun().ID)
	}
	if key.Matches(msg, m.keyMap.OpenBuildView) {
		return m.getBuild()
	}

	return tea.Batch(m.updateViews(msg)...)
}

func (m *Model) updateStageView(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	m.stageView, cmd = m.stageView.Update(msg)
	return cmd
}

func (m *Model) fetchJob() tea.Cmd {
	m.cancelInFlightRequest()
	requestCtx, cancel := m.ctx.RequestContext()
	m.cancelRequest = cancel
	m.requestGeneration++
	generation := m.requestGeneration
	jobURL := m.jobUrl
	jenkins := m.ctx.Jenkins
	runStore := m.ctx.RunStore

	return func() tea.Msg {
		id, parentIds := jobref.ToIDAndParents(jobURL)
		j, err := jenkins.GetJob(requestCtx, id, parentIds...)
		if err != nil {
			return JobLoadedMsg{Generation: generation, Error: err, ErrorTitle: "Could not open job — it may have been deleted"}
		}
		if target := util.TargetFromJob(jenkins, j); target.Kind.IsContainer() {
			return JobLoadedMsg{Generation: generation, Job: j, RedirectTarget: &target}
		}
		runs, err := j.GetPipelineRuns(requestCtx)
		if err != nil {
			return JobLoadedMsg{Generation: generation, Error: err, ErrorTitle: fmt.Sprintf("Could not load pipeline runs: %s", id)}
		}
		isFavorite := false
		if runStore != nil {
			isFavorite, err = runStore.IsFavorite(j.GetDetails().URL)
			if err != nil {
				log.Warn("Failed to check favorite status", "url", j.GetDetails().URL, "error", err)
			}
		}
		return JobLoadedMsg{
			Generation:         generation,
			Job:                j,
			PipelineRuns:       runs,
			HasInProgressBuild: m.hasInProgressRun(runs),
			IsFavorite:         isFavorite,
		}
	}
}

func (m *Model) cancelInFlightRequest() {
	if m.cancelRequest != nil {
		m.cancelRequest()
		m.cancelRequest = nil
	}
}

// renderJobPageUncached is the pre-cache composition. It remains the fallback
// when a section is not a fixed size the frame cache can wrap, and the oracle
// tests compare against it.
func (m *Model) renderJobPageUncached() string {
	jobListWidth, stageViewWidth := calcWidths(m.wWidth)

	healthStatus := m.renderHealthStatus()
	healthStatusHeight := lipgloss.Height(healthStatus)
	panelHeight := m.wHeight - panelGutter - healthStatusHeight
	jobListContentWidth := jobListWidth - (2 * panelGutter)
	stageViewContentWidth := stageViewWidth - (2 * panelGutter)

	t := styles.GetTheme()
	var stageView, jobList string
	if m.loading {
		stageView = m.renderPanelLoading(stageViewContentWidth, panelHeight)
		jobList = m.renderPanelLoading(jobListContentWidth, panelHeight)
	} else {
		stageView = m.stageView.View(stageViewContentWidth, panelHeight)
		jobList = m.jobList.View(jobListContentWidth, panelHeight)
	}

	// The two panels sit flush, so the stage view drops its left border: the job
	// list's right border becomes the single divider between them, instead of two
	// adjacent borders reading as a "||" gutter.
	if m.activeView == JobListView {
		jobList = t.S().SelectedBorder.Render(jobList)
		stageView = t.S().DefaultBorder.BorderLeft(false).Render(stageView)
	} else {
		stageView = t.S().SelectedBorder.BorderLeft(false).Render(stageView)
		jobList = t.S().DefaultBorder.Render(jobList)
	}

	panels := lipgloss.JoinHorizontal(
		lipgloss.Left,
		jobList,
		stageView,
	)
	body := lipgloss.JoinVertical(
		lipgloss.Left,
		healthStatus,
		panels,
	)

	return lipgloss.Place(
		m.wWidth,
		m.wHeight,
		lipgloss.Top,
		lipgloss.Left,
		body,
	)
}

func (m *Model) renderLoading() string {
	base := styles.GetTheme().S().Base
	content := base.Render(fmt.Sprintf("%s Loading job...", m.loadingSpinner.View()))
	return lipgloss.Place(m.wWidth, m.wHeight, lipgloss.Center, lipgloss.Center, content, lipgloss.WithWhitespaceStyle(base))
}

func (m *Model) renderPanelLoading(width, height int) string {
	base := styles.GetTheme().S().Base
	content := base.Render(fmt.Sprintf("%s Loading...", m.loadingSpinner.View()))
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, content)
}

// watchJobWindow acquires this view's polling demand and applies cached state.
func (m *Model) watchJobWindow() tea.Cmd {
	if m.pollWatch != nil || m.ctx == nil || m.ctx.PollManager == nil {
		return nil
	}
	target := poll.Target{Kind: poll.TargetJobWindow, JobURL: m.jobURL()}
	watch, replay, err := m.ctx.PollManager.Watch(poll.WatchSpec{Target: target})
	if err != nil {
		log.Error("job: failed to watch poll target", "err", err, "jobURL", m.jobURL())
		return nil
	}
	m.pollWatch = watch
	if replay.OK {
		return m.ApplyPollSnapshot(replay.Snapshot)
	}
	return nil
}

// ApplyPollSnapshot applies typed state routed by the app shell.
func (m *Model) ApplyPollSnapshot(snapshot poll.Snapshot) tea.Cmd {
	if m.pollWatch == nil || !m.pollWatch.Matches(snapshot) {
		return nil
	}
	return m.applySnapshotData(
		m.pipelineRunsFromSnapshots(snapshot.Runs),
		hasActiveRun(snapshot.Runs),
	)
}

func hasActiveRun(runs []poll.RunSnapshot) bool {
	for _, run := range runs {
		if run.Activity.IsActive() {
			return true
		}
	}
	return false
}

func (m *Model) pipelineRunsFromSnapshots(snapshots []poll.RunSnapshot) []gojenkins.PipelineRun {
	runs := make([]gojenkins.PipelineRun, len(snapshots))
	for i := range snapshots {
		snapshot := &snapshots[i]
		buildID := snapshot.BuildNumber.BuildID()
		run := gojenkins.PipelineRun{
			ID:        buildID,
			Name:      snapshot.Name,
			Status:    string(snapshot.Status),
			StartTime: snapshot.StartTimeMillis,
			EndTime:   snapshot.EndTimeMillis,
			Duration:  snapshot.DurationMillis,
		}
		if m.job != nil {
			run.Job = m.job
			run.Base = m.job.Base + "/" + buildID
		}

		run.Stages = make([]gojenkins.PipelineNode, len(snapshot.Stages))
		for j := range snapshot.Stages {
			stageSnapshot := &snapshot.Stages[j]
			run.Stages[j] = gojenkins.PipelineNode{
				ID:        stageSnapshot.ID,
				Name:      stageSnapshot.Name,
				Status:    string(stageSnapshot.Status),
				ExecNode:  stageSnapshot.ExecNode,
				StartTime: stageSnapshot.StartTimeMillis,
				Duration:  stageSnapshot.DurationMillis,
			}
		}
		runs[i] = run
	}
	return runs
}

func (m *Model) applySnapshotData(runs []gojenkins.PipelineRun, hasInProgress bool) tea.Cmd {
	m.stageView.SetPipelineRun(runs)

	if m.job != nil {
		m.jobList.setItems(m.job.GetDetails().Builds)
	}
	m.isBuildInProgress = hasInProgress
	m.stageView.hasInProgress = hasInProgress
	return m.ensureAnimation()
}

// jobURL returns the canonical URL for this job, regardless of how the model
// was constructed (NewModel with *Job or NewModelFromUrl with raw URL string).
func (m *Model) jobURL() string {
	if m.job != nil {
		return m.job.GetDetails().URL
	}
	return m.jobUrl
}

func (m *Model) hasInProgressRun(runs []gojenkins.PipelineRun) bool {
	for _, run := range runs {
		// A queued build reports run-level IN_PROGRESS with no stage graph
		// yet, so the run status itself must count, not just stage statuses.
		runStatus := buildstatus.PipelineStatus(run.Status).
			NormalizeQueueState(len(run.Stages), run.EndTime)
		if runStatus.IsActive(run.EndTime) {
			return true
		}
		for _, stage := range run.Stages {
			if buildstatus.PipelineStatus(stage.Status).IsActive(0) {
				return true
			}
		}
	}
	return false
}

func (m *Model) openLogs() tea.Cmd {
	return buildaction.OpenLog(m.stageView.GetCurrentSelectedRun())
}

func (m *Model) repley() tea.Cmd {
	return util.CmdHandler(OpenScriptViewMsg{
		Job:         m.job,
		PipelineRun: m.stageView.GetCurrentSelectedRun(),
	})
}

func (m *Model) handleCancelBuild() tea.Cmd {
	if m.stageView == nil || len(m.stageView.pipelineRuns) == 0 {
		return util.CmdHandler(util.FooterNotiMsg{
			Message: "No build available to cancel",
			Type:    util.NotiWarning,
		})
	}
	run := m.stageView.GetCurrentSelectedRun()
	finished := buildstatus.PipelineStatus(run.Status).IsTerminal(run.EndTime)
	return buildaction.ConfirmAbort(m.ctx, run.Job, run.ID, finished)
}

func (m *Model) handleTriggerBuild() tea.Cmd {
	return m.paramsRequest.Fetch(m.ctx, m.job)
}

func (m *Model) toggleFavorite() tea.Cmd {
	url := m.jobURL()
	name := jobref.ToDisplayName(url)
	wasFavorite := m.isFavorite
	return func() tea.Msg {
		var err error
		if wasFavorite {
			err = m.ctx.RunStore.RemoveFavorite(url)
		} else {
			err = m.ctx.RunStore.AddFavorite(store.FavoriteRecord{
				URL:       url,
				Name:      name,
				Kind:      "job",
				CreatedAt: time.Now().UnixMilli(),
			})
		}
		if err != nil {
			return util.FooterNotiMsg{
				Message: fmt.Sprintf("Failed to update favorite: %s", err),
				Type:    util.NotiError,
			}
		}
		return FavoriteToggledMsg{Favorited: !wasFavorite}
	}
}

func (m *Model) handleFavoriteToggled(msg FavoriteToggledMsg) tea.Cmd {
	m.isFavorite = msg.Favorited
	message := "Removed from favorites"
	if msg.Favorited {
		message = "Added to favorites"
	}
	return util.CmdHandler(util.FooterNotiMsg{Message: message})
}

func (m *Model) getBuild() tea.Cmd {
	run, ok := m.selectedRunForBuildView()
	if !ok {
		return nil
	}
	return util.CmdHandler(build.OpenBuildMsg{PipelineRun: run})
}

func (m *Model) selectedRunForBuildView() (gojenkins.PipelineRun, bool) {
	if m.activeView != JobListView {
		return m.stageView.GetCurrentSelectedRun(), true
	}

	item, ok := m.jobList.selectedItem()
	if !ok {
		return gojenkins.PipelineRun{}, false
	}
	buildID := strings.TrimPrefix(item.BuildId, "#")
	for _, run := range m.stageView.pipelineRuns {
		if run != nil && run.ID == buildID {
			return *run, true
		}
	}

	return gojenkins.PipelineRun{
		ID:   buildID,
		Job:  m.job,
		Base: strings.TrimSuffix(item.Url, "/"),
	}, true
}

func (m *Model) updateViews(msg tea.Msg) []tea.Cmd {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	if m.activeView == StageView {
		m.stageView, cmd = m.stageView.Update(msg)
		cmds = append(cmds, cmd)
	}
	if m.activeView == JobListView {
		m.jobList, cmd = m.jobList.Update(msg)
		cmds = append(cmds, cmd)
	}
	return cmds
}

func (m *Model) renderHealthStatus() string {
	renderWitdhStyle := lipgloss.NewStyle().Foreground(lipgloss.White).Padding(1, 2).Render

	if len(m.job.GetDetails().HealthReport) == 0 {
		return renderWitdhStyle("No health report available")
	}

	h := m.job.GetDetails().HealthReport[0]
	var healthIcon string
	switch h.Score {
	case 100:
		healthIcon = styles.GetTheme().S().StyledIcons.Sunny
	case 80:
		healthIcon = styles.GetTheme().S().StyledIcons.OneCloud
	case 60:
		healthIcon = styles.GetTheme().S().StyledIcons.Cloudy
	case 40:
		healthIcon = styles.GetTheme().S().StyledIcons.ThunderStorm
	default:
		healthIcon = styles.GetTheme().S().StyledIcons.Thunder
	}

	t := styles.GetTheme()

	var jobIcon string
	switch buildstatus.JobColor(m.job.GetDetails().Color).Outcome() {
	case buildstatus.OutcomeSuccess:
		jobIcon = t.S().StyledIcons.Success
	case buildstatus.OutcomeFailure:
		jobIcon = t.S().StyledIcons.Failure
	case buildstatus.OutcomeUnstable:
		jobIcon = t.S().StyledIcons.Pending
	}

	var spinner string
	if m.animating {
		spinner = m.spinnerModel.View()
	}

	title := t.S().Title.Render(m.job.GetName())
	if m.isFavorite {
		title = t.S().StyledIcons.BookMark + title
	}

	data := lipgloss.JoinVertical(
		lipgloss.Left,
		fmt.Sprintf("%s %s  %s", spinner, jobIcon, title),
		"",
		fmt.Sprintf("Health Score: %d%% %s", h.Score, healthIcon),
		"",
		h.Description,
	)

	return renderWitdhStyle(data)
}

func (m *Model) onWindowSizeChanged(msg tea.WindowSizeMsg) tea.Cmd {
	var cmds []tea.Cmd
	var cmd tea.Cmd
	m.wWidth = msg.Width
	m.wHeight = msg.Height

	// Resize has always reserved one row rather than panelGutter. Keep that
	// separate from View's layout so child viewport sizes do not change.
	healthStatusHeight := m.currentHealthHeight()
	calcHeight := msg.Height - healthStatusHeight - 1
	jobListWidth, stageViewWidth := calcWidths(msg.Width)

	m.stageView, cmd = m.stageView.Update(tea.WindowSizeMsg{Width: stageViewWidth, Height: calcHeight})
	cmds = append(cmds, cmd)

	m.jobList, cmd = m.jobList.Update(tea.WindowSizeMsg{Width: jobListWidth, Height: calcHeight})
	cmds = append(cmds, cmd)

	return tea.Batch(cmds...)
}

// panelGutter is the space View subtracts before measuring panel content.
// FIXME: calculate it from the border frame instead of assuming 2.
const panelGutter = 2

func calcWidths(full int) (int, int) {
	// The job list holds short build-number chips (e.g. "#1234") in bordered boxes.
	// full/12 squeezed it to ~8 columns on a typical terminal, collapsing the title
	// to "B…" and overflowing the chips past their panel. Give it a legible minimum
	// and a cap so it never crowds the stage view on very wide terminals.
	jobListWidth := min(24, max(16, full/8))
	stageViewWidth := full - jobListWidth
	return jobListWidth, stageViewWidth
}
