package myruns

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/buildaction"
	"github.com/atm1020/jenktui/src/ui/components/build"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/cursor"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

type requestRecordingTransport struct {
	requests int
}

func (t *requestRecordingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.requests++
	return nil, fmt.Errorf("unexpected Jenkins request before navigation")
}

// Worst-case cell contents seen in real runs: a spinner frame is 2 cells wide,
// "Waiting for approval" is the longest label, and a >999min build produces an
// 11-cell duration, and the start time is always 16 cells.
var rowLayoutContents = []struct {
	name     string
	job      string
	build    string
	status   string
	started  string
	duration string
}{
	{
		name:     "in-progress run with spinner frame",
		job:      "Ops > Monitoring > long-running-scan",
		build:    "#25",
		status:   "⣾  In progress",
		started:  "2026-07-05 10:51",
		duration: "3min 52s",
	},
	{
		name:     "failed run with long duration",
		job:      "Ops > Monitoring > long-running-scan",
		build:    "#24",
		status:   renderStatusIcon("FAILED") + " Failed",
		started:  "2026-07-05 00:17",
		duration: "1473min 43s",
	},
	{
		name:     "longest status",
		job:      "a-very-long-folder-name > and-a-very-long-job-name-that-truncates",
		build:    "#123456",
		status:   renderStatusIcon("PAUSED_PENDING_INPUT") + " Waiting for approval",
		started:  "2026-07-05 00:17",
		duration: "12s",
	},
}

// Rows must never wrap or let adjacent columns touch, no matter how wide the
// content is: a wrapped cell breaks the fixed item height of the list and
// bleeds into the next row, and a full cell joins status and date text.
func TestMyRunsRowLayout(t *testing.T) {
	base := styles.GetTheme().S().Base

	// Cover every breakpoint of myRunsColumnWidthsFor, including its edges.
	contentWidths := []int{20, 29, 40, 46, 55, 62, 76, 82, 120, 200}

	for _, tt := range rowLayoutContents {
		t.Run(tt.name, func(t *testing.T) {
			for _, contentWidth := range contentWidths {
				widths := myRunsColumnWidthsFor(contentWidth)
				cells := widths.renderCells(base, tt.job, tt.build, tt.status, tt.started, tt.duration)

				line := lipgloss.JoinHorizontal(lipgloss.Left, cells...)
				if got := lipgloss.Height(line); got != 1 {
					t.Errorf("contentWidth=%d: row wrapped to %d lines:\n%s", contentWidth, got, line)
				}
				if got, want := lipgloss.Width(line), widths.rowWidth(); got != want {
					t.Errorf("contentWidth=%d: row width = %d, want %d", contentWidth, got, want)
				}

				// Every column except the last must keep at least one blank
				// cell as a gutter so neighbouring columns never touch.
				for i, cell := range cells[:len(cells)-1] {
					if lipgloss.Width(cell) < 2 {
						continue
					}
					visible := strings.TrimRight(ansi.Strip(cell), " ")
					if lipgloss.Width(visible) >= lipgloss.Width(cell) {
						t.Errorf("contentWidth=%d cell[%d]: content %q fills the column, no gutter before next column",
							contentWidth, i, visible)
					}
				}
			}
		})
	}
}

// The common wide-terminal contents must fit their columns untruncated.
func TestMyRunsWideColumnsFitCommonContent(t *testing.T) {
	widths := myRunsColumnWidthsFor(120)

	for name, tc := range map[string]struct {
		value string
		width int
	}{
		"spinner in-progress status": {"⣾  In progress", widths.status},
		"longest status":             {renderStatusIcon("PAUSED_PENDING_INPUT") + " Waiting for approval", widths.status},
		"start time":                 {"2026-07-05 10:51", widths.started},
		"long duration":              {"1473min 43s", widths.duration},
	} {
		if lipgloss.Width(tc.value) > tc.width-1 {
			t.Errorf("%s: content width %d does not fit column width %d with a 1-cell gutter",
				name, lipgloss.Width(tc.value), tc.width)
		}
	}
}

// Filtering is now dialog-driven (SetFilteringEnabled(false)), so the list's
// own fuzzy filter can no longer be entered via user interaction. What still
// matters is that spinner ticks and poll-driven rebuilds don't corrupt the
// list's item count or lose the current selection.
func TestSpinnerTickAndPollRebuildPreserveItemsAndSelection(t *testing.T) {
	m := &Model{
		wWidth:       120,
		wHeight:      40,
		spinnerModel: spinner.New(),
		runs: []store.RunRecord{
			{ID: "a#1", JobName: "alpha-job", BuildID: "1", Status: "IN_PROGRESS"},
			{ID: "b#2", JobName: "beta-job", BuildID: "2", Status: "SUCCESS"},
		},
		hasInProgress: true,
	}
	m.buildList()
	m.list.Select(1)

	_, cmd := m.Update(spinner.TickMsg{})
	drainCmds(t, m, cmd)
	if got := len(m.list.Items()); got != 2 {
		t.Errorf("after spinner tick: len(items) = %d, want 2", got)
	}
	if ri, ok := m.list.SelectedItem().(runItem); !ok || ri.record.ID != "b#2" {
		t.Errorf("spinner tick moved selection: got %+v, want b#2 still selected", m.list.SelectedItem())
	}

	// Poll snapshots rebuild the same rows with fresh status/timing via
	// SetItem; that must not disturb selection either.
	m.runs[0].Status = "SUCCESS"
	cmd = m.buildList()
	drainCmds(t, m, cmd)
	if ri, ok := m.list.SelectedItem().(runItem); !ok || ri.record.ID != "b#2" {
		t.Errorf("poll rebuild moved selection: got %+v, want b#2 still selected", m.list.SelectedItem())
	}
	if ri, ok := m.list.Items()[0].(runItem); !ok || ri.record.Status != "SUCCESS" {
		t.Errorf("row not refreshed after poll rebuild: got %+v", m.list.Items()[0])
	}
}

// A filter that shrinks the row count must not leave the list "stuck" at
// that smaller viewport height once the row count grows back (e.g. Reset
// restoring the full, unfiltered list).
func TestBuildList_ResizesWhenItemCountGrowsBackAfterShrinking(t *testing.T) {
	m := &Model{
		wWidth:       120,
		wHeight:      100,
		spinnerModel: spinner.New(),
	}
	full := make([]store.RunRecord, 10)
	for i := range full {
		full[i] = store.RunRecord{ID: fmt.Sprintf("a#%d", i), JobName: "job", BuildID: fmt.Sprintf("%d", i), Status: "SUCCESS"}
	}

	m.runs = full
	m.buildList()
	fullHeight := m.list.Height()

	m.runs = full[:2]
	m.buildList()
	if shrunk := m.list.Height(); shrunk >= fullHeight {
		t.Fatalf("precondition: shrunk height (%d) should be less than full height (%d)", shrunk, fullHeight)
	}

	m.runs = full
	m.buildList()
	if got := m.list.Height(); got != fullHeight {
		t.Errorf("after growing back to the original row count: list height = %d, want %d restored", got, fullHeight)
	}
}

func TestOpenRunNavigatesWithoutJenkinsRequests(t *testing.T) {
	transport := &requestRecordingTransport{}
	jenkins := gojenkins.CreateJenkins(&http.Client{Transport: transport}, "http://jenkins")
	model := &Model{ctx: &context.ProgramContext{Jenkins: jenkins}}
	record := store.RunRecord{JobURL: "http://jenkins/job/demo/", BuildID: "42"}

	msg, ok := model.openRun(record)().(build.OpenBuildMsg)
	if !ok || msg.JobURL != record.JobURL || msg.BuildID != record.BuildID {
		t.Fatalf("navigation = %#v, want stored build reference", msg)
	}
	if transport.requests != 0 {
		t.Fatalf("Jenkins requests before navigation = %d, want 0", transport.requests)
	}
}

func TestLogsKeyNavigatesWithoutJenkinsRequests(t *testing.T) {
	transport := &requestRecordingTransport{}
	jenkins := gojenkins.CreateJenkins(&http.Client{Transport: transport}, "http://jenkins")
	cfg := config.GetDefaultConfig()
	record := store.RunRecord{
		ID: "http://jenkins/job/demo/#42", JobURL: "http://jenkins/job/demo/", BuildID: "42",
	}
	model := &Model{
		ctx:    &context.ProgramContext{Config: cfg, Jenkins: jenkins},
		wWidth: 120, wHeight: 40, keyMap: UserKeyMap(cfg.KeyMapConfig.MyRuns),
		runs: []store.RunRecord{record}, spinnerModel: spinner.New(),
	}
	_ = model.buildList()

	_, cmd := model.Update(tea.KeyPressMsg{Code: 'L', Text: "L"})
	if cmd == nil {
		t.Fatal("Logs key returned no navigation command")
	}
	msg, ok := cmd().(buildaction.OpenLogMsg)
	if !ok || msg.JobURL != record.JobURL || msg.BuildID != record.BuildID {
		t.Fatalf("navigation = %#v, want stored log reference", msg)
	}
	if transport.requests != 0 {
		t.Fatalf("Jenkins requests before navigation = %d, want 0", transport.requests)
	}
}

func TestMyRunsOpenActionsUseSharedDefaultKeys(t *testing.T) {
	cfg := config.GetDefaultConfig()
	record := store.RunRecord{
		ID: "http://jenkins/job/demo/#42", JobName: "demo",
		JobURL: "http://jenkins/job/demo/", BuildID: "42",
	}
	model := &Model{
		ctx: &context.ProgramContext{Config: cfg}, wWidth: 120, wHeight: 40,
		keyMap: UserKeyMap(cfg.KeyMapConfig.MyRuns), runs: []store.RunRecord{record}, spinnerModel: spinner.New(),
	}
	_ = model.buildList()

	tests := []struct {
		name string
		key  tea.KeyPressMsg
		want any
	}{
		{name: "open job", key: tea.KeyPressMsg{Code: 'j', Mod: tea.ModCtrl}, want: util.OpenJobTargetMsg{}},
		{name: "open log", key: tea.KeyPressMsg{Code: 'L', Text: "L"}, want: buildaction.OpenLogMsg{}},
		{name: "open build in browser", key: tea.KeyPressMsg{Code: 'o', Text: "o"}, want: util.BrowserOpenMsg{Url: "http://jenkins/job/demo/42"}},
		{name: "open job in browser", key: tea.KeyPressMsg{Code: 'O', Text: "O"}, want: util.BrowserOpenMsg{Url: record.JobURL}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, cmd := model.Update(tt.key)
			if cmd == nil {
				t.Fatal("key returned no action")
			}
			got := cmd()
			switch want := tt.want.(type) {
			case util.OpenJobTargetMsg:
				msg, ok := got.(util.OpenJobTargetMsg)
				if !ok || msg.Target.URL != record.JobURL {
					t.Fatalf("action = %#v, want job navigation", got)
				}
			case buildaction.OpenLogMsg:
				msg, ok := got.(buildaction.OpenLogMsg)
				if !ok || msg.JobURL != record.JobURL || msg.BuildID != record.BuildID {
					t.Fatalf("action = %#v, want log navigation", got)
				}
			case util.BrowserOpenMsg:
				msg, ok := got.(util.BrowserOpenMsg)
				if !ok || msg.Url != want.Url {
					t.Fatalf("action = %#v, want browser URL %q", got, want.Url)
				}
			}
		})
	}
}

func newTestModel(t *testing.T, runStore *store.Store) *Model {
	t.Helper()
	return &Model{
		ctx:          &context.ProgramContext{RunStore: runStore, Config: &context.Config{}},
		wWidth:       120,
		wHeight:      40,
		spinnerModel: spinner.New(),
		query:        defaultRunQuery(),
	}
}

func TestMyRunsViewUsesFriendlyStatusLabels(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.runs = []store.RunRecord{{ID: "a#1", JobName: "job", BuildID: "1", Status: "IN_PROGRESS"}}
	m.buildList()

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "In progress") {
		t.Errorf("view does not contain friendly status label:\n%s", view)
	}
	if strings.Contains(view, "IN_PROGRESS") {
		t.Errorf("view leaks raw Jenkins status:\n%s", view)
	}
}

func modelForFiltering(t *testing.T, runStore *store.Store) *Model {
	t.Helper()
	m := newTestModel(t, runStore)
	m.keyMap = UserKeyMap(context.MyRunsKeyMapConfig{
		Down:        context.KeyBinding{"down"},
		Up:          context.KeyBinding{"up"},
		Filter:      context.KeyBinding{"/"},
		NextField:   context.KeyBinding{"tab"},
		Cancel:      context.KeyBinding{"esc"},
		Enter:       context.KeyBinding{"enter"},
		SortCycle:   context.KeyBinding{"s"},
		Direction:   context.KeyBinding{"S"},
		StatusCycle: context.KeyBinding{"t"},
		ResetFilter: context.KeyBinding{"R"},
	})
	m.buildList()
	return m
}

func TestFilterKey_EntersInlineEditor(t *testing.T) {
	m := modelForFiltering(t, store.New(t.TempDir()))

	_, cmd := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if !m.filtering {
		t.Fatal("pressing Filter did not enter filtering mode")
	}
	if cmd == nil {
		t.Error("entering filtering returned a nil command, want Focus+Blink")
	}
	if !m.nameInput.Focused() {
		t.Error("name input should be focused when entering filtering")
	}
}

func TestTypingName_DebouncedGenAppliesFilteredRows(t *testing.T) {
	runStore := store.New(t.TempDir())
	must(t, runStore.AddRun(store.RunRecord{ID: "a#1", JobName: "deploy-prod", JobURL: "http://a/", BuildID: "1", Status: "SUCCESS"}))
	must(t, runStore.AddRun(store.RunRecord{ID: "a#2", JobName: "build-app", JobURL: "http://a/", BuildID: "2", Status: "SUCCESS"}))

	m := modelForFiltering(t, runStore)
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "deploy" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if m.query.JobName != "deploy" {
		t.Fatalf("m.query.JobName = %q after typing, want %q (updates synchronously, ahead of the debounce)", m.query.JobName, "deploy")
	}

	// Only the tick matching the latest keystroke's generation should reload.
	_, cmd := m.Update(FilterDebounceMsg{Gen: m.filterGen})
	drainCmds(t, m, cmd)

	if len(m.runs) != 1 || m.runs[0].ID != "a#1" {
		t.Errorf("m.runs = %+v, want only a#1 (matches JobName filter)", m.runs)
	}
}

func TestFilterDebounce_StaleGenIsIgnored(t *testing.T) {
	m := modelForFiltering(t, store.New(t.TempDir()))
	m.runs = []store.RunRecord{{ID: "a#1", JobName: "job", Status: "SUCCESS"}}
	m.buildList()
	m.filterGen = 5

	_, cmd := m.Update(FilterDebounceMsg{Gen: 3})
	if cmd != nil {
		t.Errorf("stale debounce tick returned a command, want nil (a newer keystroke superseded it)")
	}
}

func TestEnter_CommitsTypedFilterImmediately(t *testing.T) {
	runStore := store.New(t.TempDir())
	must(t, runStore.AddRun(store.RunRecord{ID: "a#1", JobName: "deploy-prod", JobURL: "http://a/", BuildID: "1", Status: "SUCCESS"}))
	must(t, runStore.AddRun(store.RunRecord{ID: "a#2", JobName: "build-app", JobURL: "http://a/", BuildID: "2", Status: "SUCCESS"}))

	m := modelForFiltering(t, runStore)
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "deploy" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	// Enter commits without waiting for the debounce tick to fire.
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.filtering {
		t.Error("filtering should be closed after Enter")
	}
	drainCmds(t, m, cmd)

	if len(m.runs) != 1 || m.runs[0].ID != "a#1" {
		t.Errorf("m.runs = %+v, want only a#1 committed immediately on Enter", m.runs)
	}
}

func TestEsc_CancelRevertsToPreEditQuery(t *testing.T) {
	runStore := store.New(t.TempDir())
	must(t, runStore.AddRun(store.RunRecord{ID: "a#1", JobName: "deploy-prod", JobURL: "http://a/", BuildID: "1", Status: "SUCCESS"}))
	must(t, runStore.AddRun(store.RunRecord{ID: "a#2", JobName: "build-app", JobURL: "http://a/", BuildID: "2", Status: "SUCCESS"}))

	m := modelForFiltering(t, runStore)
	m.query.JobName = "deploy"
	m.buildList()

	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	for _, r := range "xyz" {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	if m.query.JobName != "deployxyz" {
		t.Fatalf("precondition: m.query.JobName = %q, want %q", m.query.JobName, "deployxyz")
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.filtering {
		t.Error("filtering should be closed after Esc")
	}
	if m.query.JobName != "deploy" {
		t.Errorf("m.query.JobName = %q after cancel, want reverted to %q", m.query.JobName, "deploy")
	}
	drainCmds(t, m, cmd)
	if len(m.runs) != 1 || m.runs[0].ID != "a#1" {
		t.Errorf("m.runs = %+v, want the pre-edit filter reapplied", m.runs)
	}
}

func TestNextField_TogglesFocusBetweenNameAndBuild(t *testing.T) {
	m := modelForFiltering(t, store.New(t.TempDir()))
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if m.filterFocus != filterFieldName {
		t.Fatalf("initial focus = %d, want filterFieldName", m.filterFocus)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.filterFocus != filterFieldBuild || !m.buildInput.Focused() {
		t.Errorf("after tab: focus = %d, buildInput.Focused() = %v, want filterFieldBuild focused", m.filterFocus, m.buildInput.Focused())
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.filterFocus != filterFieldStatus {
		t.Errorf("after second tab: focus = %d, want filterFieldStatus", m.filterFocus)
	}

	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.filterFocus != filterFieldName || !m.nameInput.Focused() {
		t.Errorf("after third tab: focus = %d, nameInput.Focused() = %v, want filterFieldName focused", m.filterFocus, m.nameInput.Focused())
	}
}

func TestSortCycle_StepsThroughFieldsAndReloads(t *testing.T) {
	m := modelForFiltering(t, store.New(t.TempDir()))
	if m.query.SortBy != store.RunSortDate {
		t.Fatalf("initial SortBy = %q, want %q", m.query.SortBy, store.RunSortDate)
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	drainCmds(t, m, cmd)
	if m.query.SortBy != store.RunSortDuration {
		t.Errorf("SortBy after one cycle = %q, want %q", m.query.SortBy, store.RunSortDuration)
	}

	_, cmd = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	drainCmds(t, m, cmd)
	if m.query.SortBy != store.RunSortStatus {
		t.Errorf("SortBy after two cycles = %q, want %q", m.query.SortBy, store.RunSortStatus)
	}

	_, cmd = m.Update(tea.KeyPressMsg{Code: 's', Text: "s"})
	drainCmds(t, m, cmd)
	if m.query.SortBy != store.RunSortDate {
		t.Errorf("SortBy after three cycles = %q, want wrap back to %q", m.query.SortBy, store.RunSortDate)
	}
}

func TestDirection_TogglesAscDesc(t *testing.T) {
	m := modelForFiltering(t, store.New(t.TempDir()))
	if m.query.Direction != store.SortDescending {
		t.Fatalf("initial Direction = %q, want %q", m.query.Direction, store.SortDescending)
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	drainCmds(t, m, cmd)
	if m.query.Direction != store.SortAscending {
		t.Errorf("Direction after toggle = %q, want %q", m.query.Direction, store.SortAscending)
	}

	_, cmd = m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	drainCmds(t, m, cmd)
	if m.query.Direction != store.SortDescending {
		t.Errorf("Direction after second toggle = %q, want %q", m.query.Direction, store.SortDescending)
	}
}

// Reversing direction mirrors the whole list: the run selected before the
// toggle can land at the opposite end after it. Direction must not chase
// that run's ID to its new position (which would jerk the cursor from one
// end of the list to the other) — the cursor's numeric position should stay
// put instead.
func TestDirection_DoesNotChaseSelectionAcrossReversal(t *testing.T) {
	runStore := store.New(t.TempDir())
	for i := range 5 {
		rec := store.RunRecord{
			ID:        fmt.Sprintf("a#%d", i),
			JobName:   "job",
			JobURL:    "http://a/",
			BuildID:   fmt.Sprintf("%d", i),
			Status:    "SUCCESS",
			StartTime: int64((i + 1) * 1000),
			CreatedAt: int64((i + 1) * 1000),
		}
		must(t, runStore.AddRun(rec))
	}

	m := modelForFiltering(t, runStore)
	runs, err := runStore.QueryRuns(m.query)
	must(t, err)
	m.runs = runs
	m.buildList()

	// Default order is date-descending, so index 0 is "a#4" (latest start time).
	if got := m.list.SelectedItem().(runItem).record.ID; got != "a#4" {
		t.Fatalf("precondition: selected = %q, want a#4 (newest first)", got)
	}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'S', Text: "S"})
	drainCmds(t, m, cmd)

	// Reversed to ascending: "a#4" is now last, not first. If direction had
	// chased the old selection by ID, index 0 would show something other
	// than "a#0" (the run that belongs at the top of the reversed order).
	if got := m.list.Index(); got != 0 {
		t.Errorf("cursor index after direction toggle = %d, want 0 (unchanged, not chased to a#4's new position)", got)
	}
	if got := m.list.SelectedItem().(runItem).record.ID; got != "a#0" {
		t.Errorf("selected run after direction toggle = %q, want a#0 (oldest, now first under ascending order)", got)
	}
}

func TestFilterEditor_StatusDropdownAppliesSelection(t *testing.T) {
	runStore := store.New(t.TempDir())
	must(t, runStore.AddRun(store.RunRecord{ID: "a#1", JobName: "job", BuildID: "1", Status: "SUCCESS"}))
	must(t, runStore.AddRun(store.RunRecord{ID: "a#2", JobName: "job", BuildID: "2", Status: "FAILED"}))
	m := modelForFiltering(t, runStore)

	_, cmd := m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	drainCmds(t, m, cmd)
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	m.Update(tea.KeyPressMsg{Code: tea.KeyTab})
	if m.filterFocus != filterFieldStatus {
		t.Fatal("Status is not the third field in the shared filter editor")
	}
	wantOptions := append([]buildstatus.PipelineStatus{""}, buildstatus.RunStatuses()...)
	if got := m.statusOptions; !slices.Equal(got, wantOptions) {
		t.Fatalf("status options = %v, want %v", got, wantOptions)
	}
	view := m.View()
	for _, want := range []string{"Name:", "Build:", "Status:", "All", "Failed"} {
		if !strings.Contains(view, want) {
			t.Errorf("filter editor does not show %q:\n%s", want, view)
		}
	}

	// Step from "All" down to "FAILED"; selection filters live, Enter closes editor.
	failedIndex := slices.Index(m.statusOptions, "FAILED")
	for range failedIndex {
		_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
		drainCmds(t, m, cmd)
	}
	_, cmd = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	drainCmds(t, m, cmd)
	if m.filtering {
		t.Error("filter editor should close after Enter")
	}
	if m.query.Status != "FAILED" || len(m.runs) != 1 || m.runs[0].Status != "FAILED" {
		t.Errorf("status filter not applied: query=%+v runs=%+v", m.query, m.runs)
	}
}

func TestStatusShortcut_FocusesStatusInSharedEditor(t *testing.T) {
	m := modelForFiltering(t, store.New(t.TempDir()))
	_, cmd := m.Update(tea.KeyPressMsg{Code: 't', Text: "t"})
	drainCmds(t, m, cmd)
	if !m.filtering || m.filterFocus != filterFieldStatus {
		t.Errorf("status shortcut: filtering=%v focus=%d, want shared editor with Status focused", m.filtering, m.filterFocus)
	}
}

func TestResetFilter_RestoresDefaults(t *testing.T) {
	m := modelForFiltering(t, store.New(t.TempDir()))
	m.query = store.RunQuery{JobName: "deploy", Status: "FAILED", SortBy: store.RunSortDuration, Direction: store.SortAscending}

	_, cmd := m.Update(tea.KeyPressMsg{Code: 'R', Text: "R"})
	drainCmds(t, m, cmd)

	if m.query != defaultRunQuery() {
		t.Errorf("m.query = %+v, want default %+v", m.query, defaultRunQuery())
	}
}

func TestRunsQueried_ErrorPreservesExistingRows(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.runs = []store.RunRecord{{ID: "a#1", JobName: "job", Status: "SUCCESS"}}
	m.buildList()
	m.loadReqID = 1

	cmd := m.handleRunsQueried(RunsQueriedMsg{Error: errQueryFailed, ReqID: 1})
	if cmd == nil {
		t.Fatal("want a footer notification command on query error, got nil")
	}
	if len(m.runs) != 1 || m.runs[0].ID != "a#1" {
		t.Errorf("m.runs = %+v, want the pre-existing row preserved on error", m.runs)
	}
}

func TestRunsQueried_StaleResultIsDropped(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.runs = []store.RunRecord{{ID: "a#1", JobName: "job", Status: "SUCCESS"}}
	m.buildList()
	m.loadReqID = 2 // a newer reload has already been issued

	cmd := m.handleRunsQueried(RunsQueriedMsg{
		Runs:  []store.RunRecord{{ID: "b#1", JobName: "other", Status: "SUCCESS"}},
		ReqID: 1, // stale
	})
	if cmd != nil {
		t.Errorf("stale result returned a command, want nil (dropped)")
	}
	if len(m.runs) != 1 || m.runs[0].ID != "a#1" {
		t.Errorf("m.runs = %+v, want unchanged (stale result must not apply)", m.runs)
	}
}

func TestFilterSummary_EmptyWhenDefaultQuery(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	if got := m.renderFilterSummary(styles.GetTheme(), 80); got != "" {
		t.Errorf("renderFilterSummary with default query = %q, want empty", got)
	}
}

func TestFilterSummary_ShowsActiveFilterAndOrder(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.query = store.RunQuery{Status: "FAILED", SortBy: store.RunSortDuration, Direction: store.SortDescending}

	got := m.renderFilterSummary(styles.GetTheme(), 80)
	for _, want := range []string{"Status: Failed", "Order: Duration"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary = %q, want it to contain %q", got, want)
		}
	}
}

func TestEmptyFilteredResult_ShowsClearMessage(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.query = store.RunQuery{JobName: "no-such-job", SortBy: store.RunSortDate, Direction: store.SortDescending}
	m.runs = nil
	m.buildList()

	view := m.View()
	if !strings.Contains(view, "No runs match the current filter") {
		t.Errorf("view does not contain the empty-filter message:\n%s", view)
	}
}

// A committed observation that moves a run out of the active filter must
// disappear when the app routes RunsChanged to this query-only projection.
func TestPollRunsChangedReloadsActiveFilter(t *testing.T) {
	runStore := store.New(t.TempDir())
	jobURL := "http://jenkins/job/a/"
	must(t, runStore.AddRun(store.RunRecord{ID: jobURL + "#1", JobName: "a", JobURL: jobURL, BuildID: "1", Status: "IN_PROGRESS"}))

	m := newTestModel(t, runStore)
	m.query = store.RunQuery{Status: "IN_PROGRESS", SortBy: store.RunSortDate, Direction: store.SortDescending}
	m.runs = []store.RunRecord{{ID: jobURL + "#1", JobName: "a", JobURL: jobURL, BuildID: "1", Status: "IN_PROGRESS"}}
	m.buildList()
	must(t, runStore.UpdateRun(jobURL+"#1", "SUCCESS", 1000, 500, 500))

	drainCmds(t, m, m.PollRunsChanged())

	if len(m.runs) != 0 {
		t.Errorf("m.runs = %+v, want empty after committed run left the filter", m.runs)
	}
}

var errQueryFailed = fmt.Errorf("query failed")

// drainCmds runs the returned commands like the Bubble Tea runtime would,
// feeding resulting messages back into the model. Spinner ticks are not
// re-queued so the tick loop terminates.
func drainCmds(t *testing.T, m *Model, cmd tea.Cmd) {
	t.Helper()
	queue := []tea.Cmd{cmd}
	for steps := 0; len(queue) > 0; steps++ {
		if steps > 100 {
			t.Fatal("drainCmds: command loop did not terminate")
		}
		c := queue[0]
		queue = queue[1:]
		if c == nil {
			continue
		}
		switch msg := c().(type) {
		case nil, cursor.BlinkMsg, spinner.TickMsg:
		case tea.BatchMsg:
			queue = append(queue, msg...)
		default:
			_, next := m.Update(msg)
			queue = append(queue, next)
		}
	}
}

func TestNewModelWithQuery_StartsWithGivenQuery(t *testing.T) {
	ctx := &context.ProgramContext{RunStore: store.New(t.TempDir()), Config: &context.Config{}}
	query := store.RunQuery{JobName: "my-job", SortBy: store.RunSortDate, Direction: store.SortDescending}

	m := NewModelWithQuery(ctx, 120, 40, query)

	if m.query != query {
		t.Errorf("m.query = %+v, want %+v", m.query, query)
	}
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

// Replay resolves the row back into a live job and pipeline run and hands both
// to the script view, the same replay the job and build views offer.
func TestReplayRunOpensScriptView(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/job/demo/api/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "demo", "url": "http://" + request.Host + "/job/demo/"})
		case "/job/demo/42/wfapi/describe/api/json":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "42", "name": "#42", "status": "SUCCESS"})
		default:
			http.NotFound(w, request)
		}
	}))
	defer server.Close()

	m := newTestModel(t, store.New(t.TempDir()))
	m.ctx.Jenkins = gojenkins.CreateJenkins(nil, server.URL)

	msg := m.replayRun(store.RunRecord{JobURL: server.URL + "/job/demo/", BuildID: "42"})()

	replay, ok := msg.(build.ReplayMsg)
	if !ok {
		t.Fatalf("replay result = %#v, want build.ReplayMsg", msg)
	}
	if replay.Job == nil || replay.Job.GetName() != "demo" {
		t.Fatalf("replay job = %#v, want demo", replay.Job)
	}
	if replay.PipelineRun.ID != "42" || replay.PipelineRun.Job == nil {
		t.Fatalf("replay run = %#v, want run 42 carrying its job", replay.PipelineRun)
	}
}

// My Runs stores a job URL only, so triggering has to resolve the job before
// the parameter dialog can be opened.
func TestTriggerBuildForRunFetchesParamsByURL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/job/demo/api/json" {
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "demo", "url": "http://" + request.Host + "/job/demo/"})
			return
		}
		http.NotFound(w, request)
	}))
	defer server.Close()

	m := newTestModel(t, store.New(t.TempDir()))
	m.ctx.Jenkins = gojenkins.CreateJenkins(nil, server.URL)

	msg, ok := m.triggerBuildForRun(store.RunRecord{JobURL: server.URL + "/job/demo/"})().(buildaction.ParamsFetchedMsg)
	if !ok {
		t.Fatal("trigger result is not a buildaction.ParamsFetchedMsg")
	}
	if msg.Error != nil {
		t.Fatalf("params fetch error = %v, want nil", msg.Error)
	}
	if msg.Job == nil || msg.Job.GetName() != "demo" {
		t.Fatalf("resolved job = %#v, want demo", msg.Job)
	}
}

// Row actions must be no-ops on an empty list rather than panicking on a
// missing selection.
func TestRowActionsIgnoreEmptyList(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	m.buildList()

	if cmd := m.withSelectedRecord(func(store.RunRecord) tea.Cmd {
		t.Fatal("row action ran without a selected run")
		return nil
	}); cmd != nil {
		t.Fatal("row action on an empty list returned a command")
	}
}

func TestCleanupInvalidatesTriggerParamsRequest(t *testing.T) {
	m := newTestModel(t, store.New(t.TempDir()))
	fetch := m.paramsRequest.Fetch(m.ctx, nil)

	_ = m.CleanUp()
	_, cmd := m.Update(fetch())
	if cmd != nil {
		t.Fatal("parameter result survived page cleanup")
	}
}

// My Runs is mostly history, so aborting a finished row must not send a doomed
// stop request to Jenkins; a live row still goes through the confirmation.
func TestAbortRunGuardsFinishedRuns(t *testing.T) {
	for _, tt := range []struct {
		name   string
		record store.RunRecord
		want   string
	}{
		{
			name:   "running run asks for confirmation",
			record: store.RunRecord{BuildID: "42", JobURL: "http://jenkins/job/demo/", Status: "IN_PROGRESS"},
			want:   "confirm",
		},
		{
			name:   "finished run warns instead",
			record: store.RunRecord{BuildID: "42", JobURL: "http://jenkins/job/demo/", Status: "SUCCESS"},
			want:   "warn",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			m := newTestModel(t, store.New(t.TempDir()))

			cmd := m.abortRun(tt.record)
			if cmd == nil {
				t.Fatal("abort returned no command")
			}

			switch msg := cmd().(type) {
			case util.OpenDialogMsg:
				if tt.want != "confirm" {
					t.Fatalf("opened a confirmation for a %s run, want a warning", tt.record.Status)
				}
			case util.FooterNotiMsg:
				if tt.want != "warn" {
					t.Fatalf("warned instead of confirming for a %s run", tt.record.Status)
				}
				if msg.Type != util.NotiWarning || !strings.Contains(msg.Message, "#42") {
					t.Fatalf("warning = %#v, want a warning naming build #42", msg)
				}
			default:
				t.Fatalf("abort produced %T, want a dialog or a footer warning", msg)
			}
		})
	}
}
