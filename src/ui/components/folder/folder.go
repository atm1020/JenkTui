package folder

import (
	cc "context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/list"
	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
)

// Model is a container page: a Folder, an Organization Folder, or a
// MultiBranch Pipeline, rendered as a listing of its children.
//
// The page opens from what the caller already knew (target) and fills itself
// in from one request against job — it never needs the parent's own payload,
// so job's raw response may legitimately be empty.
type Model struct {
	target  jobref.Target
	job     *gojenkins.Job
	wWidth  int
	wHeight int

	keyMap *KeyMap

	list      *list.Model
	ctx       *context.ProgramContext
	favorites map[string]bool

	// loading is true from the moment the page opens until its child
	// request resolves. Row actions are withheld while it is set; global
	// navigation deliberately is not.
	loading bool
	spinner util.Spinner
	// loadFailed records that the request came back an error. Without it a
	// failed container is indistinguishable from an empty one once the user
	// dismisses the error dialog — both would render as "0 items".
	loadFailed bool

	// generation counts the child requests this page has issued. Re-entering
	// the page (Back/Forward re-runs Init) starts a new one, and only the
	// newest is allowed to write to the list.
	generation int
	// cancelRequest aborts the in-flight child request. Called on cleanup so
	// leaving the page doesn't leave a request running against a page nobody
	// is looking at.
	cancelRequest cc.CancelFunc
}

func NewModel(ctx *context.ProgramContext, wWidth, wHeight int, target jobref.Target, job *gojenkins.Job) *Model {
	const hPad = 2
	contentWidth := wWidth - (hPad * 2)
	widths := computeColumnWidths(contentWidth)

	l := list.New(nil, InnerJobsDelegate{Widths: widths}, contentWidth, folderListHeight(0, wHeight))
	l.SetShowStatusBar(false)
	l.SetShowTitle(false)
	l.SetShowHelp(false)
	l.SetFilteringEnabled(true)

	return &Model{
		target:  target,
		job:     job,
		wWidth:  wWidth,
		wHeight: wHeight,
		keyMap:  UserKeyMap(ctx.Config.KeyMapConfig.Folder),
		list:    &l,
		ctx:     ctx,
		spinner: util.NewSpinner("Loading children..."),
	}
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.loadFavorites(), m.startLoadingChildren())
}

func (m *Model) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m, m.handleWindowSizeMsg(msg)
	case tea.KeyMsg:
		return m, m.handleKeyMsg(msg)
	case FavoritesLoadedMsg:
		return m, m.handleFavoritesLoaded(msg)
	case FavoriteToggledMsg:
		return m, m.handleFavoriteToggled(msg)
	case ChildrenLoadedMsg:
		return m, m.handleChildrenLoaded(msg)
	case spinner.TickMsg:
		return m, m.spinner.Update(msg)
	}

	return m, m.upldateList(msg)
}

func (m Model) View() string {
	t := styles.GetTheme()
	const hPad = 2
	contentWidth := m.wWidth - (hPad * 2)
	widths := computeColumnWidths(contentWidth)

	base := t.S().Base
	title := m.renderTitle(t, base, contentWidth)

	sep := base.Foreground(t.Border).Render(strings.Repeat("─", contentWidth))

	// While the child list is loading the page still shows its known title
	// and columns; only the list area is replaced by the loader, so the page
	// doesn't jump around once children arrive.
	listBody := m.list.View()
	switch {
	case m.loading:
		listBody = base.PaddingLeft(1).Render(m.spinner.View())
	case m.loadFailed:
		listBody = base.PaddingLeft(1).Foreground(t.Error).Render("Could not load children.")
	}

	body := base.Width(contentWidth).Render(
		lipgloss.JoinVertical(lipgloss.Left,
			title,
			sep,
			m.renderHeader(widths),
			sep,
			listBody,
		),
	)

	return lipgloss.Place(
		m.wWidth, m.wHeight,
		lipgloss.Left, lipgloss.Top,
		base.Padding(1, hPad).Render(body),
	)
}

func (m Model) ID() string {
	id := "job:/" + jobref.ToDisplayName(m.target.URL) + "/"
	log.Debug("folder: ID()", "jobURL", m.target.URL, "id", id)
	return id
}

func (m Model) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl(m.target.URL)
}

// CleanUp is called by the router when the page is left. Cancelling here is
// what keeps a slow container from holding a request open behind the user's
// back — and what stops its eventual answer from being treated as live.
func (m *Model) CleanUp() tea.Cmd {
	m.cancelInFlightRequest()
	m.spinner.Stop()
	return nil
}

func (m Model) KeyMap() help.KeyMap {
	return m.keyMap
}

// IsCapturingInput reports whether the list's filter is capturing a query, so the
// app shell withholds its global letter shortcuts (Back "b", Forward "f", MyRuns
// "m", Help "?") rather than firing them into the filter.
func (m *Model) IsCapturingInput() bool {
	return m.list != nil && (*m.list).FilterState() == list.Filtering
}

// renderTitle draws the container's name with an item-count badge. The badge
// is withheld unless the list is genuinely known — "0 items" would read as an
// empty container rather than an unanswered or failed request.
func (m Model) renderTitle(t *styles.Theme, base lipgloss.Style, contentWidth int) string {
	title := t.S().Title.Render(m.target.Name)
	if m.favorites[m.target.URL] {
		title = t.S().StyledIcons.BookMark + title
	}
	titleText := fmt.Sprintf("%s  %s", containerIcon(t, m.target.Kind), title)
	if m.loading || m.loadFailed {
		return base.Width(contentWidth).Render(titleText)
	}

	countBadge := lipgloss.NewStyle().
		Background(t.Primary).
		Foreground(t.BgBase).
		Padding(0, 1).
		Bold(true).
		Render(fmt.Sprintf("%d items", len(m.list.Items())))

	return base.Width(contentWidth-lipgloss.Width(countBadge)).Render(titleText) + countBadge
}

// containerIcon picks the page's own icon from what the container actually
// is, so a MultiBranch Pipeline's header doesn't announce itself as a folder
// while its rows show branch icons two lines below. Organization Folders
// classify as KindFolder and keep the folder icon.
func containerIcon(t *styles.Theme, kind jobref.Kind) string {
	if kind == jobref.KindMultiBranch {
		return t.S().StyledIcons.Branch
	}
	return t.S().StyledIcons.Folder
}

func (m Model) renderHeader(widths ColumnWidths) string {
	t := styles.GetTheme()
	labelStyle := t.S().Base.
		Foreground(t.SubtleText).
		Bold(true)

	// PaddingLeft(1) matches the 1-char left border that HiddenBorder/RoundedBorder adds to each item row
	return t.S().Base.PaddingLeft(1).Render(
		lipgloss.JoinHorizontal(lipgloss.Left,
			labelStyle.Width(widths.Type).Render("TYPE"),
			labelStyle.Width(widths.Name).Render("NAME"),
			labelStyle.Width(widths.Status).Render("STATUS"),
			labelStyle.Width(widths.Weather).Render("W"),
			labelStyle.Width(widths.LastSuccess).Render("LAST SUCCESS"),
			labelStyle.Width(widths.LastFailure).Render("LAST FAILURE"),
			labelStyle.Width(widths.LastDuration).Render("LAST DURATION"),
		),
	)
}

func (m *Model) handleKeyMsg(msg tea.KeyMsg) tea.Cmd {
	// Row actions need a row. Until children arrive there isn't one, so the
	// page ignores its own keys — the app shell's global bindings (Back,
	// Forward, My Runs, …) are handled before this and stay available.
	if m.loading {
		return nil
	}
	// While the list is filtering, let it own every key (typing the query, enter to
	// apply, esc to cancel) instead of intercepting Enter to open a job.
	if m.list != nil && (*m.list).FilterState() == list.Filtering {
		return m.upldateList(msg)
	}
	if key.Matches(msg, m.keyMap.Enter) {
		i, ok := (*m.list).SelectedItem().(InnerJobsDelegate)
		if !ok {
			log.Error("Could not cast selected item to InnerJobsDelegate")
			return nil
		}
		log.Debug("folder: enter pressed on item", "name", i.Name, "url", i.Url, "currentFolderID", m.ID())
		// The row already knows what it is, so a child container opens
		// without being re-classified.
		return util.CmdHandler(util.OpenJobTargetMsg{Target: jobref.Target{
			URL:  i.Url,
			Name: i.Name,
			Kind: jobref.KindFromClass(i.Class),
		}})
	}
	if key.Matches(msg, m.keyMap.Favorite) {
		return m.toggleFavorite()
	}
	return m.upldateList(msg)
}

func (m *Model) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	m.wWidth = msg.Width
	m.wHeight = msg.Height
	if m.list != nil {
		const hPad = 2
		contentWidth := msg.Width - (hPad * 2)
		widths := computeColumnWidths(contentWidth)
		items := m.list.Items()
		for i, item := range items {
			if d, ok := item.(InnerJobsDelegate); ok {
				d.Widths = widths
				items[i] = d
			}
		}
		m.list.SetItems(items)
		m.list.SetSize(contentWidth, folderListHeight(len(items), m.wHeight))
	}
	return nil
}

// folderListHeight sizes the list to fit every row (each rowHeight lines
// tall — see InnerJobsDelegate.Height) plus the surrounding chrome (title,
// badge, two separators, column header — see View), capped to what actually
// fits in the window so it doesn't overflow the pane. Mirrors the pattern in
// myruns.Model.buildList.
func folderListHeight(itemCount, windowHeight int) int {
	const chromeLines = 8 // title + sep + header + sep + outer padding, with a small margin
	wanted := itemCount*rowHeight + chromeLines
	return min(wanted, max(rowHeight, windowHeight-chromeLines))
}

func (m *Model) upldateList(msg tea.Msg) tea.Cmd {
	list, cmd := m.list.Update(msg)
	m.list = &list
	return cmd
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
	m.favorites = make(map[string]bool, len(msg.Favorites))
	for _, f := range msg.Favorites {
		m.favorites[f.URL] = true
	}
	m.applyFavoritesToItems()
	return nil
}

// startLoadingChildren puts the page into its loading state and issues the
// one request that populates it. Called from Init, so re-entering the page
// through Back/Forward starts a fresh generation and abandons whatever the
// previous visit had in flight.
func (m *Model) startLoadingChildren() tea.Cmd {
	m.cancelInFlightRequest()
	m.loading = true
	m.loadFailed = false
	m.spinner.Start()
	return tea.Batch(m.spinner.Init(), m.loadChildren())
}

// loadChildren issues the container's single child request under a context
// this page owns, so it can be cancelled on the way out and can't outlive
// the configured request timeout.
func (m *Model) loadChildren() tea.Cmd {
	reqCtx, cancel := m.ctx.RequestContext()
	m.cancelRequest = cancel
	m.generation++

	// Snapshot everything the command needs: by the time it runs, the model
	// may already have moved on to another generation.
	generation := m.generation
	containerURL := m.target.URL
	job := m.job

	return func() tea.Msg {
		children, err := job.GetDetailedChildren(reqCtx)
		if err != nil {
			err = explainRequestFailure(reqCtx, job, err)
		}
		return ChildrenLoadedMsg{
			ContainerURL: containerURL,
			Generation:   generation,
			Children:     children,
			Error:        err,
		}
	}
}

// explainRequestFailure replaces an unreadable child-request error with one
// that says what Jenkins actually answered. The client decodes the response
// body before it looks at the status, so a Jenkins error page — a 403 from an
// expired session, a 404 for a container that has moved — reaches us as a JSON
// syntax error about the page's first character.
//
// Asking the same URL for the container's class alone is cheap and recovers
// the status that was lost. If that request comes back fine, the original
// failure was a real decoding problem and is passed through untouched.
func explainRequestFailure(ctx cc.Context, job *gojenkins.Job, err error) error {
	if errors.Is(err, cc.Canceled) || errors.Is(err, cc.DeadlineExceeded) {
		return err
	}

	// A *string response is read raw, so the status survives a body that
	// isn't JSON at all.
	var body string
	resp, probeErr := job.Jenkins.Requester.Get(ctx, job.Base+"/api/json", &body,
		map[string]string{"tree": "_class"})
	if probeErr != nil || resp == nil {
		return err
	}
	// The requester has already drained and closed the body.
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode/100 == 2 {
		return err
	}
	return fmt.Errorf("the Jenkins server responded with %s", resp.Status)
}

// cancelInFlightRequest aborts the current child request, if any, and
// releases its context's timer.
func (m *Model) cancelInFlightRequest() {
	if m.cancelRequest != nil {
		m.cancelRequest()
		m.cancelRequest = nil
	}
}

func (m *Model) handleChildrenLoaded(msg ChildrenLoadedMsg) tea.Cmd {
	if msg.ContainerURL != m.target.URL || msg.Generation != m.generation {
		log.Debug("folder: dropping stale children result",
			"resultURL", msg.ContainerURL, "pageURL", m.target.URL,
			"resultGeneration", msg.Generation, "pageGeneration", m.generation)
		return nil
	}
	m.cancelInFlightRequest()

	if msg.Error != nil {
		// Cancellation is this page's own doing — the user left while
		// Jenkins was still answering — and is not something to report.
		if errors.Is(msg.Error, cc.Canceled) {
			log.Debug("folder: child request cancelled", "url", msg.ContainerURL)
			return nil
		}
		m.stopLoading()
		m.loadFailed = true
		return util.CmdHandler(util.ErrorMsg{
			Title: "Could not load children",
			Error: msg.Error,
		})
	}

	m.setChildren(msg.Children)
	m.stopLoading()
	return nil
}

func (m *Model) stopLoading() {
	m.loading = false
	m.spinner.Stop()
}

// setChildren replaces the list with Jenkins' authoritative child list, in
// the order it was returned. An empty response is a real answer — the list
// becomes empty rather than staying on the loader forever.
func (m *Model) setChildren(children []gojenkins.DetailedInnerJob) {
	if m.list == nil {
		return
	}
	const hPad = 2
	contentWidth := m.wWidth - (hPad * 2)
	widths := computeColumnWidths(contentWidth)

	selectedURL := m.selectedURL()

	items := make([]list.Item, 0, len(children))
	for _, c := range children {
		items = append(items, InnerJobsDelegate{
			Class:               c.Class,
			ParentKind:          m.target.Kind,
			Name:                jobref.DecodeName(c.Name),
			Url:                 c.Url,
			Color:               c.Color,
			IsFavorite:          m.favorites[c.Url],
			Buildable:           c.Buildable,
			HealthReport:        c.HealthReport,
			LastSuccessfulBuild: c.LastSuccessfulBuild,
			LastFailedBuild:     c.LastFailedBuild,
			LastBuild:           c.LastBuild,
			Widths:              widths,
		})
	}
	m.list.SetItems(items)
	m.list.SetSize(contentWidth, folderListHeight(len(items), m.wHeight))
	m.restoreSelection(selectedURL)
}

// selectedURL returns the URL of the currently highlighted row, or "" when
// there is none.
func (m *Model) selectedURL() string {
	if m.list == nil {
		return ""
	}
	if d, ok := m.list.SelectedItem().(InnerJobsDelegate); ok {
		return d.Url
	}
	return ""
}

// restoreSelection puts the cursor back on the row the user had highlighted
// before the list was replaced — reloading a container on Back should not
// silently jump them to the top.
//
// When that row is gone (or there was none) the cursor goes to the top:
// SetItems leaves the old index in place, which on a shorter list points
// past the end and leaves nothing selected at all.
func (m *Model) restoreSelection(url string) {
	if m.list == nil {
		return
	}
	if url != "" {
		for i, item := range m.list.Items() {
			if d, ok := item.(InnerJobsDelegate); ok && d.Url == url {
				m.list.Select(i)
				return
			}
		}
	}
	m.list.Select(0)
}

// applyFavoritesToItems refreshes each row's IsFavorite flag from
// m.favorites without disturbing the list's selection or scroll position.
func (m *Model) applyFavoritesToItems() {
	if m.list == nil {
		return
	}
	items := m.list.Items()
	for i, item := range items {
		if d, ok := item.(InnerJobsDelegate); ok {
			d.IsFavorite = m.favorites[d.Url]
			items[i] = d
		}
	}
	m.list.SetItems(items)
}

func (m *Model) toggleFavorite() tea.Cmd {
	wasFavorite := m.favorites[m.target.URL]
	rec := store.FavoriteRecord{
		URL:       m.target.URL,
		Name:      m.target.Name,
		Kind:      m.target.Kind.FavoriteKind(),
		CreatedAt: time.Now().UnixMilli(),
	}
	return func() tea.Msg {
		var err error
		if wasFavorite {
			err = m.ctx.RunStore.RemoveFavorite(rec.URL)
		} else {
			err = m.ctx.RunStore.AddFavorite(rec)
		}
		if err != nil {
			return util.FooterNotiMsg{
				Message: fmt.Sprintf("Failed to update favorite: %s", err),
				Type:    util.NotiError,
			}
		}
		return FavoriteToggledMsg{URL: rec.URL, Favorited: !wasFavorite}
	}
}

func (m *Model) handleFavoriteToggled(msg FavoriteToggledMsg) tea.Cmd {
	if m.favorites == nil {
		m.favorites = make(map[string]bool)
	}
	if msg.Favorited {
		m.favorites[msg.URL] = true
	} else {
		delete(m.favorites, msg.URL)
	}
	m.applyFavoritesToItems()

	message := "Removed from favorites"
	if msg.Favorited {
		message = "Added to favorites"
	}
	return util.CmdHandler(util.FooterNotiMsg{Message: message})
}
