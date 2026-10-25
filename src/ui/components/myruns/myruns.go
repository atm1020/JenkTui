package myruns

import (
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/buildaction"
	"github.com/atm1020/jenktui/src/ui/components/build"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/charmbracelet/x/ansi"
)

// filterDebounceDelay bounds how often the inline name/build editor re-runs
// QueryRuns while the user is typing: each keystroke reschedules the tick,
// so only the last keystroke within this window actually queries SQLite.
const filterDebounceDelay = 200 * time.Millisecond

// Fields of the inline filter editor.
const (
	filterFieldName = iota
	filterFieldBuild
	filterFieldStatus
)

type Model struct {
	ctx           *context.ProgramContext
	wWidth        int
	wHeight       int
	keyMap        KeyMap
	runs          []store.RunRecord
	list          *list.Model
	spinnerModel  spinner.Model
	hasInProgress bool
	query         store.RunQuery
	loadReqID     int64
	paramsRequest buildaction.ParamsRequest

	// Inline name/build/status filter editor (opened by Filter, "/"). Sort
	// and direction act immediately; status uses a dropdown in this editor.
	filtering      bool
	nameInput      textinput.Model
	buildInput     textinput.Model
	filterFocus    int
	preFilterQuery store.RunQuery
	filterGen      int64

	// Status options are the fixed set of known Jenkins statuses (plus an
	// "All" entry); they don't depend on what's currently in the store.
	statusOptions []buildstatus.PipelineStatus // empty string is the "All" option
	statusCursor  int
}

func NewModel(ctx *context.ProgramContext, width, height int) *Model {
	t := styles.GetTheme()
	m := &Model{
		ctx:     ctx,
		wWidth:  width,
		wHeight: height,
		keyMap:  UserKeyMap(ctx.Config.KeyMapConfig.MyRuns),
		query:   defaultRunQuery(),
		spinnerModel: spinner.New(
			spinner.WithSpinner(spinner.Spinner{Frames: spinner.Dot.Frames, FPS: animation.FrameInterval}),
			spinner.WithStyle(t.S().Spinner),
		),
	}
	return m
}

// NewModelWithQuery starts My Runs pre-filtered to query instead of the
// default unfiltered, date-descending view. Used by the Favorites page's
// "jump to run history" action.
func NewModelWithQuery(ctx *context.ProgramContext, width, height int, query store.RunQuery) *Model {
	m := NewModel(ctx, width, height)
	m.query = query
	return m
}

// defaultRunQuery is the no-filter, newest-first view My Runs opens with.
func defaultRunQuery() store.RunQuery {
	return store.RunQuery{
		SortBy:    store.RunSortDate,
		Direction: store.SortDescending,
	}
}

func (m *Model) Init() tea.Cmd {
	return m.loadRuns()
}

func (m *Model) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m, m.handleWindowSizeMsg(msg)
	case RunsLoadedMsg:
		return m, m.handleRunsLoaded(msg)
	case RunsQueriedMsg:
		return m, m.handleRunsQueried(msg)
	case FilterDebounceMsg:
		return m, m.handleFilterDebounce(msg)
	case buildaction.ParamsFetchedMsg:
		return m, m.paramsRequest.Handle(m.ctx, m.wWidth, msg)
	case RunDeletedMsg:
		return m, m.loadRuns()
	case spinner.TickMsg:
		return m, m.handleSpinnerTick(msg)
	case tea.KeyMsg:
		return m, m.handleKeyPress(msg)
	default:
		return m, m.handleNonKeyMsg(msg)
	}
}

func (m *Model) handleNonKeyMsg(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	if m.filtering {
		cmds = append(cmds, m.updateFocusedFilterAnimation(msg))
	}
	cmds = append(cmds, m.updateList(msg))
	return tea.Batch(cmds...)
}

func (m *Model) View() string {
	t := styles.GetTheme()
	contentWidth := m.contentWidth()
	widths := myRunsColumnWidthsFor(contentWidth)

	if m.list == nil {
		base := t.S().Base
		content := base.Render("Loading runs...")
		return lipgloss.Place(m.wWidth, m.wHeight, lipgloss.Center, lipgloss.Center, content, lipgloss.WithWhitespaceStyle(base))
	}

	countBadge := lipgloss.NewStyle().
		Background(t.Primary).
		Foreground(t.BgBase).
		Padding(0, 1).
		Bold(true).
		Render(fmt.Sprintf("%d runs", len(m.runs)))

	badgeWidth := lipgloss.Width(countBadge)
	titleIcon := t.S().StyledIcons.Success
	if m.hasInProgress {
		titleIcon = m.spinnerModel.View()
	}
	base := t.S().Base
	titleText := fmt.Sprintf("%s  %s", titleIcon, t.S().Title.Render("My Runs"))
	if contentWidth < badgeWidth+lipgloss.Width(titleText)+1 {
		countBadge = ""
		badgeWidth = 0
	}
	titleLeft := base.
		Width(max(1, contentWidth-badgeWidth)).
		Render(titleText)
	title := titleLeft + countBadge

	sep := base.Foreground(t.Border).Render(strings.Repeat("─", contentWidth))

	rows := []string{title, sep}
	switch {
	case m.filtering:
		rows = append(rows, m.renderFilterEditor(t), sep)
	default:
		if summary := m.renderFilterSummary(t, contentWidth); summary != "" {
			rows = append(rows, summary, sep)
		}
	}
	rows = append(rows, m.renderHeader(widths), sep, m.renderListBody(t))

	body := base.Width(contentWidth).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))

	return lipgloss.Place(
		m.wWidth, m.wHeight,
		lipgloss.Left, lipgloss.Top,
		base.Padding(1, listHPad).Render(body),
	)
}

func (m *Model) ID() string {
	return "myruns"
}

func (m *Model) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl("My Runs")
}

func (m *Model) CleanUp() tea.Cmd {
	m.paramsRequest.Invalidate()
	m.hasInProgress = false
	t := styles.GetTheme()
	m.spinnerModel = spinner.New(
		spinner.WithSpinner(spinner.Spinner{Frames: spinner.Dot.Frames, FPS: animation.FrameInterval}),
		spinner.WithStyle(t.S().Spinner),
	)
	return nil
}

func (m *Model) KeyMap() help.KeyMap {
	return m.keyMap
}

// IsCapturingInput reports whether the inline filter editor is open and
// receiving text, so the app shell withholds its global letter shortcuts
// (Back "b", Forward "f", MyRuns "m", Help "?") rather than firing them into
// the field.
func (m *Model) IsCapturingInput() bool {
	return m.filtering
}

// loadRuns issues the one-time initial query on Init.
func (m *Model) loadRuns() tea.Cmd {
	query := m.query
	return func() tea.Msg {
		runs, err := m.ctx.RunStore.QueryRuns(query)
		return RunsLoadedMsg{Runs: runs, Error: err}
	}
}

// beginLoad issues a new request ID for a query reload, invalidating any
// still-in-flight reload's result so overlapping filter/poll-driven reloads
// can't overwrite fresher rows with a stale response that resolves later.
func (m *Model) beginLoad() int64 {
	m.loadReqID++
	return m.loadReqID
}

func (m *Model) isStale(reqID int64) bool {
	return reqID != m.loadReqID
}

// reloadQuery re-runs the active query, used by filter apply and by
// poll-driven updates now that SQLite (not m.runs) is the source of truth
// for what's displayed. preserveSelection is forwarded to RunsQueriedMsg
// as-is; callers should go through reloadQueryPreservingSelection or
// reloadQueryKeepingCursorPosition below rather than passing a bare bool
// here, so the call site reads as a decision rather than a magic value.
func (m *Model) reloadQuery(preserveSelection bool) tea.Cmd {
	reqID := m.beginLoad()
	query := m.query
	return func() tea.Msg {
		runs, err := m.ctx.RunStore.QueryRuns(query)
		return RunsQueriedMsg{Runs: runs, Error: err, ReqID: reqID, PreserveSelection: preserveSelection}
	}
}

// reloadQueryPreservingSelection reloads and tries to keep the same run
// selected by chasing its ID to the new result set. Use this when the
// change narrows/restores rows (name, build, or status filters) or when it
// reorders the list but not as a simple mirror (cycleSort, resetFilter), so
// following the run the user cares about is more useful than a fixed
// cursor position. It's also correct for poll-driven reloads
// (handleRunsLoaded, handleSnapshotUpdated, handleBuildCompleted), which
// only update or add individual rows in place without reordering.
func (m *Model) reloadQueryPreservingSelection() tea.Cmd {
	return m.reloadQuery(true)
}

// reloadQueryKeepingCursorPosition reloads and leaves the list's current
// page/cursor position alone instead of chasing the previous selection.
// Use this when the change mirrors the whole list around a fixed point
// (toggleDirection reversing sort direction), where a run near one end can
// land at the opposite end — chasing its ID to that new position would
// jerk the cursor/viewport across the entire list.
func (m *Model) reloadQueryKeepingCursorPosition() tea.Cmd {
	return m.reloadQuery(false)
}

func (m *Model) checkInProgress() bool {
	for _, r := range m.runs {
		if !r.IsTerminal() {
			return true
		}
	}
	return false
}

// updateSpinnerFrames pushes the current spinner frame into the list delegate.
// Animation state lives on the delegate, not on the items: touching items
// (SetItems/SetItem) while a filter is active clears or refilters the filtered
// view asynchronously, making the list flicker on every tick.
func (m *Model) updateSpinnerFrames() {
	if m.list == nil {
		return
	}
	m.list.SetDelegate(m.listDelegate(m.columnWidths()))
}

func (m *Model) contentWidth() int {
	return max(1, m.wWidth-(listHPad*2))
}

func (m *Model) columnWidths() myRunsColumnWidths {
	return myRunsColumnWidthsFor(m.contentWidth())
}

func (m *Model) listDelegate(widths myRunsColumnWidths) runItemDelegate {
	frame := ""
	if m.hasInProgress {
		frame = m.spinnerModel.View()
	}
	return runItemDelegate{widths: widths, spinnerFrame: frame}
}

func (m *Model) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	if m.list == nil {
		return nil
	}

	if m.filtering {
		return m.handleFiltering(msg)
	}

	if key.Matches(msg, m.keyMap.Filter) {
		return m.enterFiltering()
	}

	if key.Matches(msg, m.keyMap.SortCycle) {
		return m.cycleSort()
	}

	if key.Matches(msg, m.keyMap.Direction) {
		return m.toggleDirection()
	}

	if key.Matches(msg, m.keyMap.StatusCycle) {
		return m.enterStatusFiltering()
	}

	if key.Matches(msg, m.keyMap.ResetFilter) {
		return m.resetFilter()
	}

	if key.Matches(msg, m.keyMap.Enter) {
		return m.withSelectedRecord(m.openRun)
	}

	if key.Matches(msg, m.keyMap.OpenJob) {
		return m.withSelectedRecord(m.openJobOfRun)
	}

	if key.Matches(msg, m.keyMap.Logs) {
		return m.withSelectedRecord(m.openLog)
	}

	if key.Matches(msg, m.keyMap.OpenBuildBrowser) {
		return m.withSelectedRecord(m.openBuildInBrowser)
	}

	if key.Matches(msg, m.keyMap.OpenJobBrowser) {
		return m.withSelectedRecord(m.openJobInBrowser)
	}

	if key.Matches(msg, m.keyMap.Replay) {
		return m.withSelectedRecord(m.replayRun)
	}

	if key.Matches(msg, m.keyMap.TriggerBuild) {
		return m.withSelectedRecord(m.triggerBuildForRun)
	}

	if key.Matches(msg, m.keyMap.CancelBuild) {
		return m.withSelectedRecord(m.abortRun)
	}

	if key.Matches(msg, m.keyMap.Delete) {
		return m.withSelectedRecord(m.deleteRun)
	}

	ll, cmd := m.list.Update(msg)
	m.list = &ll
	return cmd
}

// withSelectedRecord runs a row action against the highlighted run, and does
// nothing when the list has no selection.
func (m *Model) withSelectedRecord(action func(store.RunRecord) tea.Cmd) tea.Cmd {
	item, ok := m.list.SelectedItem().(runItem)
	if !ok {
		return nil
	}
	return action(item.record)
}

func (m *Model) handleFiltering(msg tea.KeyMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keyMap.Cancel):
		return m.exitFiltering(false)
	case key.Matches(msg, m.keyMap.Enter):
		return m.exitFiltering(true)
	case key.Matches(msg, m.keyMap.NextField):
		return m.focusNextFilterField()
	}
	if m.filterFocus == filterFieldStatus {
		return m.updateStatusField(msg)
	}
	return m.updateFilterField(msg)
}

// enterFiltering opens the inline name/build/status editor, seeded from the
// currently active query. preFilterQuery is snapshotted so Cancel can
// restore it exactly.
func (m *Model) enterFiltering() tea.Cmd {
	m.filtering = true
	m.preFilterQuery = m.query
	m.filterFocus = filterFieldName

	m.nameInput = textinput.New()
	m.nameInput.Placeholder = "partial job name"
	m.nameInput.SetWidth(24)
	m.nameInput.SetValue(m.query.JobName)

	m.buildInput = textinput.New()
	m.buildInput.Placeholder = "42 or #42"
	m.buildInput.SetWidth(16)
	m.buildInput.SetValue(m.query.BuildID)

	m.statusOptions = statusFilterOptions()
	m.statusCursor = slices.Index(m.statusOptions, m.query.Status)
	if m.statusCursor < 0 {
		m.statusCursor = 0
	}

	return tea.Batch(m.nameInput.Focus(), textinput.Blink)
}

// exitFiltering leaves the inline editor. On commit it forces an immediate
// reload of whatever's currently typed (superseding any pending debounce
// tick via the reqID staleness check); on cancel it restores the
// pre-edit query and reloads that instead.
func (m *Model) exitFiltering(commit bool) tea.Cmd {
	m.filtering = false
	m.nameInput.Blur()
	m.buildInput.Blur()
	if !commit {
		m.query = m.preFilterQuery
	}
	return m.reloadQueryPreservingSelection()
}

func (m *Model) focusNextFilterField() tea.Cmd {
	m.nameInput.Blur()
	m.buildInput.Blur()
	switch m.filterFocus {
	case filterFieldName:
		m.filterFocus = filterFieldBuild
		return m.buildInput.Focus()
	case filterFieldBuild:
		m.filterFocus = filterFieldStatus
		return nil
	default:
		m.filterFocus = filterFieldName
		return m.nameInput.Focus()
	}
}

// updateFilterField forwards a keystroke to whichever inline field is
// focused, updates the query's JobName/BuildID immediately (so the field
// value and m.query never disagree), and reschedules the debounced reload.
func (m *Model) updateFilterField(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.filterFocus {
	case filterFieldName:
		m.nameInput, cmd = m.nameInput.Update(msg)
		m.query.JobName = strings.TrimSpace(m.nameInput.Value())
	case filterFieldBuild:
		m.buildInput, cmd = m.buildInput.Update(msg)
		m.query.BuildID = strings.TrimSpace(m.buildInput.Value())
	}
	return tea.Batch(cmd, m.scheduleFilterDebounce())
}

// updateFocusedFilterAnimation forwards non-key messages, especially cursor
// blink messages, to the active text input without treating them as edits or
// scheduling another debounced query.
func (m *Model) updateFocusedFilterAnimation(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	switch m.filterFocus {
	case filterFieldName:
		m.nameInput, cmd = m.nameInput.Update(msg)
	case filterFieldBuild:
		m.buildInput, cmd = m.buildInput.Update(msg)
	}
	return cmd
}

func (m *Model) scheduleFilterDebounce() tea.Cmd {
	m.filterGen++
	gen := m.filterGen
	return tea.Tick(filterDebounceDelay, func(time.Time) tea.Msg {
		return FilterDebounceMsg{Gen: gen}
	})
}

// cycleSort steps SortBy through Date -> Duration -> Status -> Date and
// reloads immediately (there's no text to debounce).
func (m *Model) cycleSort() tea.Cmd {
	order := []store.RunSortField{store.RunSortDate, store.RunSortDuration, store.RunSortStatus}
	idx := 0
	for i, f := range order {
		if f == m.query.SortBy {
			idx = i
			break
		}
	}
	m.query.SortBy = order[(idx+1)%len(order)]
	return m.reloadQueryPreservingSelection()
}

// toggleDirection flips ascending/descending. This mirrors the whole list
// around its middle, so a previously-selected run near one end can land at
// the opposite end — chasing its ID to that new position would jerk the
// cursor/viewport across the entire list. Leave the current page/cursor
// position alone instead of preserving selection by ID.
func (m *Model) toggleDirection() tea.Cmd {
	if m.query.Direction == store.SortAscending {
		m.query.Direction = store.SortDescending
	} else {
		m.query.Direction = store.SortAscending
	}
	return m.reloadQueryKeepingCursorPosition()
}

// enterStatusFiltering opens the shared filter editor with the Status field
// focused. The existing status shortcut remains a quick way to reach it.
func (m *Model) enterStatusFiltering() tea.Cmd {
	cmd := m.enterFiltering()
	m.nameInput.Blur()
	m.filterFocus = filterFieldStatus
	return cmd
}

// statusFilterOptions returns the fixed set of status filter choices: an
// "All" entry ("") followed by every known Jenkins status. The set of
// statuses is closed and known ahead of time, so there's no need to query
// the run store for what's currently present.
func statusFilterOptions() []buildstatus.PipelineStatus {
	statuses := buildstatus.RunStatuses()
	options := make([]buildstatus.PipelineStatus, 0, len(statuses)+1)
	options = append(options, "")
	return append(options, statuses...)
}

func (m *Model) updateStatusField(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, m.keyMap.Up) {
		m.statusCursor = (m.statusCursor - 1 + len(m.statusOptions)) % len(m.statusOptions)
	} else if key.Matches(msg, m.keyMap.Down) {
		m.statusCursor = (m.statusCursor + 1) % len(m.statusOptions)
	} else {
		return nil
	}
	m.query.Status = m.statusOptions[m.statusCursor]
	return m.reloadQueryPreservingSelection()
}

func (m *Model) resetFilter() tea.Cmd {
	m.query = defaultRunQuery()
	return m.reloadQueryPreservingSelection()
}

func (m *Model) openRun(record store.RunRecord) tea.Cmd {
	return util.CmdHandler(build.OpenBuildMsg{JobURL: record.JobURL, BuildID: record.BuildID})
}

func (m *Model) openLog(record store.RunRecord) tea.Cmd {
	return buildaction.OpenLogByURL(record.JobURL, record.BuildID)
}

func (m *Model) openBuildInBrowser(record store.RunRecord) tea.Cmd {
	return buildaction.OpenBuildInBrowser(record.JobURL, record.BuildID)
}

func (m *Model) openJobInBrowser(record store.RunRecord) tea.Cmd {
	return buildaction.OpenJobInBrowser(record.JobURL)
}

// replayRun opens the run's pipeline script in the script view, where it can
// be edited and re-run. Unlike opening a build, the script view is handed a
// live job and run rather than fetching them itself, so the row's identifiers
// have to be resolved before navigating.
func (m *Model) replayRun(record store.RunRecord) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := m.ctx.RequestContext()
		defer cancel()
		id, parentIds := jobref.ToIDAndParents(record.JobURL)

		job, err := m.ctx.Jenkins.GetJob(ctx, id, parentIds...)
		if err != nil {
			return util.ErrorMsg{
				Error: err,
				Title: "Failed to get job",
			}
		}

		pipelineRun, err := job.GetPipelineRun(ctx, record.BuildID)
		if err != nil {
			return util.ErrorMsg{
				Error: err,
				Title: fmt.Sprintf("Failed to get build #%s", record.BuildID),
			}
		}

		pipelineRun.Job = job
		return build.ReplayMsg{Job: job, PipelineRun: *pipelineRun}
	}
}

// openJobOfRun navigates to the job a run belongs to. A run record only ever
// names a job that was built, so its kind is left unknown and resolved by the
// router.
func (m *Model) openJobOfRun(record store.RunRecord) tea.Cmd {
	return buildaction.OpenJobByURL(record.JobURL, record.JobName)
}

// triggerBuildForRun starts the trigger flow for the run's job, which has to
// be resolved from its URL before its parameters can be read.
func (m *Model) triggerBuildForRun(record store.RunRecord) tea.Cmd {
	return m.paramsRequest.FetchByURL(m.ctx, record.JobURL)
}

// abortRun stops the selected run. My Runs is mostly build history, so a row
// that already finished is answered directly rather than sent to Jenkins.
func (m *Model) abortRun(record store.RunRecord) tea.Cmd {
	return buildaction.ConfirmAbortByURL(m.ctx, record.JobURL, record.BuildID, record.IsTerminal())
}

func (m *Model) deleteRun(record store.RunRecord) tea.Cmd {
	id := record.ID
	return func() tea.Msg {
		if err := m.ctx.RunStore.DeleteRun(id); err != nil {
			log.Error("Failed to delete run", "id", id, "error", err)
			return util.FooterNotiMsg{
				Message: fmt.Sprintf("Failed to delete run: %s", err),
				Type:    util.NotiError,
			}
		}
		return RunDeletedMsg{ID: id}
	}
}

func (m *Model) handleRunsLoaded(msg RunsLoadedMsg) tea.Cmd {
	var loadNotify tea.Cmd
	if msg.Error != nil {
		log.Error("Failed to load runs from store", "error", msg.Error)
		loadNotify = util.CmdHandler(util.FooterNotiMsg{
			Message: fmt.Sprintf("Failed to load runs: %s", msg.Error),
			Type:    util.NotiError,
		})
	} else {
		m.runs = msg.Runs
	}

	wasInProgress := m.hasInProgress
	m.hasInProgress = m.checkInProgress()
	cmds := []tea.Cmd{loadNotify, m.buildList()}
	// A refresh must not inject an immediate frame into the running tick chain.
	if m.hasInProgress && !wasInProgress {
		cmds = append(cmds, m.spinnerModel.Tick)
	}
	return tea.Batch(cmds...)
}

// handleRunsQueried applies the result of a filter-apply or poll-driven
// reload. Stale results (an older reload that resolves after a newer one
// was issued) are dropped so they can't overwrite fresher rows.
func (m *Model) handleRunsQueried(msg RunsQueriedMsg) tea.Cmd {
	if m.isStale(msg.ReqID) {
		return nil
	}
	if msg.Error != nil {
		log.Error("Failed to query runs from store", "error", msg.Error)
		return util.CmdHandler(util.FooterNotiMsg{
			Message: fmt.Sprintf("Filter query failed: %s", msg.Error),
			Type:    util.NotiError,
		})
	}

	var selectedID string
	if msg.PreserveSelection {
		selectedID = m.selectedRunID()
	}
	m.runs = msg.Runs
	wasInProgress := m.hasInProgress
	m.hasInProgress = m.checkInProgress()
	cmd := m.buildList()
	if msg.PreserveSelection {
		m.restoreSelection(selectedID)
	}

	cmds := []tea.Cmd{cmd}
	if m.hasInProgress && !wasInProgress {
		cmds = append(cmds, m.spinnerModel.Tick)
	}
	return tea.Batch(cmds...)
}

// selectedRunID returns the currently selected run's ID, or "" if nothing
// is selected.
func (m *Model) selectedRunID() string {
	if m.list == nil {
		return ""
	}
	if item, ok := m.list.SelectedItem().(runItem); ok {
		return item.record.ID
	}
	return ""
}

// restoreSelection re-selects the run with the given ID after a reload. If
// it's no longer present, selection falls back to the first row.
func (m *Model) restoreSelection(id string) {
	if m.list == nil || id == "" {
		return
	}
	for i, item := range m.list.Items() {
		if ri, ok := item.(runItem); ok && ri.record.ID == id {
			m.list.Select(i)
			return
		}
	}
	m.list.Select(0)
}

// buildList (re)creates the list rows from m.runs. Filtering is disabled on
// this list (see SetFilteringEnabled below), so list.SetItems/SetItem always
// return a nil command in practice; it's still propagated defensively.
func (m *Model) buildList() tea.Cmd {
	contentWidth := m.contentWidth()
	widths := myRunsColumnWidthsFor(contentWidth)

	items := make([]list.Item, len(m.runs))
	for i, r := range m.runs {
		items[i] = runItem{record: r}
	}

	listHeight := len(items)*runItemHeight + 2 + 4
	listHeight = min(listHeight, max(runItemHeight, m.wHeight-8))
	if m.list == nil {
		l := list.New(items, m.listDelegate(widths), contentWidth, listHeight)
		l.SetShowStatusBar(false)
		l.SetShowTitle(false)
		l.SetShowHelp(false)
		// Filtering goes through the inline filter editor ("/") rather
		// than the list's own fuzzy filter, so there's only one filtering path.
		l.SetFilteringEnabled(false)
		m.list = &l
		return nil
	}
	m.list.SetDelegate(m.listDelegate(widths))
	if len(items) != len(m.list.Items()) {
		// The list only takes its height from list.New at creation time;
		// SetItems alone doesn't resize it. Without this, a filter that
		// shrinks the row count leaves the viewport "stuck" at that smaller
		// height even after the row count grows back (e.g. on Reset), so
		// fewer rows than fit on screen are visible until a window resize
		// happens to recompute it.
		m.list.SetSize(contentWidth, listHeight)
		return m.list.SetItems(items)
	}
	// Same rows with updated fields: swap them in place with SetItem rather
	// than SetItems, so the list's cursor/pagination state isn't recomputed
	// from scratch on every poll tick.
	var cmd tea.Cmd
	for i, item := range items {
		cmd = m.list.SetItem(i, item)
	}
	return cmd
}

func (m *Model) updateListSize() {
	if m.list == nil {
		return
	}
	contentWidth := m.contentWidth()
	m.list.SetDelegate(m.listDelegate(myRunsColumnWidthsFor(contentWidth)))
	listHeight := len(m.list.Items())*runItemHeight + 2 + 4
	listHeight = min(listHeight, max(runItemHeight, m.wHeight-8))
	m.list.SetSize(contentWidth, listHeight)
}

// hasActiveFilter reports whether the query differs from the no-filter,
// date-descending default My Runs opens with.
func (m *Model) hasActiveFilter() bool {
	return m.query != defaultRunQuery()
}

// renderFilterSummary renders the "Name: x | Status: y | Order: z ↓" line
// shown above the table when a filter or non-default order is active, and ""
// otherwise.
func (m *Model) renderFilterSummary(t *styles.Theme, contentWidth int) string {
	if !m.hasActiveFilter() {
		return ""
	}
	var parts []string
	if m.query.JobName != "" {
		parts = append(parts, fmt.Sprintf("Name: %s", m.query.JobName))
	}
	if m.query.BuildID != "" {
		parts = append(parts, fmt.Sprintf("Build: %s", m.query.BuildID))
	}
	if m.query.Status != "" {
		parts = append(parts, fmt.Sprintf("Status: %s", m.query.Status.Label()))
	}
	parts = append(parts, fmt.Sprintf("Order: %s %s", sortFieldLabel(m.query.SortBy), directionArrow(m.query.Direction)))

	summary := util.TurncateString(strings.Join(parts, " | "), contentWidth)
	return t.S().Base.Foreground(t.SubtleText).Render(summary)
}

// renderFilterEditor renders the inline name/build/status controls shown in
// place of the summary line while the "/" editor is open.
func (m *Model) renderFilterEditor(t *styles.Theme) string {
	base := t.S().Base
	focusMark := func(focused bool) string {
		if focused {
			return base.Foreground(t.Primary).Render("▶ ")
		}
		return "  "
	}

	status := "All"
	if m.query.Status != "" {
		status = m.query.Status.Label()
	}
	statusStyle := base
	if m.filterFocus == filterFieldStatus {
		statusStyle = statusStyle.Foreground(t.Primary).Bold(true)
	}

	row := lipgloss.JoinHorizontal(lipgloss.Left,
		focusMark(m.filterFocus == filterFieldName), base.Bold(true).Render("Name: "), m.nameInput.View(),
		"  ", focusMark(m.filterFocus == filterFieldBuild), base.Bold(true).Render("Build: "), m.buildInput.View(),
		"  ", focusMark(m.filterFocus == filterFieldStatus), base.Bold(true).Render("Status: "), statusStyle.Render(status),
	)
	if m.filterFocus != filterFieldStatus {
		return row
	}

	// The dropdown belongs to the same editor row and appears directly below
	// it only while Status has focus.
	options := make([]string, 0, len(m.statusOptions))
	for i, option := range m.statusOptions {
		label := "All"
		if option != "" {
			label = option.Label()
		}
		marker := "  "
		style := base
		if i == m.statusCursor {
			marker = "▶ "
			style = style.Foreground(t.Primary).Bold(true)
		}
		options = append(options, marker+style.Render(label))
	}
	dropdown := base.Border(lipgloss.RoundedBorder()).BorderForeground(t.Border).
		Padding(0, 1).Render(lipgloss.JoinVertical(lipgloss.Left, options...))
	return lipgloss.JoinVertical(lipgloss.Left, row, dropdown)
}

// renderListBody renders the run list, or a clear empty-state message when
// the active query returns no rows.
func (m *Model) renderListBody(t *styles.Theme) string {
	if len(m.runs) == 0 {
		msg := "No runs yet"
		if m.hasActiveFilter() {
			msg = "No runs match the current filter"
		}
		return t.S().Base.Foreground(t.SubtleText).Padding(1, 0).Render(msg)
	}
	return m.list.View()
}

func sortFieldLabel(f store.RunSortField) string {
	switch f {
	case store.RunSortDuration:
		return "Duration"
	case store.RunSortStatus:
		return "Status"
	default:
		return "Date"
	}
}

func directionArrow(d store.SortDirection) string {
	if d == store.SortAscending {
		return "↑"
	}
	return "↓"
}

func (m *Model) renderHeader(widths myRunsColumnWidths) string {
	t := styles.GetTheme()
	labelStyle := t.S().Base.
		Foreground(t.SubtleText).
		Bold(true)

	cells := widths.renderCells(labelStyle, "JOB", "BUILD", "STATUS", "STARTED", "DURATION")
	// PaddingLeft(1) matches the 1-char left border that HiddenBorder/RoundedBorder adds to each item row
	return t.S().Base.PaddingLeft(1).Render(lipgloss.JoinHorizontal(lipgloss.Left, cells...))
}

// PollRunsChanged reloads the active query after the poller commits observation changes.
func (m *Model) PollRunsChanged() tea.Cmd {
	return m.reloadQueryPreservingSelection()
}

func (m *Model) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	m.wWidth = msg.Width
	m.wHeight = msg.Height
	m.updateListSize()
	return nil
}

func (m *Model) handleFilterDebounce(msg FilterDebounceMsg) tea.Cmd {
	if msg.Gen != m.filterGen {
		return nil // a later keystroke superseded this debounce tick
	}
	return m.reloadQueryPreservingSelection()
}

func (m *Model) handleSpinnerTick(msg spinner.TickMsg) tea.Cmd {
	if !m.hasInProgress {
		return nil
	}
	var cmd tea.Cmd
	m.spinnerModel, cmd = m.spinnerModel.Update(msg)
	m.updateSpinnerFrames()
	return cmd
}

func (m *Model) updateList(msg tea.Msg) tea.Cmd {
	if m.list == nil {
		return nil
	}
	ll, cmd := m.list.Update(msg)
	m.list = &ll
	return cmd
}

const (
	runItemHeight      = 3
	runItemBorderWidth = 2
	listHPad           = 2
)

type myRunsColumnWidths struct {
	job, build, status, started, duration int
	statusFirst                           bool
}

func myRunsColumnWidthsFor(contentWidth int) myRunsColumnWidths {
	available := max(1, contentWidth-runItemBorderWidth)
	if available < 28 {
		status := min(2, available)
		return myRunsColumnWidths{status: status, job: max(0, available-status), statusFirst: true}
	}
	if available < 44 {
		status := 2
		build := min(6, max(0, available-status-8))
		return myRunsColumnWidths{status: status, build: build, job: max(0, available-status-build), statusFirst: true}
	}
	// Column widths include a 1-cell gutter. Wide layouts reserve enough status
	// space for the longest friendly label ("Waiting for approval"); narrower
	// layouts truncate labels before allowing rows to wrap.
	if available < 60 {
		build, status, started := 6, 10, 17
		return myRunsColumnWidths{job: max(1, available-build-status-started), build: build, status: status, started: started}
	}
	if available < 80 {
		build, status, started, duration := 8, 15, 17, 9
		return myRunsColumnWidths{job: max(1, available-build-status-started-duration), build: build, status: status, started: started, duration: duration}
	}

	build, status, started, duration := 10, 22, 18, 12
	return myRunsColumnWidths{job: max(1, available-build-status-started-duration), build: build, status: status, started: started, duration: duration}
}

func (w myRunsColumnWidths) rowWidth() int {
	return w.job + w.build + w.status + w.started + w.duration
}

func (w myRunsColumnWidths) renderCells(style lipgloss.Style, job, build, status, started, duration string) []string {
	render := func(width int, value string) string {
		if width <= 0 {
			return ""
		}
		// Clamp every cell to one line with a 1-cell gutter: a value that
		// filled or exceeded its column would touch the next column or wrap
		// the row, breaking the fixed item height of the list.
		value = ansi.Truncate(value, max(0, width-1), "…")
		return style.Width(width).Render(value)
	}

	cells := make([]string, 0, 5)
	if w.statusFirst {
		cells = append(cells, render(w.status, status), render(w.job, job), render(w.build, build))
	} else {
		cells = append(cells, render(w.job, job), render(w.build, build), render(w.status, status))
	}
	cells = append(cells, render(w.started, started), render(w.duration, duration))
	return cells
}

// runItem implements list.Item and holds only the immutable row data. Layout
// and animation state live on runItemDelegate, so rows never need to be
// re-set for a resize or a spinner frame.
type runItem struct {
	record store.RunRecord
}

// FilterValue satisfies list.Item. It's vestigial: the list's own fuzzy
// filter is disabled (filtering goes through the inline "/" editor instead),
// so this value is never read by anything.
func (i runItem) FilterValue() string {
	return i.record.JobName
}

// runItemDelegate implements list.ItemDelegate and carries the render state
// shared by every row: column widths and the current spinner frame.
type runItemDelegate struct {
	widths       myRunsColumnWidths
	spinnerFrame string
}

func (d runItemDelegate) Height() int  { return runItemHeight }
func (d runItemDelegate) Spacing() int { return 0 }

func (d runItemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d runItemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	ri, ok := item.(runItem)
	if !ok {
		return
	}

	t := styles.GetTheme()
	isSelected := m.Index() == index

	widths := d.widths

	jobName := truncateRunCell(ri.record.JobName, widths.job)
	statusIcon := renderStatusIcon(ri.record.Status)
	startTime := ""
	if ri.record.StartTime > 0 {
		startTime = util.FormatStartTime(ri.record.StartTime)
	}
	duration := ""
	if ri.record.Duration > 0 {
		duration = util.FormatDuration(ri.record.Duration)
	}

	// Show spinner animation for statuses that represent live progress.
	spinning := d.spinnerFrame != "" && ri.record.Status.ShouldAnimate(ri.record.EndTime)
	statusText := statusIcon + " " + ri.record.Status.Label()
	if spinning {
		statusText = d.spinnerFrame + " " + ri.record.Status.Label()
	}
	if widths.status <= 2 {
		statusText = statusIcon
		if spinning {
			// Spinner frames carry a trailing space ("⣾ "); drop it so the
			// glyph survives the 1-cell truncation of the icon-only column.
			statusText = strings.TrimRight(d.spinnerFrame, " ")
		}
	}

	base := t.S().Base
	lineContent := lipgloss.JoinHorizontal(lipgloss.Left,
		widths.renderCells(base, jobName, "#"+ri.record.BuildID, statusText, startTime, duration)...,
	)

	rowStyle := base
	rowWidth := widths.rowWidth()

	var content string
	if isSelected {
		content = rowStyle.
			Width(rowWidth).
			BorderForeground(t.Primary).
			Border(lipgloss.RoundedBorder()).
			Foreground(t.Primary).
			Render(lineContent)
	} else {
		content = rowStyle.
			Width(rowWidth).
			Border(lipgloss.HiddenBorder()).
			Render(lineContent)
	}

	fmt.Fprint(w, content)
}

func truncateRunCell(value string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	if width <= 3 {
		return string([]rune(value)[:min(width, len([]rune(value)))])
	}
	return util.TurncateString(value, width-1)
}

func renderStatusIcon(status buildstatus.PipelineStatus) string {
	t := styles.GetTheme()
	switch status.Outcome() {
	case buildstatus.OutcomeSuccess:
		return t.S().StyledIcons.Success
	case buildstatus.OutcomeFailure:
		return t.S().StyledIcons.Failure
	case buildstatus.OutcomeAborted, buildstatus.OutcomeNotBuilt:
		return t.S().StyledIcons.NotBuilt
	default:
		if status.IsActive(0) {
			return t.S().StyledIcons.Pending
		}
		return t.S().StyledIcons.NotBuilt
	}
}
