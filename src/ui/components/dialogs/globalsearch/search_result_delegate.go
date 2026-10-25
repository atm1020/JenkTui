package globalsearch

import (
	"fmt"
	"io"
	"strings"

	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/ui/styles"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type SearchDelegate struct {
	wWidth int
}

// breadcrumbSep separates folder segments in a Jenkins full display name.
const breadcrumbSep = " » "

func (d SearchDelegate) Height() int  { return 2 }
func (d SearchDelegate) Spacing() int { return 1 }

func (d SearchDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d SearchDelegate) Render(w io.Writer, m list.Model, index int, ii list.Item) {
	var name, group, url string
	switch i := ii.(type) {
	case SearchResultItem:
		name, group, url = i.Name, i.Group, i.URL
	case LegacySearchResultItem:
		name, group, url = i.Name, "Items", i.URL
	default:
		return
	}

	isSelected := index == m.Index()

	t := styles.GetTheme()

	icon := d.getGroupIcon(group)
	// Non-item URLs (nodes, views, users, manage pages) are dimmed so the
	// user sees they are not openable before pressing Enter. URL shape is
	// the source of truth — Group is a server display string and may be
	// localized — matching the gate HandleJobTarget uses.
	openable := jobref.IsJobURL(url)
	leaf, breadcrumb := splitBreadcrumb(name)
	if breadcrumb == "" {
		breadcrumb = "(root)"
		if !openable {
			breadcrumb = group
		}
	}

	prefix := icon + "  "
	indent := strings.Repeat(" ", lipgloss.Width(prefix))
	textWidth := max(0, d.wWidth-lipgloss.Width(prefix))
	leaf = ansi.Truncate(leaf, textWidth, "…")
	breadcrumb = truncateLeft(breadcrumb, textWidth, "…")

	// Paint each line to the full width so the background never leaves
	// unstyled cells that would leak the terminal's default background.
	titleStyle := lipgloss.NewStyle().Width(d.wWidth)
	if !openable {
		titleStyle = titleStyle.Foreground(t.SubtleText)
	}
	pathStyle := lipgloss.NewStyle().Width(d.wWidth).Foreground(t.SubtleText)
	if isSelected {
		titleStyle = titleStyle.Background(t.SelectedBackground)
		pathStyle = pathStyle.Background(t.SelectedBackground)
		if openable {
			titleStyle = titleStyle.Foreground(t.FgBase)
		}
	}

	fmt.Fprint(w, titleStyle.Render(prefix+leaf)+"\n"+pathStyle.Render(indent+breadcrumb))
}

// splitBreadcrumb splits a Jenkins full display name ("Ops » Monitoring » scan")
// into its leaf ("scan") and folder breadcrumb ("Ops » Monitoring").
func splitBreadcrumb(name string) (leaf, breadcrumb string) {
	idx := strings.LastIndex(name, breadcrumbSep)
	if idx < 0 {
		return name, ""
	}
	return name[idx+len(breadcrumbSep):], name[:idx]
}

// truncateLeft cuts s from the left to fit width, keeping the rightmost
// segments, which are the folders nearest the result.
func truncateLeft(s string, width int, prefix string) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	if width <= lipgloss.Width(prefix) {
		return ansi.Truncate(prefix, width, "")
	}
	return prefix + ansi.TruncateLeft(s, lipgloss.Width(s)-width+lipgloss.Width(prefix), "")
}

func (d SearchDelegate) getGroupIcon(group string) string {
	t := styles.GetTheme()
	switch strings.ToLower(group) {
	case "folder", "folders":
		return t.S().RawIcon.Folder
	case "view", "views":
		return t.S().RawIcon.View
	case "items":
		return t.S().RawIcon.Item
	case "nodes":
		return t.S().RawIcon.Node
	default:
		return "•"
	}
}
