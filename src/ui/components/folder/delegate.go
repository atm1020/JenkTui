package folder

import (
	"fmt"
	"io"
	"strings"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
	"github.com/charmbracelet/x/ansi"
)

// ColumnWidths holds the rendered width of each column in a folder/branch
// listing row. Shared by the header (folder.go) and every row (this file)
// so they never drift out of sync.
type ColumnWidths struct {
	Type         int
	Name         int
	Status       int
	Weather      int
	LastSuccess  int
	LastFailure  int
	LastDuration int
}

// weatherColumnWidth is fixed and narrow: it only ever holds a single icon.
const weatherColumnWidth = 4

// computeColumnWidths splits contentWidth across the row's columns. NAME
// gets double weight since job/branch names run long (e.g.
// "merge-queue/PR-48"); WEATHER is a narrow icon-only column.
func computeColumnWidths(contentWidth int) ColumnWidths {
	const weightUnits = 7 // Type(1) + Name(2) + Status(1) + LastSuccess(1) + LastFailure(1) + LastDuration(1)
	regular := max(1, contentWidth-weatherColumnWidth)
	unit := regular / weightUnits
	return ColumnWidths{
		Type:         unit,
		Name:         unit * 2,
		Status:       unit,
		Weather:      weatherColumnWidth,
		LastSuccess:  unit,
		LastFailure:  unit,
		LastDuration: unit,
	}
}

// Total returns the full rendered row width across all columns.
func (c ColumnWidths) Total() int {
	return c.Type + c.Name + c.Status + c.Weather + c.LastSuccess + c.LastFailure + c.LastDuration
}

// renderCell clamps value to width (a 1-cell gutter short of it, so adjacent
// columns never touch) before handing it to style.Width().Render(). Style's
// own Width() wraps overflowing content onto extra lines instead of clipping
// it, which silently grows a row past its declared Height() and overflows
// the list's viewport — ansi.Truncate is used (not a byte-length cut) since
// column values can already carry ANSI styling from themed icons.
func (c ColumnWidths) renderCell(style lipgloss.Style, width int, value string) string {
	if width <= 0 {
		return ""
	}
	return style.Width(width).Render(ansi.Truncate(value, max(0, width-1), "…"))
}

type InnerJobsDelegate struct {
	Class      string
	Name       string
	Url        string
	Color      string
	IsFavorite bool

	// ParentKind is the kind of container this row was listed from. A child's
	// own class doesn't always say what it is to the user: every branch of a
	// MultiBranch Pipeline is a plain WorkflowJob, so without knowing where
	// the row came from the whole page reads "Pipeline".
	ParentKind jobref.Kind

	// Buildable is nil when Jenkins did not report buildability at all —
	// which is the normal case for a container child (a nested Folder is not
	// a buildable thing), and must not be confused with the explicit false
	// that marks a disabled or orphaned job.
	Buildable           *bool
	HealthReport        []gojenkins.HealthReport
	LastSuccessfulBuild *gojenkins.BuildRef
	LastFailedBuild     *gojenkins.BuildRef
	LastBuild           *gojenkins.BuildRef

	Widths ColumnWidths
}

func (d InnerJobsDelegate) FilterValue() string {
	return d.Name
}

// rowHeight is the true physical line count of one rendered row: Render
// wraps every row in a full Border() (top + content + bottom), even the
// "hidden" one used for non-selected rows. bubbles/list packs exactly as
// many rows into a list's configured height as Height() claims each one
// costs, so this must match Render's actual output or rows overflow the
// list's viewport (see TestInnerJobsDelegate_HeightMatchesRenderedLineCount).
const rowHeight = 3

func (d InnerJobsDelegate) Height() int  { return rowHeight }
func (d InnerJobsDelegate) Spacing() int { return 0 }

func (d InnerJobsDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d InnerJobsDelegate) Render(w io.Writer, m list.Model, index int, ii list.Item) {
	i, ok := ii.(InnerJobsDelegate)
	if !ok {
		return
	}

	t := styles.GetTheme()
	isSelected := index == m.Index()
	base := t.S().Base

	// Type icon with label
	typeIcon := d.getClassIcon(i)
	typeLabel := d.getClassLabel(i)
	typeText := i.Widths.renderCell(base, i.Widths.Type, typeIcon+" "+typeLabel)

	// Job/branch name — struck through and muted once we know it's an
	// orphaned/disabled branch (see isOrphaned), matching classic Jenkins.
	// The name is truncated to its column budget (minus room for
	// the bookmark icon) before either decoration, so a long branch name
	// (e.g. a URL-decoded "merge-queue/PR-48") can't wrap the row onto extra
	// lines — the decoration is rendered on the bare name first, then the
	// already-styled bookmark icon is prepended outside of it — combining
	// Strikethrough with pre-rendered nested ANSI in one Render call garbles
	// the output (a stray "38;2;...m" escape leaks into the visible text).
	bookmarkWidth := 0
	if i.IsFavorite {
		bookmarkWidth = lipgloss.Width(t.S().StyledIcons.BookMark)
	}
	name := ansi.Truncate(i.Name, max(0, i.Widths.Name-1-bookmarkWidth), "…")
	if d.isOrphaned(i) {
		name = base.Foreground(t.SubtleText).Strikethrough(true).Render(name)
	}
	if i.IsFavorite {
		name = t.S().StyledIcons.BookMark + name
	}
	nameText := i.Widths.renderCell(base, i.Widths.Name, name)

	// Status with icon and text
	statusIcon := d.getColorIcon(i.Color)
	statusLabel := d.getStatusLabel(i.Color)
	statusText := i.Widths.renderCell(base, i.Widths.Status, statusIcon+" "+statusLabel)

	weatherText := i.Widths.renderCell(base, i.Widths.Weather, d.getWeatherIcon(i))

	lastSuccessText := i.Widths.renderCell(base, i.Widths.LastSuccess, d.formatBuildRefTime(i.LastSuccessfulBuild))
	lastFailureText := i.Widths.renderCell(base, i.Widths.LastFailure, d.formatBuildRefTime(i.LastFailedBuild))
	lastDurationText := i.Widths.renderCell(base, i.Widths.LastDuration, d.formatLastBuildDuration(i))

	rowStyle := base

	var content string
	lineContent := lipgloss.JoinHorizontal(
		lipgloss.Left,
		typeText,
		nameText,
		statusText,
		weatherText,
		lastSuccessText,
		lastFailureText,
		lastDurationText,
	)

	rowWidth := i.Widths.Total()
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

// isOrphaned reports whether i should render as struck-through/muted, i.e.
// a real pipeline job that Jenkins has explicitly marked non-buildable (an
// orphaned MultiBranch branch, or a manually-disabled job).
//
// Two things keep this from over-firing. Jenkins omits buildable entirely
// for kinds that are not buildable at all, so a nil value means "not
// applicable", never "disabled". And container children are excluded by
// class regardless, so a plain sub-folder never looks disabled.
func (d InnerJobsDelegate) isOrphaned(i InnerJobsDelegate) bool {
	return i.Buildable != nil && !*i.Buildable && jobref.IsPipeline(i.Class)
}

func (d InnerJobsDelegate) getColorIcon(color string) string {
	t := styles.GetTheme()
	jobColor := buildstatus.JobColor(color)
	if jobColor.IsRunning() {
		return t.S().StyledIcons.Running
	}
	switch jobColor.Outcome() {
	case buildstatus.OutcomeSuccess:
		return t.S().StyledIcons.Success
	case buildstatus.OutcomeFailure:
		return t.S().StyledIcons.Failure
	case buildstatus.OutcomeNotBuilt:
		return t.S().StyledIcons.NotBuilt
	default:
		return t.S().StyledIcons.NotAvailable
	}
}

// getWeatherIcon buckets a job's recent build-health score (0-100) into the
// same four-level scheme classic Jenkins uses, matching the sun/cloud/rain
// icons already wired into the theme. Jenkins can report several health
// reports for one job (build stability, test results, coverage, …); classic
// Jenkins shows the worst of them, since that is the one worth acting on.
// A job with no health data at all gets NotAvailable — "…" would misread as
// unstyled text at this width.
func (d InnerJobsDelegate) getWeatherIcon(i InnerJobsDelegate) string {
	t := styles.GetTheme()
	if len(i.HealthReport) == 0 {
		return t.S().StyledIcons.NotAvailable
	}
	score := i.HealthReport[0].Score
	for _, r := range i.HealthReport[1:] {
		score = min(score, r.Score)
	}
	switch {
	case score >= 80:
		return t.S().StyledIcons.Sunny
	case score >= 60:
		return t.S().StyledIcons.OneCloud
	case score >= 40:
		return t.S().StyledIcons.Cloudy
	default:
		return t.S().StyledIcons.ThunderStorm
	}
}

func (d InnerJobsDelegate) formatBuildRefTime(ref *gojenkins.BuildRef) string {
	if ref == nil {
		return "N/A"
	}
	return fmt.Sprintf("%s #%d", util.FormatStartTime(ref.Timestamp), ref.Number)
}

// formatLastBuildDuration renders the latest build's duration. Jenkins
// reports duration 0 for a build that is still running, so an in-progress
// job — which its animated color is what actually tells us — says Running
// instead of claiming it finished in 0ms.
func (d InnerJobsDelegate) formatLastBuildDuration(i InnerJobsDelegate) string {
	if i.LastBuild == nil {
		return "N/A"
	}
	if buildstatus.JobColor(i.Color).IsRunning() {
		return "Running"
	}
	return util.FormatDuration(i.LastBuild.Duration)
}

func (d InnerJobsDelegate) getClassIcon(i InnerJobsDelegate) string {
	t := styles.GetTheme()

	if i.isBranch() {
		return t.S().StyledIcons.Branch
	}

	if jobref.IsMultiBranch(i.Class) {
		return t.S().StyledIcons.Branch
	}

	if jobref.IsFolder(i.Class) {
		return t.S().StyledIcons.Folder
	}

	if jobref.IsPipeline(i.Class) {
		return t.S().StyledIcons.WorkflowJobIcon
	}

	return "•"
}

// isBranch reports whether this row is a branch of a MultiBranch Pipeline.
// Jenkins models a branch as an ordinary WorkflowJob, so the only thing that
// distinguishes one is the container it was listed from.
func (d InnerJobsDelegate) isBranch() bool {
	return d.ParentKind == jobref.KindMultiBranch && jobref.IsPipeline(d.Class)
}

func (d InnerJobsDelegate) getClassLabel(i InnerJobsDelegate) string {
	if i.isBranch() {
		return "Branch"
	}

	if jobref.IsMultiBranch(i.Class) {
		return "Multibranch"
	}

	if jobref.IsFolder(i.Class) {
		return "Folder"
	}

	if jobref.IsPipeline(i.Class) {
		return "Pipeline"
	}

	if strings.Contains(i.Class, "FreeStyleProject") {
		return "Freestyle"
	}

	return "Job"
}

func (d InnerJobsDelegate) getStatusLabel(color string) string {
	return buildstatus.JobColor(color).Label()
}
