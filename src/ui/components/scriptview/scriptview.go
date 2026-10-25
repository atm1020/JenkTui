package scriptview

import (
	c "context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/poll"
	"github.com/atm1020/jenktui/src/ui/animation"
	"github.com/atm1020/jenktui/src/ui/highlight"
	"github.com/atm1020/jenktui/src/ui/styles"
	"github.com/atm1020/jenktui/src/ui/util"

	"charm.land/bubbles/v2/help"
	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/atm1020/gojenkins"
)

type RawLines *[]string

type RunScriptMsg struct {
	Url string
}

type EditScriptMsg struct {
	Content string
}

type ScriptFetchedMsg struct {
	Generation int
	Content    string
	Error      error
}

type Model struct {
	ctx     *context.ProgramContext
	wWidth  int
	wHeight int

	viewport viewport.Model
	keyMap   KeyMap
	content  string

	loading  bool
	pulseBar *animation.PulseBar

	pipelineRun       *gojenkins.PipelineRun
	job               *gojenkins.Job
	requestGeneration int
	cancelRequest     c.CancelFunc
}

func NewModel(ctx *context.ProgramContext, width, height int, job *gojenkins.Job, run *gojenkins.PipelineRun) *Model {
	return &Model{
		ctx:         ctx,
		wWidth:      width,
		wHeight:     height,
		keyMap:      UserKeyMap(util.UserKeyMap(ctx.Config.KeyMapConfig), ctx.Config.KeyMapConfig.Scriptview),
		job:         job,
		pipelineRun: run,
		loading:     true,
		pulseBar:    animation.New(animation.Settings{Width: 32, FPS: 10}),
	}
}

func appendStyle(content string) []string {
	// Apply syntax highlighting for Groovy
	highlighted := highlight.SyntaxHighlight(content, "script.groovy")

	lines := strings.Split(highlighted, "\n")
	lineNumStyle := styles.GetTheme().S().LineNumber
	lon := len(strconv.Itoa(len(lines) + 1))

	for lineNum, v := range lines {
		lineNumPadding := strings.Repeat(" ", lon-len(strconv.Itoa(lineNum)))
		lines[lineNum] = lineNumStyle.Render(fmt.Sprintf("%d%s  ", lineNum+1, lineNumPadding)) + v
	}

	return lines
}

func (m *Model) Init() tea.Cmd {
	m.loading = true
	return tea.Batch(m.pulseBar.Init(), m.fetchScript())
}

func (m *Model) Update(msg tea.Msg) (util.JenkTuiModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		return m, m.handleWindowSizeMsg(msg)
	case tea.KeyMsg:
		return m, m.handleKeyPressUpdate(msg)
	case ScriptFetchedMsg:
		return m, m.handleScriptFetched(msg)
	case animation.StepMsg:
		return m, m.handleAnimationStep(msg)
	case EditScriptMsg:
		return m, m.handleEditScript(msg)
	default:
		return m, m.updateViewport(msg)
	}
}

func (m *Model) View() string {
	if m.loading {
		return m.renderLoading()
	}

	m.viewport.Style = styles.GetTheme().S().SelectedBorder.
		BorderTop(true).
		BorderBottom(true).
		BorderLeft(false).
		BorderRight(false)
	return m.viewport.View()
}

func (m *Model) ID() string {
	return "script:" + jobref.ToDisplayName(m.job.GetDetails().URL) + "/" + m.pipelineRun.ID
}

func (m *Model) UpdateFooter() tea.Cmd {
	return util.CmdFooterUrl(fmt.Sprintf("%smainScript.groovy", m.job.GetDetails().URL))
}

func (m *Model) CleanUp() tea.Cmd {
	m.cancelInFlightRequest()
	m.loading = false
	m.requestGeneration++
	if m.pulseBar != nil {
		m.pulseBar.Stop()
	}
	return nil
}

func (m *Model) KeyMap() help.KeyMap {
	return m.keyMap
}

func (m *Model) fetchScript() tea.Cmd {
	m.cancelInFlightRequest()
	requestCtx, cancel := m.ctx.RequestContext()
	m.cancelRequest = cancel
	m.requestGeneration++
	generation := m.requestGeneration

	return func() tea.Msg {
		scriptContent, err := m.pipelineRun.GetReplayScript(requestCtx)
		return ScriptFetchedMsg{Generation: generation, Content: scriptContent, Error: err}
	}
}

func (m *Model) cancelInFlightRequest() {
	if m.cancelRequest != nil {
		m.cancelRequest()
		m.cancelRequest = nil
	}
}

func (m *Model) renderLoading() string {
	t := styles.GetTheme()
	base := t.S().Base
	content := base.Render(
		fmt.Sprintf("Loading Replay Script\n\n%s", m.pulseBar.View()),
	)

	return lipgloss.Place(
		m.wWidth,
		m.wHeight,
		lipgloss.Center,
		lipgloss.Center,
		content,
		lipgloss.WithWhitespaceStyle(base),
	)
}

func (m *Model) runScript() tea.Cmd {
	return func() tea.Msg {
		params := map[string]string{}
		params["mainScript"] = m.content
		// Replay gets a deadline but is deliberately not tied to CleanUp:
		// cancelling after Jenkins accepts it could prevent intent persistence.
		ctx, cancel := m.ctx.RequestContext()
		defer cancel()
		jobURL := m.job.GetDetails().URL
		intent, err := poll.NewTriggerIntent(jobURL, poll.KnownBuildBaseline(m.job), time.Now())
		if err != nil {
			return util.ErrorMsg{Error: err, Title: "Failed to prepare replay tracking"}
		}
		res := false
		if m.ctx.PollManager != nil {
			_, err = poll.CoordinateTrigger(ctx, m.ctx.PollManager, intent, func(invokeCtx c.Context) (int64, error) {
				ok, replayErr := m.pipelineRun.Replay(invokeCtx, params)
				res = ok
				if replayErr == nil && !ok {
					replayErr = fmt.Errorf("failed to invoke script job")
				}
				return 0, replayErr
			})
		} else {
			res, err = m.pipelineRun.Replay(ctx, params)
		}
		if err != nil {
			return util.ErrorMsg{Error: err, Title: "Failed to invoke script job"}
		}
		if !res {
			return util.ErrorMsg{Error: fmt.Errorf("failed to invoke script job"), Title: "Failed to invoke script job"}
		}

		return util.RunTriggeredMsg{
			Job: m.job, IntentID: intent.IntentID, Tracked: m.ctx.PollManager != nil,
		}
	}
}

func (m *Model) writeScriptAndEdit() (*exec.Cmd, string, error) {
	file, err := os.CreateTemp(m.ctx.CacheDir, "jenktui-replay-*.groovy")
	if err != nil {
		return nil, "", err
	}
	path := file.Name()
	if _, err := file.WriteString(m.content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return nil, "", err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return nil, "", err
	}
	return editorCmd(path), path, nil
}

func (m *Model) readEditedFile(path string) tea.Msg {
	defer os.Remove(path)
	bytes, err := os.ReadFile(path)
	if err != nil {
		return util.ErrorMsg{
			Error: err,
			Title: "Failed to read edited script file",
		}
	}
	return EditScriptMsg{
		Content: string(bytes),
	}
}

func (m *Model) editScript() tea.Cmd {
	cmd, path, err := m.writeScriptAndEdit()
	if err != nil {
		return util.CmdHandler(util.ErrorMsg{
			Error: err,
			Title: "Failed to write script file",
		})
	}
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			_ = os.Remove(path)
			return util.ErrorMsg{Error: err, Title: "Editor failed"}
		}
		return m.readEditedFile(path)
	})
}

func (m *Model) handleKeyPressMsg(msg tea.KeyMsg) tea.Cmd {
	if key.Matches(msg, m.keyMap.Run) {
		return tea.Batch(
			m.runScript(),
			util.CmdHandler(util.OpenJobTargetMsg{Target: util.TargetFromJob(m.ctx.Jenkins, m.job)}),
		)
	}
	if key.Matches(msg, m.keyMap.Edit) {
		return m.editScript()
	}
	m.viewport, _ = m.viewport.Update(msg)
	return nil
}

func (m *Model) handleWindowSizeMsg(msg tea.WindowSizeMsg) tea.Cmd {
	m.wWidth, m.wHeight = msg.Width, msg.Height
	if !m.loading {
		m.viewport.SetWidth(msg.Width)
		m.viewport.SetHeight(msg.Height)
	}
	return m.updateViewport(msg)
}

func (m *Model) handleKeyPressUpdate(msg tea.KeyMsg) tea.Cmd {
	if m.loading {
		return nil
	}
	return m.handleKeyPressMsg(msg)
}

func (m *Model) handleScriptFetched(msg ScriptFetchedMsg) tea.Cmd {
	if msg.Generation != m.requestGeneration {
		return nil
	}
	m.cancelInFlightRequest()
	if msg.Error != nil {
		m.loading = false
		if m.pulseBar != nil {
			m.pulseBar.Stop()
		}
		if errors.Is(msg.Error, c.Canceled) {
			return nil
		}
		return util.CmdHandler(util.ErrorMsg{Error: msg.Error, Title: "Failed to fetch replay script"})
	}
	m.loading = false
	m.content = msg.Content
	m.viewport = viewport.New(
		viewport.WithHeight(m.wHeight),
		viewport.WithWidth(m.wWidth),
	)
	m.viewport.SetContentLines(appendStyle(m.content))
	return nil
}

func (m *Model) handleAnimationStep(msg animation.StepMsg) tea.Cmd {
	if m.loading {
		return m.pulseBar.Update(msg)
	}
	return m.updateViewport(msg)
}

func (m *Model) handleEditScript(msg EditScriptMsg) tea.Cmd {
	m.content = msg.Content
	m.viewport.SetContentLines(appendStyle(m.content))
	return m.updateViewport(msg)
}

func (m *Model) updateViewport(msg tea.Msg) tea.Cmd {
	if !m.loading {
		m.viewport, _ = m.viewport.Update(msg)
	}
	return nil
}
