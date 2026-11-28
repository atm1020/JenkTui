package build

import (
	stdctx "context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/context"
	"github.com/atm1020/jenktui/src/ui/components/logviewer"
	"github.com/atm1020/jenktui/src/ui/styles"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"charm.land/log/v2"
	"github.com/atm1020/gojenkins"
)

type logMode int

const (
	logModeFull logMode = iota
	logModeStage
	logModeStep
	logModeOutside
)

func (m logMode) keyName() string {
	switch m {
	case logModeFull:
		return "full"
	case logModeStage:
		return "stage"
	case logModeStep:
		return "step"
	case logModeOutside:
		return "outside"
	default:
		return "full"
	}
}

type stepLogCacheEntry struct {
	filePath string
	empty    bool
}

// logPane owns the Build page's inline log viewer across four scopes:
//   - [1] full:    complete build log with instant stage/step line jumping
//   - [2] stage:   de-interleaved lines for the focused stage only
//   - [3] step:    lines for the focused step only (from index or wfapi/log)
//   - [4] outside: pre-stage setup & post-stage errors/stacktraces outside stages
type logPane struct {
	mode   logMode
	zoomed bool

	width  int
	height int

	viewer  *logviewer.Model
	loading bool
	empty   bool
	loadErr error

	stageID   string
	stageName string
	stepName  string
	nodeID    string

	// pendingJumpLine is applied as soon as the full console log's first chunk
	// loads in logviewer, landing the cursor right on the focused stage/step.
	pendingJumpLine int

	// Cached full console log + its progressiveHtml/plain-text LogIndex.
	fullLogPath     string
	fullLogIndex    *LogIndex
	fullLogTerminal bool
	fullLogFetching bool
	fullLogGen      int
	cancelFullLog   stdctx.CancelFunc

	// Per-page step fallback cache (used when progressiveHtml has no step tags).
	stepCache map[string]stepLogCacheEntry

	// Active scoped view fetch/extract generation & cancellation.
	generation int
	cancel     stdctx.CancelFunc
}

func newLogPane(width, height int) *logPane {
	return &logPane{
		mode:      logModeFull,
		width:     max(4, width),
		height:    max(4, height),
		stepCache: make(map[string]stepLogCacheEntry),
	}
}

func (p *logPane) innerSize() (w, h int) {
	return max(1, p.width-2), max(1, p.height-2)
}

func (p *logPane) setSize(width, height int) {
	p.width = max(4, width)
	p.height = max(4, height)
	if p.viewer != nil {
		iw, ih := p.innerSize()
		p.viewer.Resize(iw, ih)
	}
}

func (p *logPane) cancelInFlight() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
}

func (p *logPane) cancelFullInFlight() {
	if p.cancelFullLog != nil {
		p.cancelFullLog()
		p.cancelFullLog = nil
	}
	p.fullLogFetching = false
}

func (p *logPane) cleanUp() tea.Cmd {
	p.cancelInFlight()
	p.cancelFullInFlight()
	p.generation++
	p.fullLogGen++
	p.loading = false
	p.pendingJumpLine = 0
	if p.viewer != nil {
		return p.viewer.CleanUp()
	}
	return nil
}

func (p *logPane) resetViewer() tea.Cmd {
	if p.viewer == nil {
		return nil
	}
	cmd := p.viewer.CleanUp()
	p.viewer = nil
	return cmd
}

func (p *logPane) openFile(ctx *context.ProgramContext, filePath string) tea.Cmd {
	cleanup := p.resetViewer()
	iw, ih := p.innerSize()
	p.viewer = logviewer.NewEmbeddedModel(ctx, iw, ih, filePath)
	p.syncToolbar()
	p.loading = false
	p.empty = false
	p.loadErr = nil
	if cleanup != nil {
		return tea.Batch(cleanup, p.viewer.Init())
	}
	return p.viewer.Init()
}

func (p *logPane) syncToolbar() {
	if p.viewer == nil {
		return
	}
	p.viewer.SetEmbeddedToolbar(p.mode.keyName(), p.zoomed)
}

func (p *logPane) isCapturingInput() bool {
	return p.viewer != nil && p.viewer.IsCapturingInput()
}

func (p *logPane) isAnimating() bool {
	return p.loading
}

// render draws the framed log panel. Static when loaded; re-renders on frames
// only while actively fetching or extracting a log.
func (p *logPane) render(spinnerView string, active bool, buildID string, hasStages bool, isTerminal bool) string {
	t := styles.GetTheme()
	p.syncToolbar()

	note := p.panelNote(buildID)
	right := p.panelRight(spinnerView, t)

	iw, ih := p.innerSize()
	var body string
	switch {
	case p.loading:
		label := p.loadingLabel(buildID)
		text := t.S().Base.Foreground(t.SubtleText).Render(fmt.Sprintf("%s Fetching %s…", spinnerView, label))
		body = lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Center, text)
	case p.loadErr != nil:
		errTitle := t.S().Base.Foreground(t.Error).Bold(true).Render("Failed to load log")
		errDetail := t.S().Base.Foreground(t.SubtleText).Render(p.loadErr.Error())
		hint := t.S().Base.Foreground(t.Grey).Italic(true).Render("Press [ctrl+l] to retry · [1] full · [4] outside")
		msg := lipgloss.JoinVertical(lipgloss.Center, errTitle, errDetail, "", hint)
		body = lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Center, msg)
	case p.empty:
		title := t.S().Base.Foreground(t.Grey).Italic(true).Render(p.emptyLabel())
		hint := t.S().Base.Foreground(t.SubtleText).Render("Press [1] full · [2] stage · [3] step · [4] outside")
		body = lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, title, hint))
	case p.viewer != nil:
		body = p.viewer.View()
	case !hasStages && !isTerminal:
		text := t.S().Base.Foreground(t.Grey).Italic(true).Render("Build in queue — waiting for log output")
		hint := t.S().Base.Foreground(t.SubtleText).Render("Press [1] or [L] to fetch console log")
		body = lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, text, hint))
	default:
		text := t.S().Base.Foreground(t.Grey).Italic(true).Render("Select a stage or step to inspect its log")
		hint := t.S().Base.Foreground(t.SubtleText).Render("[1] full · [2] stage · [3] step · [4] outside · [z] zoom")
		body = lipgloss.Place(iw, ih, lipgloss.Center, lipgloss.Center, lipgloss.JoinVertical(lipgloss.Center, text, hint))
	}

	return renderPanelFrame(panelFrameOpts{
		Title:  "log",
		Note:   note,
		Right:  right,
		Width:  p.width,
		Height: p.height,
		Active: active,
	}, body)
}

func (p *logPane) loadingLabel(buildID string) string {
	switch p.mode {
	case logModeFull:
		return fmt.Sprintf("full console log #%s", buildID)
	case logModeStage:
		if p.stageName != "" {
			return fmt.Sprintf("stage %q log", p.stageName)
		}
		return "stage log"
	case logModeOutside:
		return "outside-stage log"
	default:
		if p.stepName != "" {
			return p.stepName
		}
		return "step log"
	}
}

func (p *logPane) emptyLabel() string {
	switch p.mode {
	case logModeStage:
		return "No log output recorded for this stage"
	case logModeOutside:
		return "No log output outside pipeline stages"
	default:
		return "No log output for this step"
	}
}

func (p *logPane) panelNote(buildID string) string {
	switch p.mode {
	case logModeFull:
		base := "Full Console"
		if buildID != "" {
			base = fmt.Sprintf("Full Console (#%s)", buildID)
		}
		if p.stageName != "" {
			return base + " · " + p.stageName
		}
		return base
	case logModeStage:
		if p.stageName != "" {
			return "Stage: " + p.stageName
		}
		return "Stage"
	case logModeOutside:
		return "Outside Stages (Pre & Post)"
	default:
		if p.stageName != "" && p.stepName != "" {
			return p.stageName + " › " + p.stepName
		}
		if p.stepName != "" {
			return p.stepName
		}
		return p.stageName
	}
}

func (p *logPane) panelRight(spinnerView string, t *styles.Theme) string {
	if p.loading {
		return t.S().Base.Foreground(t.SubtleText).Render(spinnerView + " loading")
	}
	if p.viewer != nil && p.viewer.TotalLines() > 0 {
		label := p.viewer.LineCountLabel()
		if p.zoomed {
			label = "zoomed · " + label
		}
		return t.S().Base.Foreground(t.SubtleText).Render(label)
	}
	if p.zoomed {
		return t.S().Base.Foreground(t.SubtleText).Render("zoomed")
	}
	return ""
}

// loadFullLog downloads and indexes the full build console log via Jenkins'
// `/logText/progressiveHtml?start=0` (with fallback to `/consoleText`).
func (m *Model) loadFullLog(forceRefresh bool) tea.Cmd {
	if m.logPane == nil {
		return nil
	}
	p := m.logPane
	p.loadErr = nil

	if !forceRefresh && p.fullLogPath != "" && p.fullLogTerminal {
		if _, err := os.Stat(p.fullLogPath); err == nil {
			if p.mode == logModeFull {
				if p.viewer != nil && p.viewer.FilePath() == p.fullLogPath {
					return m.jumpFullLogToCurrentSelection()
				}
				p.pendingJumpLine = m.desiredFullLogJumpLine()
				return p.openFile(m.ctx, p.fullLogPath)
			}
			return m.applyCurrentLogScope(false)
		}
		p.fullLogPath = ""
		p.fullLogIndex = nil
		p.fullLogTerminal = false
	}

	if !forceRefresh && p.fullLogFetching {
		return nil
	}

	run := m.pipelineRun
	if run.Job == nil || run.Job.Jenkins == nil {
		return nil
	}

	p.cancelFullInFlight()
	var cleanup tea.Cmd
	if p.mode == logModeFull {
		p.cancelInFlight()
		p.generation++
		cleanup = p.resetViewer()
		p.loading = true
		p.empty = false
		p.loadErr = nil
	}
	p.fullLogGen++
	generation := p.fullLogGen
	p.fullLogFetching = true

	ctx, cancel := m.ctx.DownloadContext()
	p.cancelFullLog = cancel
	cacheDir := m.fullLogsBaseDir()
	terminal := m.isTerminal()

	download := func() tea.Msg {
		defer cancel()
		filePath, idx, err := downloadAndIndexFullLog(ctx, &run, cacheDir)
		return FullLogDownloadedMsg{
			model:      m,
			Generation: generation,
			FilePath:   filePath,
			Index:      idx,
			Terminal:   terminal,
			Error:      err,
		}
	}

	if cleanup != nil {
		return tea.Batch(cleanup, download)
	}
	return download
}

// setLogScope switches the inline log pane to one of the 4 scopes ([1] full,
// [2] stage, [3] step, [4] outside) and refreshes the footer breadcrumb.
func (m *Model) setLogScope(mode logMode, forceRefresh bool) tea.Cmd {
	if m.logPane == nil {
		return nil
	}
	m.logPane.mode = mode
	return tea.Batch(m.applyCurrentLogScope(forceRefresh), m.UpdateFooter())
}

// applyCurrentLogScope renders or jumps the inline log pane for the active
// scope using the cached full-log index whenever possible.
func (m *Model) applyCurrentLogScope(forceRefresh bool) tea.Cmd {
	if m.logPane == nil {
		return nil
	}
	p := m.logPane
	m.syncLogSelectionMetadata()

	switch p.mode {
	case logModeFull:
		if !forceRefresh && p.fullLogPath != "" {
			if p.viewer != nil && p.viewer.FilePath() == p.fullLogPath {
				return m.jumpFullLogToCurrentSelection()
			}
			p.pendingJumpLine = m.desiredFullLogJumpLine()
			return p.openFile(m.ctx, p.fullLogPath)
		}
		return m.loadFullLog(forceRefresh)

	case logModeStage:
		if p.fullLogPath == "" || forceRefresh {
			p.loading = true
			return m.loadFullLog(forceRefresh)
		}
		return m.extractScopedView(logModeStage, p.stageID)

	case logModeOutside:
		if p.fullLogPath == "" || forceRefresh {
			p.loading = true
			return m.loadFullLog(forceRefresh)
		}
		return m.extractScopedView(logModeOutside, "outside")

	default: // logModeStep
		step, ok := m.selectedStep()
		if !ok || step.ID == "" {
			p.cancelInFlight()
			p.loading = false
			p.loadErr = nil
			p.empty = true
			return p.resetViewer()
		}
		// Prefer instant zero-HTTP extraction from the progressiveHtml index.
		if !forceRefresh && p.fullLogPath != "" && p.fullLogIndex != nil && p.fullLogIndex.HasNodeTags {
			if ranges := p.fullLogIndex.StepRanges[step.ID]; len(ranges) > 0 {
				return m.extractScopedView(logModeStep, step.ID)
			}
		}
		return m.loadStepLog(p.stageName, step, forceRefresh)
	}
}

func (m *Model) syncLogSelectionMetadata() {
	if m.logPane == nil {
		return
	}
	if st, ok := m.focusedNavStage(); ok {
		m.logPane.stageID = st.ID
		m.logPane.stageName = st.Name
	} else if m.stageDetails != nil && m.stageDetails.pipelineNode != nil {
		m.logPane.stageID = m.stageDetails.pipelineNode.ID
		m.logPane.stageName = m.stageDetails.pipelineNode.Name
	}
	if !m.stageDetailsStale() {
		if step, ok := m.selectedStep(); ok {
			m.logPane.nodeID = step.ID
			m.logPane.stepName = step.Name
		} else {
			m.logPane.nodeID = ""
			m.logPane.stepName = ""
		}
	}
}

func (m *Model) desiredFullLogJumpLine() int {
	if m.logPane == nil || m.logPane.fullLogIndex == nil {
		return 0
	}
	idx := m.logPane.fullLogIndex
	// Opened directly via 'L' (full log) starts at line 1 unless the user jumps.
	if m.startInFullLog && m.logPane.zoomed {
		return 0
	}
	if !m.stageDetailsStale() {
		if step, ok := m.selectedStep(); ok && step.ID != "" {
			if line, found := idx.JumpLineForStep(step.ID); found {
				return line
			}
		}
	}
	if st, ok := m.focusedNavStage(); ok && st.ID != "" {
		if line, found := idx.JumpLineForStage(st.ID); found {
			return line
		}
	}
	if len(m.pipelineRun.Stages) == 0 {
		if line, found := idx.JumpLineForPostStage(); found {
			return line
		}
	}
	return 0
}

func (m *Model) jumpFullLogToCurrentSelection() tea.Cmd {
	if m.logPane == nil || m.logPane.mode != logModeFull || m.logPane.fullLogIndex == nil {
		return nil
	}
	m.syncLogSelectionMetadata()
	line := m.desiredFullLogJumpLine()
	if line <= 0 {
		return nil
	}
	if m.logPane.viewer == nil || m.logPane.viewer.TotalLines() == 0 {
		m.logPane.pendingJumpLine = line
		return nil
	}
	m.logPane.pendingJumpLine = 0
	return m.logPane.viewer.JumpToLine(line)
}

func (m *Model) extractScopedView(scope logMode, key string) tea.Cmd {
	p := m.logPane
	if p == nil || p.fullLogPath == "" {
		return nil
	}
	idx := p.fullLogIndex
	if idx == nil {
		p.cancelInFlight()
		p.loading = false
		p.loadErr = nil
		p.empty = true
		return p.resetViewer()
	}

	var ranges []LineRange
	switch scope {
	case logModeStage:
		ranges = idx.StageRanges[key]
	case logModeStep:
		ranges = idx.StepRanges[key]
	case logModeOutside:
		if idx.OutsideLineCount() == 0 {
			p.cancelInFlight()
			p.loading = false
			p.loadErr = nil
			p.empty = true
			return p.resetViewer()
		}
	}
	if scope != logModeOutside && len(ranges) == 0 {
		p.cancelInFlight()
		p.loading = false
		p.loadErr = nil
		p.empty = true
		return p.resetViewer()
	}

	p.cancelInFlight()
	cleanup := p.resetViewer()
	p.generation++
	generation := p.generation
	p.loading = true
	p.empty = false
	p.loadErr = nil

	fullPath := p.fullLogPath
	outDir := filepath.Join(m.stepLogsBaseDir(), "scoped", safePathComponent(m.pipelineRun.ID))
	outPath := filepath.Join(outDir, fmt.Sprintf("%s_%s.log", scope.keyName(), safePathComponent(key)))
	rangesCopy := append([]LineRange(nil), ranges...)

	extract := func() tea.Msg {
		var (
			n   int
			err error
		)
		if scope == logModeOutside {
			n, err = extractOutsideStageLog(fullPath, idx, outPath)
		} else {
			n, err = extractLogRanges(fullPath, rangesCopy, outPath)
		}
		return ScopedLogExtractedMsg{
			model:      m,
			Generation: generation,
			Scope:      scope,
			Key:        key,
			FilePath:   outPath,
			Empty:      n == 0 && err == nil,
			Error:      err,
		}
	}
	if cleanup != nil {
		return tea.Batch(cleanup, extract)
	}
	return extract
}

// loadStepLog starts an async wfapi/log fetch (or instant cache hit) for a step.
// Used when progressiveHtml node tags are unavailable or a step wasn't indexed yet.
func (m *Model) loadStepLog(stageName string, step gojenkins.PipelineNode, forceRefresh bool) tea.Cmd {
	if m.logPane == nil || step.ID == "" {
		return nil
	}
	p := m.logPane
	p.mode = logModeStep
	p.stageName = stageName
	p.stepName = step.Name
	p.nodeID = step.ID

	stepTerminal := buildstatus.PipelineStatus(step.Status).IsTerminal(step.StartTime + step.Duration)
	if !forceRefresh && stepTerminal {
		if cached, ok := p.stepCache[step.ID]; ok {
			p.cancelInFlight()
			p.generation++
			p.loadErr = nil
			p.loading = false
			if cached.empty {
				cleanup := p.resetViewer()
				p.empty = true
				return cleanup
			}
			if _, err := os.Stat(cached.filePath); err == nil {
				return p.openFile(m.ctx, cached.filePath)
			}
			delete(p.stepCache, step.ID)
		}
	}

	if m.pipelineRun.Job == nil || m.pipelineRun.Job.Jenkins == nil {
		return nil
	}

	p.cancelInFlight()
	cleanup := p.resetViewer()
	p.generation++
	generation := p.generation
	p.loading = true
	p.empty = false
	p.loadErr = nil

	ctx, cancel := m.ctx.RequestContext()
	p.cancel = cancel
	run := m.pipelineRun
	nodeID := step.ID
	stepName := step.Name
	logsRoot := m.stepLogsBaseDir()

	fetch := func() tea.Msg {
		defer cancel()
		nodeLog, err := run.GetNodeLog(ctx, nodeID)
		if err != nil {
			return StepLogFetchedMsg{
				model:      m,
				Generation: generation,
				NodeID:     nodeID,
				StepName:   stepName,
				Terminal:   stepTerminal,
				Error:      err,
			}
		}
		if nodeLog == nil || nodeLog.Text == "" {
			return StepLogFetchedMsg{
				model:      m,
				Generation: generation,
				NodeID:     nodeID,
				StepName:   stepName,
				Terminal:   stepTerminal,
				Empty:      true,
			}
		}

		dir := filepath.Join(logsRoot, "flow_nodes", safePathComponent(run.ID))
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return StepLogFetchedMsg{model: m, Generation: generation, NodeID: nodeID, StepName: stepName, Error: err}
		}
		if err := os.Chmod(dir, 0o700); err != nil {
			return StepLogFetchedMsg{model: m, Generation: generation, NodeID: nodeID, StepName: stepName, Error: err}
		}

		fileName := filepath.Join(dir, fmt.Sprintf("%s.log", safePathComponent(nodeID)))
		if err := os.WriteFile(fileName, []byte(nodeLog.Text), 0o600); err != nil {
			return StepLogFetchedMsg{model: m, Generation: generation, NodeID: nodeID, StepName: stepName, Error: err}
		}
		if err := os.Chmod(fileName, 0o600); err != nil {
			return StepLogFetchedMsg{model: m, Generation: generation, NodeID: nodeID, StepName: stepName, Error: err}
		}

		return StepLogFetchedMsg{
			model:      m,
			Generation: generation,
			NodeID:     nodeID,
			StepName:   stepName,
			FilePath:   fileName,
			Terminal:   stepTerminal,
		}
	}

	if cleanup != nil {
		return tea.Batch(cleanup, fetch)
	}
	return fetch
}

func (m *Model) handleStepLogFetched(msg StepLogFetchedMsg) tea.Cmd {
	if m.logPane == nil || msg.model != m || msg.Generation != m.logPane.generation {
		return nil
	}
	p := m.logPane
	p.cancelInFlight()
	p.loading = false

	if msg.Error != nil {
		if errors.Is(msg.Error, stdctx.Canceled) {
			return nil
		}
		p.loadErr = msg.Error
		p.empty = false
		return nil
	}

	if msg.Empty {
		p.empty = true
		p.loadErr = nil
		if msg.Terminal {
			p.stepCache[msg.NodeID] = stepLogCacheEntry{empty: true}
		}
		return p.resetViewer()
	}

	if msg.Terminal {
		p.stepCache[msg.NodeID] = stepLogCacheEntry{filePath: msg.FilePath}
	}
	return p.openFile(m.ctx, msg.FilePath)
}

func (m *Model) handleFullLogDownloaded(msg FullLogDownloadedMsg) tea.Cmd {
	if m.logPane == nil || msg.model != m || msg.Generation != m.logPane.fullLogGen {
		return nil
	}
	p := m.logPane
	p.cancelFullInFlight()

	if msg.Error != nil {
		if errors.Is(msg.Error, stdctx.Canceled) {
			return nil
		}
		if p.mode == logModeFull || p.mode == logModeStage || p.mode == logModeOutside {
			p.loading = false
			p.loadErr = msg.Error
			p.empty = false
		}
		return nil
	}

	p.fullLogPath = msg.FilePath
	p.fullLogIndex = msg.Index
	p.fullLogTerminal = msg.Terminal
	m.syncLogSelectionMetadata()

	switch p.mode {
	case logModeFull:
		p.pendingJumpLine = m.desiredFullLogJumpLine()
		return p.openFile(m.ctx, msg.FilePath)
	case logModeStage:
		return m.extractScopedView(logModeStage, p.stageID)
	case logModeOutside:
		return m.extractScopedView(logModeOutside, "outside")
	default:
		// If step mode was waiting or can use the fresh index, leave already-loaded
		// step viewer untouched.
		return nil
	}
}

func (m *Model) handleScopedLogExtracted(msg ScopedLogExtractedMsg) tea.Cmd {
	if m.logPane == nil || msg.model != m || msg.Generation != m.logPane.generation {
		return nil
	}
	p := m.logPane
	p.loading = false
	if msg.Error != nil {
		p.loadErr = msg.Error
		p.empty = false
		return nil
	}
	if msg.Empty {
		p.empty = true
		p.loadErr = nil
		return p.resetViewer()
	}
	return p.openFile(m.ctx, msg.FilePath)
}

func (m *Model) stepLogsBaseDir() string {
	if m.ctx != nil {
		if m.ctx.DataLogsDir != "" {
			return m.ctx.DataLogsDir
		}
		if m.ctx.CacheDir != "" {
			return m.ctx.CacheDir
		}
	}
	return filepath.Join(os.TempDir(), "jenktui-logs")
}

func (m *Model) fullLogsBaseDir() string {
	if m.ctx != nil {
		if m.ctx.CacheDir != "" {
			return m.ctx.CacheDir
		}
		if m.ctx.DataLogsDir != "" {
			return m.ctx.DataLogsDir
		}
	}
	return filepath.Join(os.TempDir(), "jenktui-cache")
}

func downloadFullConsoleLog(ctx stdctx.Context, run *gojenkins.PipelineRun, cacheDir string) (string, error) {
	if err := os.MkdirAll(cacheDir, 0o700); err != nil {
		return "", fmt.Errorf("failed to prepare log cache directory: %w", err)
	}
	sanitized := strings.ReplaceAll(run.Base, "/", "_")
	if sanitized == "" {
		sanitized = "build_" + safePathComponent(run.ID)
	}
	filePath := filepath.Join(cacheDir, sanitized+".log")
	log.Info("Downloading full console log", "cacheDir", cacheDir, "fileName", filePath)

	file, err := run.SaveTextLog(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("download failed: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("failed to close downloaded file: %w", err)
	}
	if err := os.Chmod(filePath, 0o600); err != nil {
		return "", fmt.Errorf("failed to secure downloaded file: %w", err)
	}
	return filePath, nil
}
