package build

import (
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
	Node             *gojenkins.PipelineNode
	Build            gojenkins.Build
	StartedBy        string
	StartedByDetails string
	Error            error
	ErrorTitle       string
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

type NodeLogDownloaded struct {
	PiplineNode gojenkins.PipelineNode
}

type FetchFlowNodeLogMsg struct {
	FlowNode gojenkins.PipelineNode
}

// FetchNodeLogMsg requests the parent component to fetch a node's log file.
// Direction: Child → Parent
type FetchNodeLogMsg struct {
	NodeID string
}

type FlowNodeLogFetched struct {
	NodeID   string
	FileName string
}

// OpenLogViewerMsg requests opening a log file in the log viewer
type OpenLogViewerMsg struct {
	FilePath string
}
