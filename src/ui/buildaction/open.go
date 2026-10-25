package buildaction

import (
	"errors"
	"strings"

	"github.com/atm1020/jenktui/src/jobref"
	"github.com/atm1020/jenktui/src/ui/util"

	tea "charm.land/bubbletea/v2"
	"github.com/atm1020/gojenkins"
)

// OpenLogMsg asks the app shell to open a run's full log. Callers with a live
// run pass PipelineRun; identifier-only callers pass JobURL and BuildID.
type OpenLogMsg struct {
	PipelineRun *gojenkins.PipelineRun
	JobURL      string
	BuildID     string
}

// OpenJob navigates to an already-resolved pipeline job without fetching it
// again.
func OpenJob(job *gojenkins.Job) tea.Cmd {
	if job == nil {
		return openError("job is not available", "Could not open job")
	}
	return util.CmdHandler(util.OpenJobComponentMsg{Job: job})
}

// OpenJobByURL navigates to a job known only by its stored identity. The app's
// job router resolves its kind before choosing the destination page.
func OpenJobByURL(jobURL, jobName string) tea.Cmd {
	if strings.TrimSpace(jobURL) == "" {
		return openError("job URL is not available", "Could not open job")
	}
	return util.CmdHandler(util.OpenJobTargetMsg{Target: jobref.Target{
		URL:  jobURL,
		Name: jobName,
	}})
}

// OpenLog navigates to the full log for an already-resolved run.
func OpenLog(run gojenkins.PipelineRun) tea.Cmd {
	if run.Job == nil || strings.TrimSpace(run.ID) == "" {
		return openError("build is not available", "Could not open log")
	}
	return util.CmdHandler(OpenLogMsg{PipelineRun: &run})
}

// OpenLogByURL navigates to the full log for a stored run identity.
func OpenLogByURL(jobURL, buildID string) tea.Cmd {
	if strings.TrimSpace(jobURL) == "" || strings.TrimSpace(buildID) == "" {
		return openError("build identity is not available", "Could not open log")
	}
	return util.CmdHandler(OpenLogMsg{JobURL: jobURL, BuildID: buildID})
}

// OpenJobInBrowser asks the app shell to open a Jenkins job URL.
func OpenJobInBrowser(jobURL string) tea.Cmd {
	if strings.TrimSpace(jobURL) == "" {
		return openError("job URL is not available", "Open Browser")
	}
	return util.CmdHandler(util.BrowserOpenMsg{Url: jobURL})
}

// OpenBuildInBrowser asks the app shell to open a Jenkins build URL.
func OpenBuildInBrowser(jobURL, buildID string) tea.Cmd {
	if strings.TrimSpace(jobURL) == "" || strings.TrimSpace(buildID) == "" {
		return openError("build identity is not available", "Open Browser")
	}
	return util.CmdHandler(util.BrowserOpenMsg{Url: jobref.BuildURL(jobURL, buildID)})
}

func openError(message, title string) tea.Cmd {
	return util.CmdHandler(util.ErrorMsg{Error: errors.New(message), Title: title})
}
