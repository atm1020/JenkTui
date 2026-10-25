package ui

import (
	"strings"
	"testing"

	"github.com/atm1020/jenktui/src/config"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/buildaction"
	"github.com/atm1020/jenktui/src/ui/components/build"
	"github.com/atm1020/jenktui/src/ui/router"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/spinner"
	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

func buildNavigationApp(t *testing.T) *App {
	t.Helper()
	r := router.NewRouter()
	r.SetInitialView(testViewModel{})
	t.Cleanup(r.CleanUp)
	cfg := config.GetDefaultConfig()
	return &App{
		ctx: &context.ProgramContext{
			Config: cfg, Jenkins: gojenkins.CreateJenkins(nil, "http://jenkins"),
		},
		keyMap: util.UserKeyMap(cfg.KeyMapConfig),
		router: r, wWidth: 100, wHeight: 30,
	}
}

func TestOpenBuildNavigatesImmediatelyFromBothSources(t *testing.T) {
	for _, source := range []string{"my runs", "job"} {
		t.Run(source, func(t *testing.T) {
			app := buildNavigationApp(t)
			msg := build.OpenBuildMsg{JobURL: "http://jenkins/job/demo/", BuildID: "42"}
			if source == "job" {
				msg = build.OpenBuildMsg{PipelineRun: gojenkins.PipelineRun{
					ID: "42", Base: "/job/demo/42", Status: "SUCCESS",
					Job: &gojenkins.Job{
						Jenkins: app.ctx.Jenkins, Base: "/job/demo",
						Raw: &gojenkins.JobResponse{URL: "http://jenkins/job/demo/"},
					},
				}}
			}
			_, cmd := app.Update(msg)
			nav, ok := cmd().(router.NavigateMsg)
			if !ok {
				t.Fatal("opening a build did not immediately emit navigation")
			}
			// Enter the route but do not execute any fetching commands.
			_, _ = app.Update(nav)
			current := app.router.CurrentView()
			if current.ID() != "build:http://jenkins/job/demo/42" || !strings.Contains(current.View(), "Loading") {
				t.Fatalf("destination = %s, want loading Build page", current.ID())
			}
			_, _ = app.Update(tea.KeyPressMsg{Code: 'b', Text: "b"})
			if app.router.CurrentView().ID() != "test" {
				t.Fatal("Back was not usable while loading")
			}
			_ = app.router.Forward()
			if app.router.CurrentView() != current {
				t.Fatal("loading introduced an extra history entry")
			}
		})
	}
}

func TestOpenStoredLogNavigatesImmediatelyToLoadingPage(t *testing.T) {
	app := buildNavigationApp(t)
	app.ctx.CacheDir = t.TempDir()

	_, cmd := app.Update(buildaction.OpenLogMsg{JobURL: "http://jenkins/job/demo/", BuildID: "42"})
	nav, ok := cmd().(router.NavigateMsg)
	if !ok {
		t.Fatal("opening a stored log did not immediately emit navigation")
	}
	// Enter the route but do not execute the download command.
	_, _ = app.Update(nav)
	current := app.router.CurrentView()
	if current.ID() != "log:/job/demo/42" || !strings.Contains(current.View(), "Downloading Job Log #42") {
		t.Fatalf("destination = %s, want loading log page", current.ID())
	}
}

// An invalid ID fails locally, so these navigation/error tests never contact
// Jenkins. Only the finite fetch command is run; spinner loops stay stopped.
func openFailingBuild(t *testing.T, app *App) (*build.Model, build.LoadFailedMsg) {
	t.Helper()
	_, cmd := app.Update(build.OpenBuildMsg{JobURL: "http://jenkins/job/demo/", BuildID: "invalid"})
	nav := cmd().(router.NavigateMsg)
	model := nav.Model.(*build.Model)
	_, initCmd := app.Update(nav)
	var failure build.LoadFailedMsg
	var execute func(tea.Cmd)
	execute = func(cmd tea.Cmd) {
		if cmd == nil {
			return
		}
		switch msg := cmd().(type) {
		case tea.BatchMsg:
			for _, child := range msg {
				execute(child)
			}
		case build.BuildFetchedMsg:
			_, next := model.Update(msg)
			execute(next)
		case build.LoadFailedMsg:
			failure = msg
		case util.CloseDialogMsg:
			t.Fatal("navigation must not schedule a delayed dialog close")
		case spinner.TickMsg:
			// Do not start an animation loop.
		}
	}
	execute(initCmd)
	if failure.Error == nil {
		t.Fatal("invalid build did not produce a load failure")
	}
	return model, failure
}

func TestBuildLoadFailureReturnsToSourceWithErrorDialog(t *testing.T) {
	app := buildNavigationApp(t)
	_, failure := openFailingBuild(t, app)
	_, _ = app.Update(failure)
	if app.router.CurrentView().ID() != "test" {
		t.Fatal("failed load did not return to its source page")
	}
	if app.dialog == nil || !strings.Contains(app.dialog.View(), "Could not load build details") {
		t.Fatal("failed load did not show the existing error dialog")
	}
}

func TestLateBuildFailureDoesNotNavigateOrShowError(t *testing.T) {
	for _, destination := range []string{"source", "same build reentered", "another instance"} {
		t.Run(destination, func(t *testing.T) {
			app := buildNavigationApp(t)
			model, failure := openFailingBuild(t, app)
			_ = app.router.Back()
			switch destination {
			case "same build reentered":
				_ = app.router.Forward()
			case "another instance":
				other := build.NewModelFromURL(app.ctx, "http://jenkins/job/demo/", "invalid", 100, 30)
				if model.ID() != other.ID() {
					t.Fatal("test requires the same route identity")
				}
				app.router.RegisterView(other)
				_ = app.router.NavigateTo(other.ID())
			}
			before := app.router.CurrentView()
			if _, cmd := app.Update(failure); cmd != nil {
				t.Fatal("late failure emitted a command")
			}
			if app.router.CurrentView() != before || app.dialog != nil {
				t.Fatal("late failure changed navigation or opened an error")
			}
		})
	}
}
