package job

import (
	"github.com/atm1020/jenktui/src/jobref"

	"github.com/atm1020/gojenkins"
)

type JobLoadedMsg struct {
	Generation         int
	Job                *gojenkins.Job
	PipelineRuns       []gojenkins.PipelineRun
	HasInProgressBuild bool
	IsFavorite         bool
	RedirectTarget     *jobref.Target
	Error              error
	ErrorTitle         string
}

type OpenScriptViewMsg struct {
	Job         *gojenkins.Job
	PipelineRun gojenkins.PipelineRun
}

type AttachmentDownloadCompletedMsg struct{}

// FavoriteToggledMsg is emitted after a toggle's store write completes.
type FavoriteToggledMsg struct {
	Favorited bool
}
