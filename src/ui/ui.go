package ui

import (
	c "context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jenkinsclient"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/notification"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/store"
	colortheme "github.com/atm1020/jenktui/src/theme"
	"github.com/atm1020/jenktui/src/ui/buildaction"
	"github.com/atm1020/jenktui/src/ui/components/build"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/errorpage"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/globalsearch"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/serverselector"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/serversetup"
	"github.com/atm1020/jenktui/src/ui/components/dialogs/themeselector"
	"github.com/atm1020/jenktui/src/ui/components/favorites"
	"github.com/atm1020/jenktui/src/ui/components/flowlogviewer"
	"github.com/atm1020/jenktui/src/ui/components/folder"
	"github.com/atm1020/jenktui/src/ui/components/footer"
	"github.com/atm1020/jenktui/src/ui/components/header"
	"github.com/atm1020/jenktui/src/ui/components/home"
	"github.com/atm1020/jenktui/src/ui/components/job"
	"github.com/atm1020/jenktui/src/ui/components/joblog"
	"github.com/atm1020/jenktui/src/ui/components/myruns"
	"github.com/atm1020/jenktui/src/ui/components/scriptview"
	"github.com/atm1020/jenktui/src/ui/router"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	applog "github.com/atm1020/jenktui/src/log"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
)

const appName = "JenkTui"

type HelpLevel int

const (
	HelpHidden HelpLevel = iota
	HelpLocal
	HelpFull
)

var requiredPlugins = []struct {
	ShortName   string
	DisplayName string
}{
	{"pipeline-stage-step", "Pipeline: Stage Step"},
}

type App struct {
	ctx                        *context.ProgramContext
	wWidth, wHeight, maxHeight int
	keyMap                     util.GlobalKeyMap
	header                     *header.Model
	footer                     *footer.Model
	router                     *router.Router
	dialog                     util.JenkTuiDialogModel
	desktopNotifier            notification.Sender
	httpClient                 *http.Client

	// needsServerSelection is true when no remembered server (or more than one
	// configured server with none remembered) requires the user to pick one
	// via the server selector dialog before the app can connect.
	needsServerSelection bool

	help      help.Model
	helpLevel HelpLevel

	// triggerUserCache memoizes the trigger user per build (key: "jobURL#buildID").
	// findAndSaveUserBuilds runs on every poll snapshot; without this cache it
	// would call GetBuild once per non-terminal run per cycle. The trigger user
	// is immutable for a given build, so caching is sound for the app's lifetime.
	// Sentinel value "" means "build was inspected and is not the current user".
	triggerUserCacheMu  sync.Mutex
	triggerUserCache    map[string]string
	notifiedCompletions map[string]struct{}
}

func newJenkinsClient(server *context.ServerConfig, logsDir string) (*gojenkins.Jenkins, *http.Client) {
	httpClient := jenkinsclient.New(jenkinsclient.Options{})
	jenkins := gojenkins.CreateJenkins(httpClient, server.JenkinsURL, server.Username, server.APIToken)
	jenkins.SetLogger(applog.GoJenkinsLogger(logsDir))
	return jenkins, httpClient
}

func addCACertificate(jenkins *gojenkins.Jenkins, certPath string) error {
	if certPath == "" {
		return nil
	}
	requester := jenkins.Requester.(*gojenkins.Requester)
	if requester.CACert != nil {
		return nil
	}
	caCert, err := os.ReadFile(certPath)
	if err != nil {
		return fmt.Errorf("read CA certificate %q: %w", certPath, err)
	}
	requester.CACert = caCert
	return nil
}

func InitAppModel(subdirs *config.Subdirs) (*App, error) {
	// Get base directories
	configDir, err := config.GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get cache directory: %w", err)
	}

	dataDir, err := config.GetDataDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get data directory: %w", err)
	}

	log.Debug("Directories initialized",
		"configDir", configDir,
		"cacheDir", cacheDir,
		"dataDir", dataDir,
		"dataLogsDir", subdirs.DataLogsDir,
		"dataArtifactsDir", subdirs.DataArtifactsDir)

	// Load server configuration
	servers, err := config.LoadServerConfigs(configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load server configuration: %w", err)
	}

	// Load application configuration
	cfg, err := config.LoadConfig(configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load configuration: %w", err)
	}

	// Reconnect to the last server the user picked, if it's still configured.
	// Otherwise fall back to the first server and let the selector dialog
	// (shown from App.Init) ask the user to pick one.
	var serverCfg *context.ServerConfig
	if len(servers) > 0 {
		serverCfg = &servers[0]
	}
	needsServerSelection := len(servers) > 1
	if cfg.LastServerID != "" {
		if s := findServerByID(servers, cfg.LastServerID); s != nil {
			serverCfg = s
			needsServerSelection = false
		}
	}

	// Initialize the embedded + custom theme catalog before resolving config.
	styles.SetThemeDefinitions(colortheme.LoadAll(configDir))
	themeType := styles.ThemeTypeFromString(cfg.Theme)
	styles.SwitchTheme(themeType)
	log.Debug("Theme initialized from config", "theme", cfg.Theme, "themeType", themeType)

	var jenkins *gojenkins.Jenkins
	var httpClient *http.Client
	var runStore *store.Store
	username := ""
	if serverCfg != nil {
		jenkins, httpClient = newJenkinsClient(serverCfg, subdirs.DataLogsDir)
		if err := addCACertificate(jenkins, serverCfg.CACertPath); err != nil {
			return nil, fmt.Errorf("failed to configure Jenkins server %q: %w", serverCfg.Name, err)
		}
		runStore = store.NewForServer(dataDir, serverCfg.ID)
		username = serverCfg.Username
	}

	ctx := &context.ProgramContext{
		Jenkins:      jenkins,
		ServerConfig: serverCfg,
		Servers:      servers,
		Config:       cfg,
		RunStore:     runStore,
		ConfigDir:    configDir,
		CacheDir:     cacheDir,
		DataDir:      dataDir,

		// Data subdirectories
		DataLogsDir:      subdirs.DataLogsDir,
		DataArtifactsDir: subdirs.DataArtifactsDir,
	}
	r := router.NewRouter()
	r.SetInitialView(home.NewModel(ctx))
	return &App{
		keyMap:               util.UserKeyMap(ctx.Config.KeyMapConfig),
		header:               header.NewModel(),
		footer:               footer.NewModel(ctx, username),
		router:               r,
		ctx:                  ctx,
		help:                 newHelpModel(),
		desktopNotifier:      notification.NewDesktop(),
		httpClient:           httpClient,
		triggerUserCache:     make(map[string]string),
		notifiedCompletions:  make(map[string]struct{}),
		needsServerSelection: needsServerSelection,
	}, nil
}

type setupServersLoadedMsg struct {
	dialog  *serversetup.Dialog
	id      string
	servers []context.ServerConfig
	err     error
}

func (a *App) openServerSetup(id string) tea.Cmd {
	settings := serversetup.Settings{}
	if server := findServerByID(a.ctx.Servers, id); server != nil {
		settings = serversetup.Settings{ID: server.ID, Name: server.Name, URL: server.JenkinsURL, Username: server.Username, CAPath: server.CACertPath}
	}
	cleanup := a.replaceDialog(serversetup.New(a.ctx.ConfigDir, settings, a.wWidth, a.ctx.Config.KeyMapConfig.Dialog.ServerSetup))
	return tea.Batch(cleanup, a.dialog.Init())
}

func (a *App) cancelServerSetup(msg serversetup.CancelMsg) tea.Cmd {
	if a.dialog != msg.Dialog {
		return nil
	}
	if len(a.ctx.Servers) == 0 {
		return a.quit()
	}
	cleanup := a.replaceDialog(a.newServerSelectorDialog())
	return tea.Batch(cleanup, a.dialog.Init())
}

func (a *App) reloadSetupServer(msg serversetup.SavedMsg) tea.Cmd {
	if a.dialog != msg.Dialog {
		return nil
	}
	dir := a.ctx.ConfigDir
	return func() tea.Msg {
		servers, err := config.LoadServerConfigs(dir)
		return setupServersLoadedMsg{dialog: msg.Dialog, id: msg.ID, servers: servers, err: err}
	}
}

func (a *App) connectSetupServer(msg setupServersLoadedMsg) tea.Cmd {
	if a.dialog != msg.dialog {
		return nil
	}
	if msg.err != nil {
		return a.handleError(util.ErrorMsg{Title: "Server configuration", Error: errors.New("cannot reload saved server configuration")})
	}
	server := findServerByID(msg.servers, msg.id)
	if server == nil {
		return nil
	}
	a.ctx.Servers = msg.servers
	a.needsServerSelection = false
	return a.selectServer(*server)
}

// findServerByID returns a pointer to the server with the given ID, or nil if
// none matches.
func findServerByID(servers []context.ServerConfig, id string) *context.ServerConfig {
	for i := range servers {
		if servers[i].ID == id {
			return &servers[i]
		}
	}
	return nil
}

type JenkinsInitMsg struct{}

type themeDefinitionsLoadedMsg struct {
	definitions []colortheme.Definition
}

type pollDeliveryMsg struct {
	manager  *poll.Manager
	delivery poll.Delivery
}

type completionDeliveryDoneMsg struct {
	failed []poll.CompletionKey
	err    error
}

type pollSnapshotConsumer interface {
	ApplyPollSnapshot(poll.Snapshot) tea.Cmd
}

var (
	_ pollSnapshotConsumer = (*job.Model)(nil)
	_ pollSnapshotConsumer = (*build.Model)(nil)
)

type pollRunsChangedConsumer interface {
	PollRunsChanged() tea.Cmd
}

type JenkinsInitErrMsg struct {
	Error error
}

func (a *App) Init() tea.Cmd {
	if len(a.ctx.Servers) == 0 {
		return a.openServerSetup("")
	}
	if a.needsServerSelection {
		cleanup := a.replaceDialog(a.newServerSelectorDialog())
		return tea.Batch(cleanup, a.dialog.Init())
	}
	return tea.Batch(a.router.CurrentView().Init(), a.initJenkinsClient())
}

func (a *App) newServerSelectorDialog() *serverselector.Dialog {
	return serverselector.New(a.ctx.Servers, a.wWidth, a.wHeight, a.ctx.Config.KeyMapConfig.Dialog.ServerSelector)
}

func (a *App) initJenkinsClient() tea.Cmd {
	return func() tea.Msg {
		if err := addCACertificate(a.ctx.Jenkins, a.ctx.ServerConfig.CACertPath); err != nil {
			return JenkinsInitErrMsg{Error: err}
		}
		ctx, cancel := a.ctx.RequestContext()
		defer cancel()
		_, err := a.ctx.Jenkins.Init(ctx)
		if err != nil {
			return JenkinsInitErrMsg{Error: err}
		}
		log.Info("Jenkins client initialized", "version", a.ctx.Jenkins.Version)

		// Check required plugins
		plugins, err := a.ctx.Jenkins.GetPlugins(ctx, 1)
		if err != nil {
			log.Warn("Could not check plugins", "error", err)
			return JenkinsInitMsg{}
		}

		var missing []string
		for _, rp := range requiredPlugins {
			if plugins.Contains(rp.ShortName) == nil {
				missing = append(missing, rp.DisplayName+" ("+rp.ShortName+")")
			}
		}

		if len(missing) > 0 {
			return util.ErrorMsg{
				Title: "Missing Required Plugins",
				Error: fmt.Errorf("the following required plugins are not installed:\n\n• %s\n\ninstall them from Manage Jenkins > Plugins",
					strings.Join(missing, "\n• ")),
			}
		}

		return JenkinsInitMsg{}
	}
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return a, a.handleKeyPressMsg(msg)
	case tea.WindowSizeMsg:
		return a, a.handleWindowResize(msg)
	case util.OpenDialogMsg:
		return a, a.handleOpenDialog(msg)
	case util.CloseDialogMsg:
		return a, a.handleCloseDialog()
	case serverselector.SelectedMsg:
		return a, a.selectServer(msg.Server)
	case serverselector.SetupMsg:
		return a, a.openServerSetup(msg.ID)
	case serversetup.CancelMsg:
		return a, a.cancelServerSetup(msg)
	case serversetup.SavedMsg:
		return a, a.reloadSetupServer(msg)
	case setupServersLoadedMsg:
		return a, a.connectSetupServer(msg)
	case util.ThemePreviewMsg:
		return a, a.router.Update(msg)
	case util.ThemeChangedMsg:
		return a, a.handleThemeChangedMsg(msg)
	case themeDefinitionsLoadedMsg:
		// Filesystem loading happened in the command. Install the in-memory
		// catalog on the update loop, then open the picker at the active theme.
		styles.SetThemeDefinitions(msg.definitions)
		currentTheme := styles.GetCurrentThemeType()
		cleanup := a.replaceDialog(themeselector.NewThemeSelectorDialog(a.ctx, a.wWidth, a.wHeight, currentTheme))
		return a, tea.Batch(cleanup, a.dialog.Init())
	case router.NavigateMsg:
		return a, a.handleNavigate(msg)
	case util.OpenJobTargetMsg:
		return a, util.HandleJobTarget(msg.Target, a.ctx)
	case util.OpenJobComponentMsg:
		return a, a.navigateTo(job.NewModel(a.ctx, a.wWidth, a.wHeight, msg.Job))
	case util.BrowserOpenMsg:
		return a, util.Open(msg.Url)
	case job.OpenScriptViewMsg:
		return a, a.navigateTo(scriptview.NewModel(a.ctx, a.wWidth, a.wHeight, msg.Job, &msg.PipelineRun))
	case footer.UpdateFooterUrlMsg:
		return a, a.handleFooterURL(msg)
	case buildaction.OpenLogMsg:
		return a, a.handleOpenLog(msg)
	case util.OpenJobFolderMsg:
		return a, a.navigateTo(folder.NewModel(a.ctx, a.wWidth, a.wHeight, msg.Target, msg.Job))
	case build.OpenBuildMsg:
		return a, a.handleOpenBuild(msg)
	case build.LoadFailedMsg:
		return a, a.handleBuildLoadFailed(msg)
	case build.OpenLogViewerMsg:
		return a, a.navigateTo(flowlogviewer.NewModel(a.ctx, a.wWidth, a.wHeight, msg.FilePath))
	case build.ReplayMsg:
		return a, a.navigateTo(scriptview.NewModel(a.ctx, a.wWidth, a.wHeight, msg.Job, &msg.PipelineRun))
	case pollDeliveryMsg:
		return a, a.handlePollDeliveryMessage(msg)
	case completionDeliveryDoneMsg:
		return a, a.handleCompletionDeliveryDone(msg)
	case util.RunTriggeredMsg:
		return a, a.handleRunTriggeredMsg(msg)
	case JenkinsInitMsg:
		return a, a.handleJenkinsInitMsg()
	case JenkinsInitErrMsg:
		return a, a.handleJenkinsInitError(msg)
	case util.ErrorMsg:
		return a, a.handleError(msg)
	default:
		return a, a.updateComponents(msg)
	}
}

func (a *App) View() tea.View {
	t := styles.GetTheme()

	var body string
	if current := a.router.CurrentView(); current != nil {
		body = current.View()
	}
	elements := []string{
		a.header.View(),
		body,
		a.footer.View(),
	}

	if a.helpLevel != HelpHidden {
		elements = append(elements, a.help.View(a.helpKeyMap()))
	}

	appView := lipgloss.JoinVertical(
		lipgloss.Left,
		elements...,
	)

	layers := []*lipgloss.Layer{
		lipgloss.NewLayer(appView),
	}

	if a.dialog != nil {
		dialogWidth, _ := a.dialog.Dimensions()
		dialogView := a.dialog.View()

		// Center horizontally, position at top vertically
		col := (a.wWidth - dialogWidth) / 2
		row := 2 // Position near top of window

		layers = append(layers, lipgloss.NewLayer(dialogView).X(col).Y(row))
	}

	comp := lipgloss.NewCompositor(layers...)

	var view tea.View
	view.AltScreen = true
	// BackgroundColor emits OSC 11: the terminal's default background becomes
	// BgBase for the session, so unstyled cells render on the theme surface
	// without any per-cell painting (same model as Charm's crush).
	view.BackgroundColor = t.BgBase
	view.Content = comp.Render()
	return view
}

func (a *App) handleOpenDialog(msg util.OpenDialogMsg) tea.Cmd {
	cleanup := a.replaceDialog(msg.Dialog)
	return tea.Batch(cleanup, a.dialog.Init())
}

func (a *App) handleCloseDialog() tea.Cmd {
	_, wasServerSelector := a.dialog.(*serverselector.Dialog)
	cleanup := a.replaceDialog(nil)
	if wasServerSelector && a.ctx.PollManager == nil {
		// Startup dialog dismissed without a pick — proceed with the
		// already-resolved default server instead of leaving the app
		// with no Jenkins client.
		return tea.Batch(cleanup, a.router.CurrentView().Init(), a.initJenkinsClient())
	}
	return cleanup
}

func (a *App) handleNavigate(msg router.NavigateMsg) tea.Cmd {
	// Close the old dialog before starting the destination. An asynchronous
	// CloseDialogMsg could otherwise dismiss a fast load failure's new dialog.
	cleanup := a.replaceDialog(nil)
	return tea.Batch(cleanup, a.router.Update(msg))
}

// replaceDialog installs next (nil dismisses) after cleaning up the current
// dialog. Every change of a.dialog goes through here, so a replaced dialog
// always releases what it owns.
func (a *App) replaceDialog(next util.JenkTuiDialogModel) tea.Cmd {
	var cleanup tea.Cmd
	if a.dialog != nil {
		cleanup = a.dialog.CleanUp()
	}
	a.dialog = next
	return cleanup
}

func (a *App) handleOpenBuild(msg build.OpenBuildMsg) tea.Cmd {
	if msg.JobURL != "" {
		return a.navigateTo(build.NewModelFromURL(a.ctx, msg.JobURL, msg.BuildID, a.wWidth, a.wHeight))
	}
	return a.navigateTo(build.NewModel(a.ctx, msg.PipelineRun, a.wWidth, a.wHeight))
}

func (a *App) handleOpenLog(msg buildaction.OpenLogMsg) tea.Cmd {
	if msg.PipelineRun != nil {
		return a.navigateTo(joblog.NewModel(a.ctx, a.wWidth, a.wHeight, msg.PipelineRun))
	}
	return a.navigateTo(joblog.NewModelFromURL(a.ctx, a.wWidth, a.wHeight, msg.JobURL, msg.BuildID))
}

func (a *App) handleBuildLoadFailed(msg build.LoadFailedMsg) tea.Cmd {
	current, ok := a.router.CurrentView().(*build.Model)
	if !ok || !current.MatchesLoadFailure(msg) {
		return nil
	}
	// Both state changes happen on the update loop, not in unordered commands.
	back := a.router.Back()
	dialog := a.handleError(util.ErrorMsg{Error: msg.Error, Title: msg.ErrorTitle})
	return tea.Batch(back, dialog)
}

func (a *App) handleFooterURL(msg footer.UpdateFooterUrlMsg) tea.Cmd {
	a.footer.SetCurrentPage(msg.Url)
	return tea.Tick(5*time.Second, func(time.Time) tea.Msg {
		return footer.FooterCleanProcessMsg{}
	})
}

func (a *App) handlePollDeliveryMessage(msg pollDeliveryMsg) tea.Cmd {
	if msg.manager != a.ctx.PollManager {
		return nil
	}
	return a.handlePollDelivery(msg)
}

func (a *App) handleCompletionDeliveryDone(msg completionDeliveryDoneMsg) tea.Cmd {
	for _, key := range msg.failed {
		a.forgetCompletionNotification(key)
	}
	if msg.err != nil {
		log.Warn("completion delivery was not acknowledged", "error", msg.err)
	}
	return nil
}

func (a *App) handleJenkinsInitError(msg JenkinsInitErrMsg) tea.Cmd {
	return util.CmdHandler(util.ErrorMsg{
		Title: "Jenkins connection failed",
		Error: msg.Error,
	})
}

func (a *App) handleError(msg util.ErrorMsg) tea.Cmd {
	log.Error("Error message received in UI:", "title", msg.Title, "error", msg.Error)
	// Show error as a dialog overlay.
	cleanup := a.replaceDialog(errorpage.NewModel(msg.Error, msg.Title, a.wWidth, a.wHeight))
	return tea.Batch(cleanup, a.dialog.Init())
}

func (a *App) navigateTo(model util.JenkTuiModel) tea.Cmd {
	return util.CmdHandler(router.NavigateMsg{Model: model})
}

func (a *App) updateComponents(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd
	if a.dialog != nil {
		var cmd tea.Cmd
		a.dialog, cmd = a.dialog.Update(msg)
		cmds = append(cmds, cmd)
	}

	var cmd tea.Cmd
	a.footer, cmd = a.footer.Update(msg)
	cmds = append(cmds, cmd)
	cmds = append(cmds, a.router.Update(msg))
	return tea.Batch(cmds...)
}

func (a *App) handleKeyPressMsg(msg tea.KeyPressMsg) tea.Cmd {
	var cmds []tea.Cmd

	if a.dialog != nil {
		var cmd tea.Cmd
		a.dialog, cmd = a.dialog.Update(msg)
		return cmd
	}

	// While the focused view is capturing text (e.g. the log search box), don't let
	// the app's global letter shortcuts (Back "b", Forward "f", MyRuns "m",
	// Favorites "g", Help "?") fire into the field — forward straight to the view.
	// Ctrl+C still quits.
	if cur := a.router.CurrentView(); cur != nil {
		if capturer, ok := cur.(interface{ IsCapturingInput() bool }); ok && capturer.IsCapturingInput() {
			if key.Matches(msg, a.keyMap.Quit) {
				return a.quit()
			}
			return a.router.Update(msg)
		}
	}

	if key.Matches(msg, a.keyMap.Quit) {
		return a.quit()
	}

	if key.Matches(msg, a.keyMap.GlobalSearch) {
		cmds = append(cmds, a.replaceDialog(globalsearch.NewGlobalSearchDialogCmp(a.ctx, a.wWidth, a.wHeight)), a.dialog.Init())
	}

	if key.Matches(msg, a.keyMap.ThemeSelector) {
		// Re-scan custom files each time, matching cliamp's picker behavior. The
		// command keeps filesystem work out of Update.
		cmds = append(cmds, loadThemeDefinitions(a.ctx.ConfigDir))
	}

	if key.Matches(msg, a.keyMap.ServerSelector) {
		cmds = append(cmds, a.replaceDialog(a.newServerSelectorDialog()), a.dialog.Init())
	}

	if key.Matches(msg, a.keyMap.MyRuns) {
		model := myruns.NewModel(a.ctx, a.wWidth, a.wHeight)
		if provider, ok := a.router.CurrentView().(interface{ RunHistoryQuery() store.RunQuery }); ok {
			model = myruns.NewModelWithQuery(a.ctx, a.wWidth, a.wHeight, provider.RunHistoryQuery())
		}
		return util.CmdHandler(router.NavigateMsg{Model: model})
	}

	if key.Matches(msg, a.keyMap.Favorites) {
		return util.CmdHandler(router.NavigateMsg{
			Model: favorites.NewModel(a.ctx, a.wWidth, a.wHeight),
		})
	}

	if key.Matches(msg, a.keyMap.Back) {
		return a.router.Back()
	}

	if key.Matches(msg, a.keyMap.Forward) {
		return a.router.Forward()
	}

	if key.Matches(msg, a.keyMap.HelpToggle) {
		if a.helpLevel == HelpHidden {
			a.helpLevel = HelpLocal
		} else {
			a.helpLevel = HelpHidden
		}
		a.help.ShowAll = a.helpLevel != HelpHidden
		return util.CmdHandler(tea.WindowSizeMsg{
			Width:  a.wWidth,
			Height: a.maxHeight,
		})
	}

	if key.Matches(msg, a.keyMap.Help) {
		switch a.helpLevel {
		case HelpLocal:
			a.helpLevel = HelpFull
		case HelpFull:
			a.helpLevel = HelpLocal
		default:
			return nil
		}
		return util.CmdHandler(tea.WindowSizeMsg{
			Width:  a.wWidth,
			Height: a.maxHeight,
		})
	}

	cmds = append(cmds, a.router.Update(msg))
	return tea.Batch(cmds...)
}

func (a *App) handleWindowResize(msg tea.WindowSizeMsg) tea.Cmd {
	var cmds []tea.Cmd
	_, _ = a.header.Update(msg)
	a.footer, _ = a.footer.Update(msg)

	// FIXME Use fix value ?
	headerHeight := lipgloss.Height(a.header.View())
	footerHeight := lipgloss.Height(a.footer.View())
	helperHeight := 0
	if a.helpLevel != HelpHidden {
		helperHeight = lipgloss.Height(a.help.View(a.helpKeyMap()))
	}

	// Floor the body height at 1: on a short terminal (or with help expanded) the
	// chrome can exceed the window height, and a negative value forwarded to every
	// child viewport corrupts their layout. Width is floored the same way so a
	// degenerate terminal degrades instead of producing nonsense geometry.
	a.wHeight = max(1, msg.Height-footerHeight-helperHeight-headerHeight)
	a.wWidth = max(1, msg.Width)
	a.maxHeight = msg.Height

	log.Debug("---------------- Window Resize Event ----------------")
	log.Debug("Window size msg:", "width", msg.Width, "height", msg.Height)
	log.Debug("Calculated window size:", "width", a.wWidth, "height", a.wHeight)
	log.Debug("Header", "height", headerHeight)
	log.Debug("Footer", "height", footerHeight)
	log.Debug("----------------------------------------------------")

	if a.dialog != nil {
		var cmd tea.Cmd
		a.dialog, cmd = a.dialog.Update(tea.WindowSizeMsg{Width: a.wWidth, Height: a.wHeight})
		cmds = append(cmds, cmd)
	}
	cmds = append(cmds, a.router.Update(tea.WindowSizeMsg{Width: a.wWidth, Height: a.wHeight}))
	return tea.Batch(cmds...)
}

func waitForPollDelivery(manager *poll.Manager) tea.Cmd {
	return func() tea.Msg {
		delivery, ok := <-manager.Results()
		if !ok {
			return nil
		}
		return pollDeliveryMsg{manager: manager, delivery: delivery}
	}
}

func (a *App) handlePollDelivery(msg pollDeliveryMsg) tea.Cmd {
	cmds := []tea.Cmd{waitForPollDelivery(msg.manager)}
	currentView := a.router.CurrentView()
	if consumer, ok := currentView.(pollSnapshotConsumer); ok {
		for i := range msg.delivery.Batch.Snapshots {
			cmds = append(cmds, consumer.ApplyPollSnapshot(msg.delivery.Batch.Snapshots[i]))
		}
	}
	for _, failure := range msg.delivery.Batch.Failures {
		log.Warn("poll target failed", "jobURL", failure.Target.JobURL, "buildID", failure.Target.BuildNumber.BuildID(),
			"occurrences", failure.Occurrences, "error", failure.Err)
	}
	if msg.delivery.Batch.RunsChanged {
		if consumer, ok := currentView.(pollRunsChangedConsumer); ok {
			cmds = append(cmds, consumer.PollRunsChanged())
		}
	}
	var notify []poll.Completion
	var alreadyHandled []poll.CompletionKey
	var completionCmds []tea.Cmd
	for i := range msg.delivery.Batch.Completions {
		completion := msg.delivery.Batch.Completions[i]
		if !a.markCompletionNotified(completion.Key.JobURL, completion.Key.BuildNumber) {
			alreadyHandled = append(alreadyHandled, completion.Key)
			continue
		}
		notify = append(notify, completion)
		completionCmds = append(completionCmds, util.CmdHandler(util.FooterNotiMsg{
			Message: fmt.Sprintf("Build #%s completed: %s", completion.Key.BuildNumber.BuildID(), completion.Status.Label()),
			Type:    footerNotiTypeForStatus(completion.Status),
		}))
	}
	if len(notify)+len(alreadyHandled) > 0 {
		completionCmds = append(completionCmds, a.deliverCompletionBatchCmd(notify, alreadyHandled, msg.delivery.Ack))
		cmds = append(cmds, tea.Sequence(completionCmds...))
	}
	return tea.Batch(cmds...)
}

func footerNotiTypeForStatus(status buildstatus.PipelineStatus) util.NotiType {
	switch status.Outcome() {
	case buildstatus.OutcomeSuccess:
		return util.NotiSuccess
	case buildstatus.OutcomeFailure:
		return util.NotiError
	case buildstatus.OutcomeNotBuilt, buildstatus.OutcomeAborted:
		return util.NotiSkipped
	case buildstatus.OutcomeUnstable:
		return util.NotiWarning
	default:
		return util.NotiInfo
	}
}

func (a *App) deliverCompletionCmd(completion poll.Completion, ack func(c.Context, []poll.CompletionKey) error) tea.Cmd {
	return a.deliverCompletionBatchCmd([]poll.Completion{completion}, nil, ack)
}

func (a *App) deliverCompletionBatchCmd(
	completions []poll.Completion,
	alreadyHandled []poll.CompletionKey,
	ack func(c.Context, []poll.CompletionKey) error,
) tea.Cmd {
	return func() tea.Msg {
		successful := append([]poll.CompletionKey(nil), alreadyHandled...)
		var failed []poll.CompletionKey
		var deliveryErr error
		for _, completion := range completions {
			err := error(nil)
			if a.ctx.Config != nil && a.ctx.Config.Notification.DesktopEnabled && a.desktopNotifier != nil {
				jobID, _ := jobref.ToIDAndParents(completion.Key.JobURL)
				err = a.desktopNotifier.Send(
					fmt.Sprintf("%s #%s — %s", jobref.DecodeName(jobID), completion.Key.BuildNumber.BuildID(), completion.Status.Label()),
					plainJobName(completion.Key.JobURL),
					notification.Options{
						Sound: a.ctx.Config.Notification.SoundEnabled,
						Alert: completion.Status.Outcome() == buildstatus.OutcomeFailure,
					},
				)
			}
			if err != nil {
				deliveryErr = errors.Join(deliveryErr, err)
				failed = append(failed, completion.Key)
				continue
			}
			successful = append(successful, completion.Key)
		}
		if ack != nil {
			if err := ack(c.Background(), successful); err != nil {
				deliveryErr = errors.Join(deliveryErr, err)
				failed = append(failed, successful...)
			}
		}
		return completionDeliveryDoneMsg{failed: failed, err: deliveryErr}
	}
}

func (a *App) forgetCompletionNotification(key poll.CompletionKey) {
	delete(a.notifiedCompletions, key.JobURL+"#"+key.BuildNumber.BuildID())
}

func (a *App) markCompletionNotified(jobURL string, buildNumber poll.BuildNumber) bool {
	buildID := buildNumber.BuildID()
	if a.notifiedCompletions == nil {
		a.notifiedCompletions = make(map[string]struct{})
	}
	key := jobURL + "#" + buildID
	if _, exists := a.notifiedCompletions[key]; exists {
		return false
	}
	a.notifiedCompletions[key] = struct{}{}
	return true
}

func plainJobName(jobURL string) string {
	id, parents := jobref.ToIDAndParents(jobURL)
	parts := append(append([]string(nil), parents...), id)
	for i := range parts {
		parts[i] = jobref.DecodeName(parts[i])
	}
	return strings.Join(parts, " / ")
}

func loadThemeDefinitions(configDir string) tea.Cmd {
	return func() tea.Msg {
		return themeDefinitionsLoadedMsg{definitions: colortheme.LoadAll(configDir)}
	}
}

func (a *App) handleThemeChangedMsg(msg util.ThemeChangedMsg) tea.Cmd {
	styles.SwitchTheme(msg.ThemeType)

	a.ctx.Config.Theme = msg.ThemeType.String()
	if err := config.SaveConfig(a.ctx.ConfigDir, a.ctx.Config); err != nil {
		log.Error("Failed to save theme to config", "error", err, "theme", msg.ThemeType.String())
		// Don't fail - theme is still applied in memory
	} else {
		log.Debug("Theme saved to config", "theme", msg.ThemeType.String())
	}

	return tea.Batch(a.replaceDialog(nil), a.router.Update(msg))
}

func (a *App) handleRunTriggeredMsg(msg util.RunTriggeredMsg) tea.Cmd {
	if msg.Job != nil {
		jobURL := msg.Job.GetDetails().URL
		noti := util.CmdHandler(util.FooterNotiMsg{
			Message: fmt.Sprintf("Build triggered, polling started: %s", jobref.ToDisplayName(jobURL)),
		})
		return tea.Batch(noti, a.router.Update(msg))
	}

	return tea.Batch(a.router.Update(msg))
}

func (a *App) quit() tea.Cmd {
	if a.router != nil {
		a.router.CleanUp()
	}
	if a.ctx != nil && a.ctx.PollManager != nil {
		a.ctx.PollManager.Stop()
	}
	jenkinsclient.CloseIdle(a.httpClient)
	return tea.Quit
}

func (a *App) selectServer(server context.ServerConfig) tea.Cmd {
	cleanup := a.replaceDialog(nil)
	if a.ctx.PollManager != nil {
		a.ctx.PollManager.Stop()
		a.ctx.PollManager = nil
	}
	if a.router != nil {
		a.router.CleanUp()
	}
	jenkinsclient.CloseIdle(a.httpClient)
	a.httpClient = nil
	a.ctx.ServerConfig = &server

	a.ctx.Config.LastServerID = server.ID
	if err := config.SaveConfig(a.ctx.ConfigDir, a.ctx.Config); err != nil {
		log.Error("Failed to save last selected server to config", "error", err, "serverID", server.ID)
		// Don't fail - selection still applies for this session
	}

	a.ctx.Jenkins, a.httpClient = newJenkinsClient(&server, a.ctx.DataLogsDir)
	// Close the previous store's SQLite pool; otherwise every server switch
	// leaks a *sql.DB (connections + management goroutines) for the process
	// lifetime.
	if a.ctx.RunStore != nil {
		if err := a.ctx.RunStore.Close(); err != nil {
			log.Error("Failed to close run store on server switch", "error", err)
		}
	}
	a.ctx.RunStore = store.NewForServer(a.ctx.DataDir, server.ID)
	a.footer = footer.NewModel(a.ctx, server.Username)
	a.router = router.NewRouter()
	a.router.SetInitialView(home.NewModel(a.ctx))
	a.triggerUserCache = make(map[string]string)
	a.notifiedCompletions = make(map[string]struct{})

	// The replacement components did not receive the terminal's original resize
	// event. Apply the current dimensions immediately; otherwise the new home
	// view and footer render using their zero-value widths after a server switch.
	_, _ = a.footer.Update(tea.WindowSizeMsg{Width: a.wWidth, Height: a.maxHeight})
	resizeCmd := a.router.Update(tea.WindowSizeMsg{Width: a.wWidth, Height: a.wHeight})
	return tea.Batch(cleanup, a.router.CurrentView().Init(), resizeCmd, a.initJenkinsClient())
}

func (a *App) handleJenkinsInitMsg() tea.Cmd {
	pm := poll.NewManager(a.ctx.Jenkins, a.ctx.RunStore, poll.Config{
		PollInterval:    time.Duration(a.ctx.Config.PollInterval) * time.Second,
		MaxParallelJobs: a.ctx.Config.PollMaxParallelJobs,
		RequestTimeout:  time.Duration(a.ctx.Config.JenkinsRequestTimeoutSeconds) * time.Second,
	})
	a.ctx.PollManager = pm
	pm.Start()

	return waitForPollDelivery(pm)
}

// newHelpModel builds the app-level help model with the app background painted
// onto every sub-style, so the help line's key/desc/separator cells don't leak
// the terminal's default background under the app's solid canvas.
func newHelpModel() help.Model {
	h := help.New()
	return h
}

// helpKeyMap returns the appropriate KeyMap based on the current help level.
func (a *App) helpKeyMap() help.KeyMap {
	var localKM help.KeyMap
	if a.dialog != nil {
		localKM = a.dialog.KeyMap()
	} else {
		localKM = a.router.CurrentView().KeyMap()
	}

	if a.helpLevel == HelpFull {
		return util.MergedKeyMap{Local: localKM, Global: a.keyMap}
	}
	return util.LocalWithHint{Local: localKM, Hint: a.keyMap.Help}
}
