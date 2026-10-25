package joblog

import (
	stdctx "context"
	"errors"
	"fmt"
	"strings"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/components/logviewer"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/atm1020/gojenkins"
)

// Model orchestrates download and viewing
type Model struct {
	// State
	downloading bool

	// Components
	viewer   *logviewer.Model
	pulseBar *animation.PulseBar

	// Jenkins context
	pipelineRun *gojenkins.PipelineRun
	downloader  *Downloader

	// Dimensions
	width  int
	height int

	// Context
	ctx                *context.ProgramContext
	downloadGeneration int
	cancelRequest      stdctx.CancelFunc
}

// NewModel creates a new joblog model
func NewModel(ctx *context.ProgramContext, width, height int, pipelineRun *gojenkins.PipelineRun) *Model {
	return &Model{
		ctx:         ctx,
		width:       width,
		height:      height,
		pipelineRun: pipelineRun,
		downloading: true,
		pulseBar:    animation.New(animation.Settings{Width: 32, FPS: 10}),
		downloader:  NewDownloader(pipelineRun, ctx.CacheDir),
	}
}

// NewModelFromURL creates the log loading page from the identifiers persisted
// by My Runs. JobRef builds the Jenkins request handle without doing network
// work before navigation.
func NewModelFromURL(ctx *context.ProgramContext, width, height int, jobURL, buildID string) *Model {
	jobID, parentIDs := jobref.ToIDAndParents(jobURL)
	job := ctx.Jenkins.JobRef(jobID, parentIDs...)
	job.Raw.URL = jobref.ToAbsolute(ctx.Jenkins.Server, jobURL)
	run := &gojenkins.PipelineRun{
		ID:   buildID,
		Base: strings.TrimRight(job.Base, "/") + "/" + buildID,
		Job:  job,
	}
	return NewModel(ctx, width, height, run)
}

// Init initializes the model
func (m *Model) Init() tea.Cmd {
	m.downloading = true
	m.viewer = nil
	return tea.Batch(
		m.pulseBar.Init(),
		m.downloadLog(),
	)
}

// Update handles messages
func (m *Model) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m, m.handleWindowSizeMsg(msg)

	case DownloadCompleteMsg:
		return m, m.handleDownloadComplete(msg)

	case animation.StepMsg:
		return m, m.handleAnimationStep(msg)
	}

	if !m.downloading {
		return m, m.updateViewer(msg)
	}

	return m, nil
}

// View renders the model
func (m *Model) View() string {
	if m.downloading {
		return m.renderDownloadProgress()
	}

	if m.viewer != nil {
		return m.viewer.View()
	}

	return "Initializing..."
}

func (m *Model) ID() string {
	return "log:" + m.pipelineRun.Base
}

func (m *Model) UpdateFooter() tea.Cmd {
	if m.pipelineRun != nil {
		return util.CmdFooterUrl(fmt.Sprintf("%s/console", m.pipelineRun.Base))
	}
	return nil
}

func (m *Model) CleanUp() tea.Cmd {
	m.cancelInFlightRequest()
	m.downloading = false
	m.downloadGeneration++
	if m.pulseBar != nil {
		m.pulseBar.Stop()
	}
	if m.viewer != nil {
		return m.viewer.CleanUp()
	}
	return nil
}

func (m *Model) KeyMap() help.KeyMap {
	if m.viewer != nil {
		return m.viewer.KeyMap()
	}
	return nil
}

// IsCapturingInput delegates to the log viewer (once loaded) so the app shell
// withholds global letter shortcuts while its search box is focused.
func (m *Model) IsCapturingInput() bool {
	return m.viewer != nil && m.viewer.IsCapturingInput()
}

// renderDownloadProgress renders download progress UI
func (m *Model) renderDownloadProgress() string {
	t := styles.GetTheme()
	base := t.S().Base
	content := base.Render(
		fmt.Sprintf("📥 Downloading Job Log #%s\n\n%s",
			m.pipelineRun.ID,
			m.pulseBar.View(),
		),
	)

	return lipgloss.Place(
		m.width,
		m.height,
		lipgloss.Center,
		lipgloss.Center,
		content,
		lipgloss.WithWhitespaceStyle(base),
	)
}

// downloadLog downloads the log file.
func (m *Model) downloadLog() tea.Cmd {
	m.cancelInFlightRequest()
	downloadCtx, cancel := m.ctx.DownloadContext()
	m.cancelRequest = cancel
	m.downloadGeneration++
	generation := m.downloadGeneration

	return func() tea.Msg {
		filePath, lineCount, err := m.downloader.Download(downloadCtx)
		return DownloadCompleteMsg{
			Generation: generation,
			FilePath:   filePath,
			LineCount:  lineCount,
			Error:      err,
		}
	}
}

func (m *Model) cancelInFlightRequest() {
	if m.cancelRequest != nil {
		m.cancelRequest()
		m.cancelRequest = nil
	}
}

func (m *Model) updateViewer(msg tea.Msg) tea.Cmd {
	if m.viewer == nil {
		return nil
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(msg)
	return cmd
}

func (m *Model) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	m.width = msg.Width
	m.height = msg.Height
	return m.updateViewer(msg)
}

func (m *Model) handleAnimationStep(msg animation.StepMsg) tea.Cmd {
	if m.downloading {
		return m.pulseBar.Update(msg)
	}
	return m.updateViewer(msg)
}

func (m *Model) handleDownloadComplete(msg DownloadCompleteMsg) tea.Cmd {
	if msg.Generation != m.downloadGeneration {
		return nil
	}
	m.cancelInFlightRequest()
	if msg.Error != nil {
		m.downloading = false
		if m.pulseBar != nil {
			m.pulseBar.Stop()
		}
		if errors.Is(msg.Error, stdctx.Canceled) {
			return nil
		}
		return util.CmdHandler(util.ErrorMsg{Error: msg.Error, Title: "Log download failed"})
	}

	m.downloading = false
	m.viewer = logviewer.NewModel(m.ctx, m.width, m.height, msg.FilePath)
	return m.viewer.Init()
}
