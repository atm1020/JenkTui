package ui

import (
	"bytes"
	stdcontext "context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/notification"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/store"
	"github.com/atm1020/jenktui/src/ui/components/folder"
	"github.com/atm1020/jenktui/src/ui/components/footer"
	"github.com/atm1020/jenktui/src/ui/components/header"
	"github.com/atm1020/jenktui/src/ui/router"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

type recordingDesktopNotifier struct {
	title   string
	message string
	options notification.Options
	calls   int
	err     error
}

func (n *recordingDesktopNotifier) Send(title, message string, options notification.Options) error {
	n.title = title
	n.message = message
	n.options = options
	n.calls++
	return n.err
}

type testViewModel struct{}

type namedTestViewModel struct {
	testViewModel
	id string
}

func (m namedTestViewModel) ID() string { return m.id }

func (m namedTestViewModel) Update(tea.Msg) (util.JenkTuiModel, tea.Cmd) { return m, nil }

type recordingPollView struct {
	testViewModel
	snapshots  []poll.Snapshot
	runReloads int
}

func (v *recordingPollView) ApplyPollSnapshot(snapshot poll.Snapshot) tea.Cmd {
	v.snapshots = append(v.snapshots, snapshot)
	return nil
}

func (v *recordingPollView) PollRunsChanged() tea.Cmd {
	v.runReloads++
	return nil
}

func (testViewModel) Init() tea.Cmd { return nil }

func (testViewModel) View() string { return "content" }

func (testViewModel) ID() string { return "test" }

func (testViewModel) Update(tea.Msg) (util.JenkTuiModel, tea.Cmd) { return testViewModel{}, nil }

func (testViewModel) UpdateFooter() tea.Cmd { return nil }

func (testViewModel) CleanUp() tea.Cmd { return nil }

func (testViewModel) KeyMap() help.KeyMap { return nil }

func TestNewJenkinsClientOwnsRequesterClientAndKeepsConfiguration(t *testing.T) {
	server := &context.ServerConfig{
		JenkinsURL: "https://jenkins.example/",
		Username:   "user",
		APIToken:   "token",
	}

	jenkins, client := newJenkinsClient(server, t.TempDir())
	requester, ok := jenkins.Requester.(*gojenkins.Requester)
	if !ok {
		t.Fatalf("requester = %T, want *gojenkins.Requester", jenkins.Requester)
	}
	if requester.Client != client {
		t.Fatal("Jenkins requester does not use the returned HTTP client")
	}
	if client == http.DefaultClient || client.Transport == http.DefaultTransport {
		t.Fatal("factory reused the process-default HTTP client or transport")
	}
	if requester.Base != "https://jenkins.example" {
		t.Errorf("requester base = %q, want trimmed server URL", requester.Base)
	}
	if requester.BasicAuth == nil || requester.BasicAuth.Username != "user" || requester.BasicAuth.Password != "token" {
		t.Fatalf("basic auth = %#v, want configured credentials", requester.BasicAuth)
	}
	if requester.Logger == nil {
		t.Fatal("request logger was not installed")
	}
}

func TestAddCACertificateAddsFileContentsToRequester(t *testing.T) {
	certificate := []byte("CA certificate")
	certPath := filepath.Join(t.TempDir(), "company-ca.pem")
	if err := os.WriteFile(certPath, certificate, 0o600); err != nil {
		t.Fatal(err)
	}
	jenkins, _ := newJenkinsClient(&context.ServerConfig{JenkinsURL: "https://jenkins.example"}, t.TempDir())

	if err := addCACertificate(jenkins, certPath); err != nil {
		t.Fatalf("addCACertificate() error = %v", err)
	}
	requester := jenkins.Requester.(*gojenkins.Requester)
	if !bytes.Equal(requester.CACert, certificate) {
		t.Fatalf("requester CA certificate = %q, want %q", requester.CACert, certificate)
	}
}

func TestAddCACertificateReportsMissingFile(t *testing.T) {
	certPath := filepath.Join(t.TempDir(), "missing-ca.pem")
	jenkins, _ := newJenkinsClient(&context.ServerConfig{JenkinsURL: "https://jenkins.example"}, t.TempDir())

	err := addCACertificate(jenkins, certPath)
	if err == nil || !strings.Contains(err.Error(), certPath) {
		t.Fatalf("addCACertificate() error = %v, want missing path error", err)
	}
}

func TestSelectServerReplacesOwnedHTTPClient(t *testing.T) {
	cfg := config.GetDefaultConfig()
	configDir := t.TempDir()
	dataDir := t.TempDir()
	oldServer := context.ServerConfig{ID: "old", JenkinsURL: "http://old.example", Username: "old-user"}
	oldJenkins, oldClient := newJenkinsClient(&oldServer, t.TempDir())
	r := router.NewRouter()
	r.SetInitialView(testViewModel{})
	app := &App{
		ctx: &context.ProgramContext{
			Jenkins: oldJenkins, ServerConfig: &oldServer, Config: cfg,
			ConfigDir: configDir, DataDir: dataDir, DataLogsDir: t.TempDir(),
			RunStore: store.NewForServer(dataDir, oldServer.ID),
		},
		httpClient: oldClient,
		router:     r,
	}

	newServer := context.ServerConfig{ID: "new", JenkinsURL: "http://new.example", Username: "new-user"}
	_ = app.selectServer(newServer)
	t.Cleanup(func() {
		if app.ctx.RunStore != nil {
			_ = app.ctx.RunStore.Close()
		}
	})

	if app.httpClient == nil || app.httpClient == oldClient {
		t.Fatal("server switch did not install a distinct owned HTTP client")
	}
	requester := app.ctx.Jenkins.Requester.(*gojenkins.Requester)
	if requester.Client != app.httpClient || requester.Base != newServer.JenkinsURL {
		t.Fatalf("replacement requester = %#v, want new server and owned client", requester)
	}
}

func TestHTTPClientLifecycleToleratesNilClient(t *testing.T) {
	if cmd := (&App{}).quit(); cmd == nil {
		t.Fatal("quit returned nil command")
	}

	cfg := config.GetDefaultConfig()
	dataDir := t.TempDir()
	r := router.NewRouter()
	r.SetInitialView(testViewModel{})
	app := &App{
		ctx: &context.ProgramContext{
			Config: cfg, ConfigDir: t.TempDir(), DataDir: dataDir, DataLogsDir: t.TempDir(),
			RunStore: store.NewForServer(dataDir, "old"),
		},
		router: r,
	}
	_ = app.selectServer(context.ServerConfig{ID: "new", JenkinsURL: "http://new.example"})
	if app.ctx.RunStore != nil {
		_ = app.ctx.RunStore.Close()
	}
}

func TestAppRoutesAggregateSnapshotToCurrentView(t *testing.T) {
	manager := poll.NewManager(nil, nil, poll.Config{})
	defer manager.Stop()
	view := &recordingPollView{}
	r := router.NewRouter()
	r.SetInitialView(view)
	app := &App{
		ctx:    &context.ProgramContext{PollManager: manager},
		router: r,
	}
	snapshot := poll.Snapshot{
		Target: poll.Target{Kind: poll.TargetJobWindow, JobURL: "http://jenkins/job/demo/"},
		Runs:   []poll.RunSnapshot{{BuildNumber: 42}},
	}

	model, cmd := app.Update(pollDeliveryMsg{
		manager:  manager,
		delivery: poll.Delivery{Batch: poll.Batch{Snapshots: []poll.Snapshot{snapshot, snapshot}, RunsChanged: true}},
	})

	if model != app {
		t.Fatalf("Update returned model %T, want current app", model)
	}
	if len(view.snapshots) != 2 || view.snapshots[0].Runs[0].BuildNumber != 42 {
		t.Fatalf("routed snapshots = %#v, want two build 42 snapshots", view.snapshots)
	}
	if view.runReloads != 1 {
		t.Fatalf("run reloads = %d, want one per aggregate delivery", view.runReloads)
	}
	if cmd == nil {
		t.Fatal("aggregate delivery did not re-arm the single result receiver")
	}
}

func TestAppIgnoresPollResultFromReplacedManager(t *testing.T) {
	active := poll.NewManager(nil, nil, poll.Config{})
	stale := poll.NewManager(nil, nil, poll.Config{})
	defer active.Stop()
	defer stale.Stop()

	app := &App{ctx: &context.ProgramContext{PollManager: active}}
	model, cmd := app.Update(pollDeliveryMsg{
		manager: stale,
		delivery: poll.Delivery{Batch: poll.Batch{Snapshots: []poll.Snapshot{{
			Target: poll.Target{Kind: poll.TargetJobWindow, JobURL: "http://old-jenkins/job/demo/"},
		}}}},
	})

	if model != app {
		t.Fatalf("Update returned model %T, want the current app", model)
	}
	if cmd != nil {
		t.Fatal("stale poll result returned a command; it could re-arm the replaced manager")
	}
}

// The app relies on tea.View.BackgroundColor (OSC 11) to paint the terminal
// surface instead of per-cell background painting; the view must always carry
// the theme background so unstyled cells render on it.
func TestAppViewSetsThemeBackgroundColor(t *testing.T) {
	styles.SwitchTheme(styles.Github)

	cfg := config.GetDefaultConfig()
	ctx := &context.ProgramContext{Config: cfg}
	r := router.NewRouter()
	r.SetInitialView(testViewModel{})

	app := &App{
		wWidth:  24,
		wHeight: 8,
		header:  header.NewModel(),
		footer:  footer.NewModel(ctx, "user"),
		router:  r,
		help:    help.New(),
	}
	_, _ = app.header.Update(tea.WindowSizeMsg{Width: app.wWidth, Height: app.wHeight})
	_, _ = app.footer.Update(tea.WindowSizeMsg{Width: app.wWidth, Height: app.wHeight})

	view := app.View()

	if view.BackgroundColor != styles.GetTheme().BgBase {
		t.Fatalf("view.BackgroundColor = %v, want theme BgBase %v", view.BackgroundColor, styles.GetTheme().BgBase)
	}
	if !strings.Contains(view.Content, "content") {
		t.Fatalf("view content missing router view output: %q", view.Content)
	}
}

func TestHandleJenkinsInitStartsPollerLifecycle(t *testing.T) {
	cfg := config.GetDefaultConfig()
	app := &App{ctx: &context.ProgramContext{Config: cfg}}

	cmd := app.handleJenkinsInitMsg()
	defer app.ctx.PollManager.Stop()
	if cmd == nil {
		t.Fatal("jenkins init did not arm the aggregate receiver")
	}
	if app.ctx.PollManager == nil {
		t.Fatal("jenkins init did not start the poll manager")
	}
}

// The job page may finish loading before the poll manager's first request.
// Preserve that page's observed build number as the trigger baseline.
func TestLatestKnownBuildNumberUsesJobPageHistoryBeforeTrigger(t *testing.T) {
	job := &gojenkins.Job{Raw: &gojenkins.JobResponse{
		Builds:          []gojenkins.JobBuild{{Number: 143}, {Number: 145}, {Number: 144}},
		LastBuild:       gojenkins.JobBuild{Number: 144},
		NextBuildNumber: 146,
	}}

	if got := poll.KnownBuildBaseline(job); got != 145 {
		t.Fatalf("KnownBuildBaseline() = %d, want 145", got)
	}
}

// Completion notifications are user-facing, so they must translate Jenkins'
// wire value through the same friendly labels as the build views.
func TestDurableCompletionNotifiesBeforeAcknowledgement(t *testing.T) {
	recorder := &recordingDesktopNotifier{}
	cfg := config.GetDefaultConfig()
	cfg.Notification.DesktopEnabled = true
	var acknowledged []poll.CompletionKey
	app := &App{
		ctx:             &context.ProgramContext{Config: cfg},
		desktopNotifier: recorder,
	}
	completion := poll.Completion{
		Key:    poll.CompletionKey{JobURL: "http://jenkins/job/demo/", BuildNumber: 42, Kind: "finished"},
		Status: "SUCCESS",
	}

	msg := app.deliverCompletionCmd(completion, func(_ stdcontext.Context, keys []poll.CompletionKey) error {
		acknowledged = append(acknowledged, keys...)
		return nil
	})()
	if done, ok := msg.(completionDeliveryDoneMsg); !ok || done.err != nil {
		t.Fatalf("delivery result = %#v, want success", msg)
	}
	if recorder.calls != 1 {
		t.Fatalf("desktop calls = %d, want 1", recorder.calls)
	}
	if len(acknowledged) != 1 || acknowledged[0].BuildNumber != 42 {
		t.Fatalf("acknowledged = %#v, want build 42 after notification", acknowledged)
	}
}

func TestFailedDurableNotificationIsReleasedWithoutAcknowledgement(t *testing.T) {
	recorder := &recordingDesktopNotifier{err: errors.New("notifier unavailable")}
	cfg := config.GetDefaultConfig()
	cfg.Notification.DesktopEnabled = true
	var acknowledged []poll.CompletionKey
	ackCalls := 0
	app := &App{ctx: &context.ProgramContext{Config: cfg}, desktopNotifier: recorder}
	completion := poll.Completion{
		Key:    poll.CompletionKey{JobURL: "http://jenkins/job/demo/", BuildNumber: 42, Kind: "finished"},
		Status: "FAILED",
	}

	msg := app.deliverCompletionCmd(completion, func(_ stdcontext.Context, keys []poll.CompletionKey) error {
		ackCalls++
		acknowledged = append(acknowledged, keys...)
		return nil
	})()
	if done, ok := msg.(completionDeliveryDoneMsg); !ok || done.err == nil {
		t.Fatalf("delivery result = %#v, want notifier failure", msg)
	}
	if ackCalls != 1 {
		t.Fatalf("ack calls = %d, want one release call", ackCalls)
	}
	if len(acknowledged) != 0 {
		t.Fatalf("acknowledged = %#v, want no successful keys", acknowledged)
	}
}

func TestBuildCompletionNotificationUsesFriendlyStatus(t *testing.T) {
	manager := poll.NewManager(nil, nil, poll.Config{})
	defer manager.Stop()
	app := &App{
		ctx:    &context.ProgramContext{PollManager: manager},
		router: router.NewRouter(),
	}

	cmd := app.handlePollDelivery(pollDeliveryMsg{
		manager: manager,
		delivery: poll.Delivery{Batch: poll.Batch{Completions: []poll.Completion{{
			Key:    poll.CompletionKey{JobURL: "http://jenkins/job/demo/", BuildNumber: 42, Kind: "finished"},
			Status: "NOT_EXECUTED",
		}}}},
	})
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("completion command = %#v, want two-command batch", msg)
	}

	notification := firstFooterNotification(t, batch[1])
	if !strings.Contains(notification.Message, "Not executed") {
		t.Errorf("notification = %q, want friendly status", notification.Message)
	}
	if strings.Contains(notification.Message, "NOT_EXECUTED") {
		t.Errorf("notification leaks raw Jenkins status: %q", notification.Message)
	}
	if notification.Type != util.NotiSkipped {
		t.Errorf("notification type = %v, want NotiSkipped", notification.Type)
	}
}

func TestBuildCompletionFooterUsesStatusColor(t *testing.T) {
	tests := []struct {
		status string
		want   util.NotiType
	}{
		{status: "SUCCESS", want: util.NotiSuccess},
		{status: "FAILURE", want: util.NotiError},
		{status: "FAILED", want: util.NotiError},
		{status: "NOT_EXECUTED", want: util.NotiSkipped},
		{status: "ABORTED", want: util.NotiSkipped},
	}

	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			manager := poll.NewManager(nil, nil, poll.Config{})
			defer manager.Stop()
			app := &App{
				ctx:    &context.ProgramContext{PollManager: manager},
				router: router.NewRouter(),
			}

			cmd := app.handlePollDelivery(pollDeliveryMsg{
				manager: manager,
				delivery: poll.Delivery{Batch: poll.Batch{Completions: []poll.Completion{{
					Key:    poll.CompletionKey{JobURL: "http://jenkins/job/demo/", BuildNumber: 42, Kind: "finished"},
					Status: buildstatus.PipelineStatus(tt.status),
				}}}},
			})
			msg := cmd()
			batch, ok := msg.(tea.BatchMsg)
			if !ok || len(batch) != 2 {
				t.Fatalf("completion command = %#v, want two-command batch", msg)
			}
			notification := firstFooterNotification(t, batch[1])
			if notification.Type != tt.want {
				t.Errorf("notification type = %v, want %v", notification.Type, tt.want)
			}
		})
	}
}

func TestBuildCompletionSendsConfiguredDesktopNotification(t *testing.T) {
	manager := poll.NewManager(nil, nil, poll.Config{})
	defer manager.Stop()
	recorder := &recordingDesktopNotifier{}
	cfg := config.GetDefaultConfig()
	cfg.Notification.DesktopEnabled = true
	cfg.Notification.SoundEnabled = true
	app := &App{
		ctx:             &context.ProgramContext{Config: cfg, PollManager: manager},
		router:          router.NewRouter(),
		desktopNotifier: recorder,
	}

	cmd := app.handlePollDelivery(pollDeliveryMsg{
		manager: manager,
		delivery: poll.Delivery{Batch: poll.Batch{Completions: []poll.Completion{{
			Key: poll.CompletionKey{
				JobURL:      "http://jenkins/job/DevTeam/job/Frontend/job/mobile-app/",
				BuildNumber: 145,
				Kind:        "finished",
			},
			Status: "SUCCESS",
		}}}},
	})
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("completion command = %#v, want two-command batch", cmd())
	}

	runSequence(t, batch[1])
	if recorder.calls != 1 {
		t.Fatalf("desktop notification calls = %d, want 1", recorder.calls)
	}
	if recorder.title != "mobile-app #145 — Success" {
		t.Errorf("desktop notification title = %q", recorder.title)
	}
	if recorder.message != "DevTeam / Frontend / mobile-app" {
		t.Errorf("desktop notification message = %q", recorder.message)
	}
	if !recorder.options.Sound {
		t.Error("desktop notification sound = false, want true")
	}
	if recorder.options.Alert {
		t.Error("desktop notification alert = true, want false for successful build")
	}
}

func TestFailedBuildCompletionUsesDesktopAlert(t *testing.T) {
	manager := poll.NewManager(nil, nil, poll.Config{})
	defer manager.Stop()
	recorder := &recordingDesktopNotifier{}
	cfg := config.GetDefaultConfig()
	cfg.Notification.DesktopEnabled = true
	cfg.Notification.SoundEnabled = true
	app := &App{
		ctx:             &context.ProgramContext{Config: cfg, PollManager: manager},
		router:          router.NewRouter(),
		desktopNotifier: recorder,
	}

	cmd := app.handlePollDelivery(pollDeliveryMsg{
		manager: manager,
		delivery: poll.Delivery{Batch: poll.Batch{Completions: []poll.Completion{{
			Key: poll.CompletionKey{
				JobURL:      "http://jenkins/job/DevTeam/job/Frontend/job/mobile-app/",
				BuildNumber: 145,
				Kind:        "finished",
			},
			Status: "FAILURE",
		}}}},
	})
	batch := cmd().(tea.BatchMsg)
	runSequence(t, batch[1])

	if recorder.title != "mobile-app #145 — Failed" {
		t.Errorf("desktop notification title = %q", recorder.title)
	}
	if !recorder.options.Alert {
		t.Error("desktop notification alert = false, want true for failed build")
	}
}

func TestBuildCompletionSkipsDesktopNotificationWhenDisabled(t *testing.T) {
	manager := poll.NewManager(nil, nil, poll.Config{})
	defer manager.Stop()
	recorder := &recordingDesktopNotifier{}
	cfg := config.GetDefaultConfig()
	cfg.Notification.SoundEnabled = true
	app := &App{
		ctx:             &context.ProgramContext{Config: cfg, PollManager: manager},
		router:          router.NewRouter(),
		desktopNotifier: recorder,
	}

	cmd := app.handlePollDelivery(pollDeliveryMsg{
		manager: manager,
		delivery: poll.Delivery{Batch: poll.Batch{Completions: []poll.Completion{{
			Key:    poll.CompletionKey{JobURL: "http://jenkins/job/demo/", BuildNumber: 42, Kind: "finished"},
			Status: "SUCCESS",
		}}}},
	})
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("completion command = %#v, want two-command batch", cmd())
	}
	runSequence(t, batch[1])
	if recorder.calls != 0 {
		t.Fatalf("desktop notification calls = %d, want 0", recorder.calls)
	}
}

func firstFooterNotification(t *testing.T, cmd tea.Cmd) util.FooterNotiMsg {
	t.Helper()
	for _, msg := range collectCmdMsgs(cmd) {
		if notification, ok := msg.(util.FooterNotiMsg); ok {
			return notification
		}
	}
	t.Fatal("footer notification not found")
	return util.FooterNotiMsg{}
}

func runSequence(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	_ = collectCmdMsgs(cmd)
}

func collectCmdMsgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	return collectMsgTree(cmd())
}

func collectMsgTree(msg tea.Msg) []tea.Msg {
	if msg == nil {
		return nil
	}
	switch msg := msg.(type) {
	case tea.BatchMsg:
		var out []tea.Msg
		for _, cmd := range msg {
			out = append(out, collectCmdMsgs(cmd)...)
		}
		return out
	}

	value := reflect.ValueOf(msg)
	if value.Kind() == reflect.Slice && value.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
		var out []tea.Msg
		for i := 0; i < value.Len(); i++ {
			out = append(out, collectCmdMsgs(value.Index(i).Interface().(tea.Cmd))...)
		}
		return out
	}
	return []tea.Msg{msg}
}

// TestGlobalNavigationWorksWhileAViewIsLoading covers the seam the folder
// page's own tests can't reach: the app shell withholds its global letter
// shortcuts only from views that report they're capturing input. A container
// page waiting on Jenkins must not be one of them, or Back would be dead
// while the loader spins and the user would be stuck on it.
func TestDefaultForwardBindingDispatchesInsteadOfOpeningFavorites(t *testing.T) {
	cfg := config.GetDefaultConfig()
	ctx := &context.ProgramContext{Config: cfg, RunStore: store.New(t.TempDir())}
	r := router.NewRouter()
	r.SetInitialView(testViewModel{})
	second := namedTestViewModel{id: "second"}
	r.Update(router.NavigateMsg{Model: second})
	r.Back()

	app := &App{ctx: ctx, router: r, keyMap: util.UserKeyMap(cfg.KeyMapConfig)}
	app.handleKeyPressMsg(tea.KeyPressMsg{Code: ']', Text: "]"})
	if got := r.CurrentView().ID(); got != "second" {
		t.Fatalf("after Forward current view = %q, want second", got)
	}
}

func TestGlobalNavigationWorksWhileAViewIsLoading(t *testing.T) {
	cfg := config.GetDefaultConfig()
	ctx := &context.ProgramContext{
		Config:   cfg,
		Jenkins:  gojenkins.CreateJenkins(nil, "http://jenkins"),
		RunStore: store.New(t.TempDir()),
	}

	r := router.NewRouter()
	r.SetInitialView(testViewModel{})

	loadingContainer := folder.NewModel(ctx, 120, 40,
		jobref.Target{URL: "http://jenkins/job/mb/", Name: "mb", Kind: jobref.KindMultiBranch},
		ctx.Jenkins.JobRef("mb"))
	defer func() { _ = loadingContainer.CleanUp() }()

	// NavigateTo runs the view's Init, which puts it into its loading state.
	// The commands it returns are never executed here, so nothing is fetched.
	r.Update(router.NavigateMsg{Model: loadingContainer})
	if r.CurrentView().ID() != loadingContainer.ID() {
		t.Fatalf("current view = %q, want the container page", r.CurrentView().ID())
	}

	app := &App{ctx: ctx, router: r, keyMap: util.UserKeyMap(cfg.KeyMapConfig)}
	app.handleKeyPressMsg(tea.KeyPressMsg{Code: 'b', Text: "b"})

	if got := r.CurrentView().ID(); got != "test" {
		t.Errorf("after Back the current view is %q, want the previous view — global navigation was swallowed by the loading page", got)
	}
}
