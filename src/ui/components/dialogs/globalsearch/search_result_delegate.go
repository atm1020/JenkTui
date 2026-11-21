package globalsearch

import (
	"fmt"
	"io"
	"strings"

	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type SearchDelegate struct {
	wWidth        int
	hideSelection bool
}

// breadcrumbSep separates folder segments in a Jenkins full display name.
const breadcrumbSep = " » "

func (d SearchDelegate) Height() int  { return 2 }
func (d SearchDelegate) Spacing() int { return 1 }

func (d SearchDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d SearchDelegate) Render(w io.Writer, m list.Model, index int, ii list.Item) {
	var name, group, url, favoriteParent string
	var kind jobref.Kind
	switch i := ii.(type) {
	case SearchResultItem:
		name, group, url = i.Name, i.Group, i.URL
		kind = jobref.KindFromClass(i.Type)
	case LegacySearchResultItem:
		name, group, url = i.Name, "Items", i.URL
	case favoriteItem:
		name, group, url = jobref.DecodeName(i.leaf), "Items", i.URL
		kind, favoriteParent = i.target().Kind, i.parent
		if name == "" {
			name = i.Name
		}
	default:
		return
	}

	isSelected := !d.hideSelection && index == m.Index()

	t := styles.GetTheme()

	icon := d.getGroupIcon(group)
	favorite, saved := ii.(favoriteItem)
	// Non-item URLs (nodes, views, users, manage pages) are dimmed so the
	// user sees they are not openable before pressing Enter. URL shape is
	// the source of truth — Group is a server display string and may be
	// localized — matching the gate HandleJobTarget uses.
	openable := jobref.IsJobURL(url)
	switch kind {
	case jobref.KindFolder:
		icon = t.S().RawIcon.Folder
	case jobref.KindMultiBranch:
		icon = styles.BranchIcon
	case jobref.KindPipeline:
		icon = styles.WorkflowJobIcon
	default:
		if saved && favorite.Kind == "job" {
			icon = styles.WorkflowJobIcon
		}
	}
	leaf, breadcrumb := splitBreadcrumb(name)
	if favoriteParent != "" {
		breadcrumb = favoriteParent
	}
	if breadcrumb == "" {
		breadcrumb = "(root)"
		if !openable {
			breadcrumb = group
		}
	}

	// Keep names aligned even when icons occupy different numbers of cells.
	prefix := ansi.Truncate(util.PadWidth(strings.TrimSpace(icon), 2)+" ", d.wWidth, "")
	prefixWidth := lipgloss.Width(prefix)
	indent := strings.Repeat(" ", prefixWidth)
	textWidth := max(0, d.wWidth-prefixWidth)
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
