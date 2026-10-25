package util

import (
	c "context"
	"errors"
	"fmt"
	"image/color"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"

	appcontext "github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/ui/styles"

	pocApi "github.com/atm1020/gojenkins"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
)

func CmdHandler(msg tea.Msg) tea.Cmd {
	return func() tea.Msg {
		return msg
	}
}

func CmdFooterUrl(url string) tea.Cmd {
	return CmdHandler(FooterUrlMsg{
		Url: jobref.ToDisplayName(url),
	})
}

func HorizontalLine(width int, color color.Color) string {
	return lipgloss.NewStyle().Foreground(color).Render(strings.Repeat("─", width))
}

// PadWidth pads s to the given width so multi-part layouts (dialog rows) stay
// aligned. Multi-line input is padded per line. The padding is unstyled — the
// surface background comes from OSC 11 (see App.View).
func PadWidth(s string, width int) string {
	return lipgloss.NewStyle().
		Width(width).
		Render(s)
}

// JoinDialogParts pads each part to the width of the widest part and joins
// them with newlines. It returns the body and that content width. Padding to
// the widest part (rather than a fixed width) guarantees no part is wrapped.
// Size the surrounding container to width+frame.
func JoinDialogParts(parts ...string) (body string, width int) {
	for _, p := range parts {
		if pw := lipgloss.Width(p); pw > width {
			width = pw
		}
	}
	for i, p := range parts {
		parts[i] = PadWidth(p, width)
	}
	return strings.Join(parts, "\n"), width
}

func Title(title string, width int) string {
	t := styles.GetTheme()
	char := "╱"
	length := lipgloss.Width(title) + 1
	remainingWidth := width - length
	if remainingWidth > 0 {
		lines := strings.Repeat(char, remainingWidth)
		lines = t.S().Base.Bold(true).Foreground(t.Primary).Render(lines)
		title = lipgloss.JoinHorizontal(lipgloss.Left, t.S().Title.Render(title), lines)
	}
	return title
}

func MakePulseBar(iteration, width int) string {
	position := iteration % (width * 2)
	var bar strings.Builder
	for i := range width {
		distance := abs(position - i)
		if distance < 3 {
			bar.WriteString("█")
		} else if distance < 5 {
			bar.WriteString("▓")
		} else if distance < 7 {
			bar.WriteString("▒")
		} else {
			bar.WriteString("░")
		}
	}
	return bar.String()
}

func GetSpinner(spinner int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	frame := frames[spinner%len(frames)]
	spinnerStyle := lipgloss.NewStyle().Foreground(styles.GetTheme().SpinnerColor).Bold(true)
	return spinnerStyle.Render(frame)
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func Open(url string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		switch runtime.GOOS {
		case "linux":
			cmd = exec.CommandContext(c.Background(), "xdg-open", url)
		case "windows":
			cmd = exec.CommandContext(c.Background(), "rundll32", "url.dll,FileProtocolHandler", url)
		case "darwin":
			cmd = exec.CommandContext(c.Background(), "open", url)
		default:
			return ErrorMsg{
				Title: "Open Browser",
				Error: fmt.Errorf("unsuported runtime %s", runtime.GOOS),
			}
		}

		err := cmd.Start()
		if err != nil {
			return ErrorMsg{
				Title: "Open Browser",
				Error: err,
			}
		}
		return FooterNotiMsg{
			Message: fmt.Sprintf("Opened %s", url),
		}
	}
}

type TickMsg time.Time

func GetTickCmd(loopTime time.Duration) func() tea.Msg {
	return tea.Tick(loopTime, func(t time.Time) tea.Msg {
		return TickMsg(t)
	})
}

// HandleJobTarget is the app's single routing seam: it turns a navigation
// target into the message that opens the right page.
//
// A URL that is not a Jenkins item (a node, view, user, or manage page) is
// rejected immediately with a footer warning, as is a job kind JenkTui has
// no page for (for example Freestyle). Every entry point — global search,
// folder view, favorites, My Runs — goes through this function, so that
// warning is not search-specific.
//
// A target whose Kind is already known is routed on the spot. Containers in
// particular open with no request at all — their page issues one focused
// child query of its own, so a discovery poll here would only re-learn a
// class the caller already told us. Only a genuinely unknown target (a URL
// from global search or a run record) pays for classification first.
func HandleJobTarget(target jobref.Target, program *appcontext.ProgramContext) tea.Cmd {
	api := program.Jenkins
	if !jobref.IsJobURL(target.URL) {
		log.Debug("HandleJobTarget: unsupported destination",
			"reason", "url is not a Jenkins item",
			"url", target.URL,
			"name", target.Name,
			"kind", target.Kind,
		)
		return CmdHandler(unsupportedNavigationMsg(target))
	}

	if target.Kind.IsContainer() {
		id, parentIDs := jobref.ToIDAndParents(target.URL)
		return CmdHandler(OpenJobFolderMsg{
			Target: target,
			Job:    api.JobRef(id, parentIDs...),
		})
	}

	return func() tea.Msg {
		ctx, cancel := program.RequestContext()
		defer cancel()
		id, parentIDs := jobref.ToIDAndParents(target.URL)
		log.Debug("HandleJobTarget: resolving", "url", target.URL, "id", id, "parents", parentIDs)

		job, err := api.GetJob(ctx, id, parentIDs...)
		if err != nil {
			return ErrorMsg{
				Error: err,
				Title: "Could not open job — it may have been deleted",
			}
		}

		resolved := TargetFromJob(api, job)
		log.Debug("HandleJobTarget: resolved job",
			"requestedUrl", target.URL,
			"resolvedUrl", resolved.URL,
			"class", job.GetDetails().Class,
			"kind", resolved.Kind,
		)
		switch {
		case resolved.Kind.IsContainer():
			return OpenJobFolderMsg{Target: resolved, Job: job}
		case resolved.Kind == jobref.KindPipeline:
			return OpenJobComponentMsg{Job: job}
		default:
			log.Debug("HandleJobTarget: unsupported destination",
				"reason", "job kind has no page",
				"requestedUrl", target.URL,
				"resolvedUrl", resolved.URL,
				"name", target.Name,
				"class", job.GetDetails().Class,
				"kind", resolved.Kind,
			)
			if strings.TrimSpace(target.Name) == "" {
				return unsupportedNavigationMsg(resolved)
			}
			return unsupportedNavigationMsg(target)
		}
	}
}

// unsupportedNavigationMsg is the feedback for a target JenkTui has no page
// for. The quoted label prefers the caller-supplied name, then the URL's
// display form, then a generic fallback so an empty search hit still reads.
func unsupportedNavigationMsg(target jobref.Target) FooterNotiMsg {
	label := strings.TrimSpace(target.Name)
	if label == "" {
		label = jobref.ToDisplayName(target.URL)
	}
	if label == "" {
		label = "this item"
	} else {
		label = fmt.Sprintf("%q", label)
	}
	return FooterNotiMsg{
		Message: fmt.Sprintf("Navigation to %s is not supported", label),
		Type:    NotiWarning,
	}
}

// TargetFromJob builds a navigation target from an already-resolved job, so
// a page that holds a polled job can hand downstream navigation the
// classification it already paid for.
func TargetFromJob(api *pocApi.Jenkins, job *pocApi.Job) jobref.Target {
	details := job.GetDetails()
	return jobref.Target{
		URL:  jobref.ToAbsolute(api.Server, details.URL),
		Name: jobref.DecodeName(job.GetName()),
		Kind: jobref.KindFromClass(details.Class),
	}
}

func FormatDuration(duration int64) string {
	d := time.Duration(duration) * time.Millisecond
	if d < time.Second {
		return fmt.Sprintf("%dms", max(int(d.Milliseconds()), 0))
	}
	seconds := int(d.Seconds())
	if seconds < 60 {
		return fmt.Sprintf("%ds", seconds)
	}
	minutes := seconds / 60
	secs := seconds % 60
	return fmt.Sprintf("%dmin %ds", minutes, secs)
}

func FormatStartTime(startTime int64) string {
	t := time.UnixMilli(startTime)
	return t.Format("2006-01-02 15:04")
}

func TurncateString(s string, maxLen int) string {
	var result strings.Builder
	if len(s) <= maxLen {
		return s
	}

	calcMaxlen := maxLen - 3
	for idx, c := range s {
		if idx >= calcMaxlen {
			result.WriteString("...")
			break
		}
		result.WriteRune(c)
	}
	return result.String()
}

// isVersionGreaterOrEqual checks if Jenkins version is >= the specified version
// Version format: "2.516" or "2.516.1"
func IsVersionGreaterOrEqual(currentVersion, targetVersion string) bool {
	if currentVersion == "" {
		return true
	}

	current, err := ParseVersion(currentVersion)
	if err != nil {
		// If parsing fails, assume newer version for safety
		return true
	}

	target, err := ParseVersion(targetVersion)
	if err != nil {
		// If target parsing fails, assume newer version for safety
		return true
	}

	// Compare major version
	if current[0] > target[0] {
		return true
	}
	if current[0] < target[0] {
		return false
	}

	// Compare minor version
	if current[1] > target[1] {
		return true
	}
	if current[1] < target[1] {
		return false
	}

	// Compare patch version (if exists)
	if len(current) > 2 && len(target) > 2 {
		return current[2] >= target[2]
	}

	// Equal up to minor version
	return true
}

// parseVersion extracts major, minor, patch from version string
// Examples: "2.516", "2.440.1", "2.426.3"
// Returns error if version string is invalid
func ParseVersion(version string) ([]int, error) {
	parts := []int{0, 0, 0}

	// Split by dots
	segments := strings.Split(version, ".")

	if len(segments) == 0 {
		return parts, errors.New("invalid version string: empty")
	}

	for i := 0; i < len(segments) && i < 3; i++ {
		val, err := strconv.Atoi(segments[i])
		if err != nil {
			return parts, fmt.Errorf("invalid version segment '%s': %w", segments[i], err)
		}
		parts[i] = val
	}

	return parts, nil
}
