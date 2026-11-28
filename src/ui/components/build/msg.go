package build

import (
	"github.com/atm1020/jenktui/src/stagegraph"

	"github.com/atm1020/gojenkins"
)

// OpenBuildMsg is sent when the build view should be opened.
// Pass a known PipelineRun from Job, or JobURL and BuildID from My Runs.
// The destination fetches missing data after navigation.
type OpenBuildMsg struct {
	PipelineRun gojenkins.PipelineRun
	JobURL      string
	BuildID     string
}

// BuildFetchedMsg contains all data needed for the initial build render.
type BuildFetchedMsg struct {
	model            *Model
	Generation       int
	PipelineRun      gojenkins.PipelineRun
	PluginTree       *stagegraph.PluginTree
	StageGraph       *stagegraph.Graph
	FocusedStageID   string
	Node             *gojenkins.PipelineNode
	Build            gojenkins.Build
	StartedBy        string
	StartedByDetails string
	Error            error
	ErrorTitle       string
}

// GraphTreeFetchedMsg delivers an asynchronous pipeline-graph-view refresh for
// an active build during live polling.
type GraphTreeFetchedMsg struct {
	model      *Model
	Generation int
	Tree       *stagegraph.PluginTree
	Error      error
}

// LoadFailedMsg asks the shell to return to the source and show its error dialog.
// The shell checks both the page instance and generation before navigating.
type LoadFailedMsg struct {
	Model      *Model
	Generation int
	Error      error
	ErrorTitle string
}

type NodeDownloadedMsg struct {
	model        *Model
	Generation   int
	PipelineNode gojenkins.PipelineNode
	Error        error
	ErrorTitle   string
}

// ReplayMsg requests replaying (re-running) the build via the script view.
type ReplayMsg struct {
	Job         *gojenkins.Job
	PipelineRun gojenkins.PipelineRun
}

// BuildParamsFetchedMsg is sent when build parameters have been fetched.
type BuildParamsFetchedMsg struct {
	Params []gojenkins.BuildParameter
	Error  error
}

// ArtifactsDownloadedMsg is sent when the requested artifacts have been saved to disk.
type ArtifactsDownloadedMsg struct {
	Dir   string
	Count int
}

// ArtifactsDownloadFailedMsg is sent when artifact download fails.
type ArtifactsDownloadFailedMsg struct {
	Error error
}

// FetchNodeLogMsg requests the parent build model to load and focus a step's log.
type FetchNodeLogMsg struct {
	NodeID string
}

// StepLogFetchedMsg delivers the inline log file (or empty/error status) for a
// pipeline flow node without navigating away from the Build page.
type StepLogFetchedMsg struct {
	model      *Model
	Generation int
	NodeID     string
	StepName   string
	FilePath   string
	Empty      bool
	Terminal   bool
	Error      error
}

// FullLogDownloadedMsg delivers the downloaded full console log file and its
// optional stage/step/outside-stage line index for the inline log pane.
type FullLogDownloadedMsg struct {
	model      *Model
	Generation int
	FilePath   string
	Index      *LogIndex
	Terminal   bool
	Error      error
}

// ScopedLogExtractedMsg delivers a locally extracted slice of the indexed full
// console log (for a single stage, single step, or outside-of-stage lines).
type ScopedLogExtractedMsg struct {
	model      *Model
	Generation int
	Scope      logMode
	Key        string
	FilePath   string
	Empty      bool
	Error      error
}
