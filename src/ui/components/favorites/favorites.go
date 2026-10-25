package favorites

import (
	"fmt"
	"io"
	"strings"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/components/myruns"
	"github.com/atm1020/jenktui/src/ui/router"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const (
	listHPad = 2

	// favoriteItemHeight is the rendered height of a row: a top border
	// line, the content line, and a bottom border line. Selected rows
	// use a visible RoundedBorder and unselected rows a HiddenBorder,
	// but both render 3 lines, so the delegate's Height() must match.
	favoriteItemHeight = 3
	// favoriteItemBorderWidth is the left+right cell width the border
	// itself adds on top of a row's styled content width.
	favoriteItemBorderWidth = 2

	favoriteIconGap = " "
	favoriteNameGap = "  "

	// favoriteRowWrapMargin is slack kept between the row's actual
	// content width and the Width() passed to lipgloss. lipgloss's
	// word-wrap miscounts width when the string contains embedded
	// ANSI-styled substrings (the icon, the colored timestamp) and
	// wraps one word early if content exactly fills the target width;
	// this margin keeps content strictly under that width so the false
	// wrap never triggers.
	favoriteRowWrapMargin = 2
)

// Tab selects which kind of favorite the list shows.
type Tab int

const (
	JobsTab Tab = iota
	FoldersTab
	MultibranchTab
)

type Model struct {
	ctx       *context.ProgramContext
	wWidth    int
	wHeight   int
	keyMap    KeyMap
	favorites []store.FavoriteRecord
	activeTab Tab
	list      *list.Model
}

func NewModel(ctx *context.ProgramContext, width, height int) *Model {
	return &Model{
		ctx:       ctx,
		wWidth:    width,
		wHeight:   height,
		keyMap:    UserKeyMap(ctx.Config.KeyMapConfig.Favorites),
		activeTab: JobsTab,
	}
}

func (m *Model) Init() tea.Cmd {
	return m.loadFavorites()
}

func (m *Model) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.wWidth, m.wHeight = msg.Width, msg.Height
		m.updateListSize()
		return m, nil
	case FavoritesLoadedMsg:
		return m, m.handleFavoritesLoaded(msg)
	case FavoriteDeletedMsg:
		return m, m.loadFavorites()
	case tea.KeyMsg:
		return m, m.handleKeyPress(msg)
	}

	if m.list != nil {
		ll, cmd := m.list.Update(msg)
		m.list = &ll
		return m, cmd
	}
	return m, nil
}

func (m *Model) View() string {
	t := styles.GetTheme()
	if m.list == nil {
		base := t.S().Base
		content := base.Render("Loading favorites...")
		return lipgloss.Place(m.wWidth, m.wHeight, lipgloss.Center, lipgloss.Center, content, lipgloss.WithWhitespaceStyle(base))
	}

	contentWidth := m.contentWidth()
	base := t.S().Base

	jobCount, folderCount, multibranchCount := m.tabCounts()
	tabs := m.renderTabs(t, jobCount, folderCount, multibranchCount)
	sep := base.Foreground(t.Border).Render(strings.Repeat("─", contentWidth))

	rows := []string{tabs, sep, m.renderHeader(t, contentWidth), sep, m.renderListBody(t)}
	body := base.Width(contentWidth).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))

	return lipgloss.Place(
		m.wWidth, m.wHeight,
		lipgloss.Left, lipgloss.Top,
		base.Padding(1, listHPad).Render(body),
	)
}

func (m *Model) ID() string {
	return "favorites"
}

func (m *Model) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl("Favorites")
}

func (m *Model) CleanUp() tea.Cmd {
	return nil
}

func (m *Model) KeyMap() help.KeyMap {
	return m.keyMap
}

// IsCapturingInput reports whether the favorites search field is active so
// the app shell does not intercept global letter shortcuts while typing.
func (m *Model) IsCapturingInput() bool {
	return m.list != nil && m.list.FilterState() == list.Filtering
}

func (m *Model) loadFavorites() tea.Cmd {
	return func() tea.Msg {
		favs, err := m.ctx.RunStore.GetAllFavorites()
		return FavoritesLoadedMsg{Favorites: favs, Error: err}
	}
}

func (m *Model) handleFavoritesLoaded(msg FavoritesLoadedMsg) tea.Cmd {
	if msg.Error != nil {
		return util.CmdHandler(util.FooterNotiMsg{
			Message: fmt.Sprintf("Failed to load favorites: %s", msg.Error),
			Type:    util.NotiError,
		})
	}
	m.favorites = msg.Favorites
	return m.buildList()
}

func (m *Model) handleKeyPress(msg tea.KeyMsg) tea.Cmd {
	if m.list == nil {
		return nil
	}
	if m.list.FilterState() == list.Filtering {
		ll, cmd := m.list.Update(msg)
		m.list = &ll
		return cmd
	}
	if key.Matches(msg, m.keyMap.JobsTab) {
		return m.switchTab(JobsTab)
	}
	if key.Matches(msg, m.keyMap.FoldersTab) {
		return m.switchTab(FoldersTab)
	}
	if key.Matches(msg, m.keyMap.MultibranchTab) {
		return m.switchTab(MultibranchTab)
	}
	if key.Matches(msg, m.keyMap.Enter) {
		item, ok := m.list.SelectedItem().(favoriteItem)
		if !ok {
			return nil
		}
		// The stored kind already tells us whether this is a container, so
		// a favorited folder opens straight into its page.
		return util.CmdHandler(util.OpenJobTargetMsg{Target: jobref.Target{
			URL:  item.record.URL,
			Name: item.record.Name,
			Kind: jobref.KindFromFavoriteKind(item.record.Kind),
		}})
	}
	if key.Matches(msg, m.keyMap.Delete) {
		item, ok := m.list.SelectedItem().(favoriteItem)
		if !ok {
			return nil
		}
		return m.deleteFavorite(item.record.URL)
	}
	if key.Matches(msg, m.keyMap.OpenRuns) {
		return m.openRuns()
	}

	ll, cmd := m.list.Update(msg)
	m.list = &ll
	return cmd
}

func (m *Model) switchTab(tab Tab) tea.Cmd {
	if m.activeTab == tab {
		return nil
	}
	m.activeTab = tab
	return m.buildList()
}

func (m *Model) deleteFavorite(url string) tea.Cmd {
	return func() tea.Msg {
		if err := m.ctx.RunStore.RemoveFavorite(url); err != nil {
			return util.FooterNotiMsg{
				Message: fmt.Sprintf("Failed to remove favorite: %s", err),
				Type:    util.NotiError,
			}
		}
		return FavoriteDeletedMsg{URL: url}
	}
}

// openRuns jumps into My Runs pre-filtered to the selected job's run
// history. Folders have no runs, so it's a no-op with a footer notice on
// the Folders tab.
func (m *Model) openRuns() tea.Cmd {
	if m.activeTab != JobsTab {
		return util.CmdHandler(util.FooterNotiMsg{
			Message: "Containers have no run history",
			Type:    util.NotiWarning,
		})
	}
	item, ok := m.list.SelectedItem().(favoriteItem)
	if !ok {
		return nil
	}
	return util.CmdHandler(router.NavigateMsg{
		Model: myruns.NewModelWithQuery(m.ctx, m.wWidth, m.wHeight, store.RunQuery{
			JobName:   item.record.Name,
			SortBy:    store.RunSortDate,
			Direction: store.SortDescending,
		}),
	})
}

func (m *Model) buildList() tea.Cmd {
	filtered := m.filteredFavorites()
	items := make([]list.Item, len(filtered))
	for i, f := range filtered {
		items[i] = favoriteItem{record: f}
	}

	contentWidth := m.contentWidth()
	listHeight := min(len(items)*favoriteItemHeight+2+4, max(favoriteItemHeight, m.wHeight-8))
	if m.list == nil {
		l := list.New(items, favoriteItemDelegate{}, contentWidth, listHeight)
		l.SetShowStatusBar(false)
		l.SetShowTitle(false)
		l.SetShowHelp(false)
		l.SetFilteringEnabled(true)
		l.KeyMap.Filter = m.keyMap.Search
		m.list = &l
		return nil
	}
	m.list.SetSize(contentWidth, listHeight)
	return m.list.SetItems(items)
}

func (m *Model) filteredFavorites() []store.FavoriteRecord {
	wantKind := "job"
	switch m.activeTab {
	case FoldersTab:
		wantKind = "folder"
	case MultibranchTab:
		wantKind = "multibranch"
	}
	var out []store.FavoriteRecord
	for _, f := range m.favorites {
		if f.Kind == wantKind {
			out = append(out, f)
		}
	}
	return out
}

func (m *Model) tabCounts() (jobs, folders, multibranch int) {
	for _, f := range m.favorites {
		switch f.Kind {
		case "folder":
			folders++
		case "multibranch":
			multibranch++
		default:
			jobs++
		}
	}
	return jobs, folders, multibranch
}

func (m *Model) contentWidth() int {
	return max(1, m.wWidth-(listHPad*2))
}

func (m *Model) updateListSize() {
	if m.list == nil {
		return
	}
	contentWidth := m.contentWidth()
	listHeight := min(len(m.list.Items())*favoriteItemHeight+2+4, max(favoriteItemHeight, m.wHeight-8))
	m.list.SetSize(contentWidth, listHeight)
}

func (m *Model) renderTabs(t *styles.Theme, jobCount, folderCount, multibranchCount int) string {
	base := t.S().Base
	labels := []struct {
		tab  Tab
		text string
	}{
		{JobsTab, fmt.Sprintf("Jobs (%d)", jobCount)},
		{FoldersTab, fmt.Sprintf("Folders (%d)", folderCount)},
		{MultibranchTab, fmt.Sprintf("Multibranch (%d)", multibranchCount)},
	}

	rendered := make([]string, 0, len(labels)*2-1)
	for i, l := range labels {
		if i > 0 {
			rendered = append(rendered, "   ")
		}
		if m.activeTab == l.tab {
			rendered = append(rendered, base.Bold(true).Foreground(t.Primary).Render(l.text))
		} else {
			rendered = append(rendered, base.Foreground(t.SubtleText).Render(l.text))
		}
	}
	return lipgloss.JoinHorizontal(lipgloss.Left, rendered...)
}

// renderHeader renders column labels aligned to the current tab's row
// layout — favoriteRowLayout must be given the same icon the active tab's
// rows use, since icon glyphs are not all the same rendered width.
func (m *Model) renderHeader(t *styles.Theme, contentWidth int) string {
	kind := "job"
	switch m.activeTab {
	case FoldersTab:
		kind = "folder"
	case MultibranchTab:
		kind = "multibranch"
	}
	icon := favoriteKindIcon(t, kind)
	rowWidth := max(0, contentWidth-favoriteItemBorderWidth)
	nameWidth, timestampWidth := favoriteRowLayout(rowWidth, icon)

	labelStyle := t.S().Base.Foreground(t.SubtleText).Bold(true)
	iconSpacer := strings.Repeat(" ", lipgloss.Width(icon))
	nameLabel := labelStyle.Render(formatFavoriteName("NAME", nameWidth))
	updatedLabel := labelStyle.Width(timestampWidth).Align(lipgloss.Right).Render("UPDATED")

	header := lipgloss.JoinHorizontal(lipgloss.Left,
		iconSpacer, favoriteIconGap, nameLabel, favoriteNameGap, updatedLabel,
	)
	// PaddingLeft(1) matches the 1-char left border that HiddenBorder/
	// RoundedBorder adds to each item row.
	return t.S().Base.PaddingLeft(1).Render(header)
}

func (m *Model) renderListBody(t *styles.Theme) string {
	if len(m.filteredFavorites()) == 0 {
		msg := "No favorites yet"
		if len(m.favorites) > 0 {
			switch m.activeTab {
			case JobsTab:
				msg = "No favorite jobs"
			case FoldersTab:
				msg = "No favorite folders"
			case MultibranchTab:
				msg = "No favorite multibranch pipelines"
			}
		}
		return t.S().Base.Foreground(t.SubtleText).Padding(1, 0).Render(msg)
	}
	return m.list.View()
}

type favoriteItem struct {
	record store.FavoriteRecord
}

func (i favoriteItem) FilterValue() string {
	return i.record.Name
}

type favoriteItemDelegate struct{}

func (d favoriteItemDelegate) Height() int  { return favoriteItemHeight }
func (d favoriteItemDelegate) Spacing() int { return 0 }
func (d favoriteItemDelegate) Update(msg tea.Msg, m *list.Model) tea.Cmd {
	return nil
}

func (d favoriteItemDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	fi, ok := item.(favoriteItem)
	if !ok {
		return
	}
	t := styles.GetTheme()
	icon := favoriteKindIcon(t, fi.record.Kind)
	base := t.S().Base
	timestamp := util.FormatStartTime(fi.record.CreatedAt)

	rowWidth := max(0, m.Width()-favoriteItemBorderWidth)
	nameWidth, _ := favoriteRowLayout(rowWidth, icon)
	name := formatFavoriteName(fi.record.Name, nameWidth)

	line := lipgloss.JoinHorizontal(lipgloss.Left,
		icon, favoriteIconGap, name, favoriteNameGap, base.Foreground(t.SubtleText).Render(timestamp),
	)

	var content string
	if m.Index() == index {
		content = base.Width(rowWidth).BorderForeground(t.Primary).Border(lipgloss.RoundedBorder()).Foreground(t.Primary).Render(line)
	} else {
		content = base.Width(rowWidth).Border(lipgloss.HiddenBorder()).Render(line)
	}
	fmt.Fprint(w, content)
}

// favoriteKindIcon returns the styled icon for a favorite's kind. Icon
// glyphs are not all the same rendered width, so callers that need to
// align other content around an icon (favoriteRowLayout, renderHeader)
// must use the icon for the specific kind being laid out.
func favoriteKindIcon(t *styles.Theme, kind string) string {
	switch kind {
	case "folder":
		return t.S().StyledIcons.Folder
	case "multibranch":
		return t.S().StyledIcons.Branch
	default:
		return t.S().StyledIcons.WorkflowJobIcon
	}
}

// favoriteRowLayout computes how a row's content is divided given the
// row's total content width (excluding the border) and the icon it will
// be rendered with: how much width is left for the name field, and the
// timestamp field's fixed width. Shared by Render and renderHeader so
// header labels always align with the rows below them.
func favoriteRowLayout(rowWidth int, icon string) (nameWidth, timestampWidth int) {
	timestampWidth = lipgloss.Width(util.FormatStartTime(0))
	nameWidth = max(0, rowWidth-lipgloss.Width(icon)-lipgloss.Width(favoriteIconGap)-lipgloss.Width(favoriteNameGap)-timestampWidth-favoriteRowWrapMargin)
	return nameWidth, timestampWidth
}

// formatFavoriteName truncates name to fit width, padding with trailing
// spaces so that fixed-width content appended after it (the timestamp)
// lands at a consistent offset regardless of name length.
func formatFavoriteName(name string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(name) > width {
		if width <= 3 {
			name = string([]rune(name)[:width])
		} else {
			name = util.TurncateString(name, width)
		}
	}
	return name + strings.Repeat(" ", max(0, width-lipgloss.Width(name)))
}
