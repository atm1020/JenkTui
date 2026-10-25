package poll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/atm1020/gojenkins"
	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/jobref"
)

// SourceErrorKind classifies failures at the Jenkins boundary.
type SourceErrorKind uint8

const (
	SourceErrorUnknown SourceErrorKind = iota
	SourceErrorAuthentication
	SourceErrorTimeout
	SourceErrorCanceled
	SourceErrorNotFound
	SourceErrorMalformed
	SourceErrorUnavailable
)

// SourceError is a typed Jenkins adapter failure.
type SourceError struct {
	Kind      SourceErrorKind
	Operation string
	Target    Target
	Err       error
}

func (e *SourceError) Error() string {
	return fmt.Sprintf("%s %s: %v", e.Operation, e.Target.JobURL, e.Err)
}
func (e *SourceError) Unwrap() error { return e.Err }

type statusError struct{ code int }

func (e statusError) Error() string { return fmt.Sprintf("Jenkins returned HTTP %d", e.code) }

type observationSource interface {
	fetchWindow(context.Context, string, BuildNumber) (Observation, error)
	fetchExact(context.Context, Target) (Observation, error)
}

type jenkinsSource struct{ jenkins *gojenkins.Jenkins }

func (s jenkinsSource) fetchWindow(ctx context.Context, jobURL string, cursor BuildNumber) (Observation, error) {
	job, err := s.resolveJob(ctx, jobURL)
	if err != nil {
		return Observation{}, classifySource("resolve job", Target{Kind: TargetJobWindow, JobURL: jobURL}, false, err)
	}
	query := url.Values{"fullStages": {"true"}}
	if cursor > 0 {
		query.Set("since", "#"+strconv.FormatInt(int64(cursor), 10))
	}
	var runs []gojenkins.PipelineRun
	if err := fetchJSON(ctx, job, job.Base+"/wfapi/runs", query, &runs); err != nil {
		return Observation{}, classifySource("fetch job window", Target{Kind: TargetJobWindow, JobURL: jobURL}, false, err)
	}
	snapshots, err := snapshotsFromRuns(ctx, job, jobURL, runs)
	if err != nil {
		return Observation{}, err
	}
	return Observation{Target: Target{Kind: TargetJobWindow, JobURL: jobURL}, ObservedAt: time.Now(), Runs: snapshots}, nil
}

func (s jenkinsSource) fetchExact(ctx context.Context, target Target) (Observation, error) {
	job, err := s.resolveJob(ctx, target.JobURL)
	if err != nil {
		return Observation{}, classifySource("resolve job", target, true, err)
	}
	var run gojenkins.PipelineRun
	endpoint := job.Base + "/" + strconv.FormatInt(int64(target.BuildNumber), 10) + "/wfapi/describe"
	if err := fetchJSON(ctx, job, endpoint, nil, &run); err != nil {
		return Observation{}, classifySource("fetch exact run", target, true, err)
	}
	runs, err := snapshotsFromRuns(ctx, job, target.JobURL, []gojenkins.PipelineRun{run})
	if err != nil {
		return Observation{}, err
	}
	return Observation{Target: target, ObservedAt: time.Now(), Runs: runs}, nil
}

func (s jenkinsSource) resolveJob(ctx context.Context, jobURL string) (*gojenkins.Job, error) {
	if s.jenkins == nil {
		return nil, errors.New("jenkins client is not initialized")
	}
	id, parents := jobref.ToIDAndParents(jobURL)
	return s.jenkins.GetJob(ctx, id, parents...)
}

func fetchJSON(ctx context.Context, job *gojenkins.Job, endpoint string, query url.Values, dst any) error {
	if job == nil || job.Jenkins == nil || job.Jenkins.Requester == nil {
		return errors.New("jenkins job is not initialized")
	}
	requester, ok := job.Jenkins.Requester.(*gojenkins.Requester)
	if !ok {
		return errors.New("jenkins requester does not expose its HTTP transport")
	}
	requestURL := strings.TrimRight(requester.Base, "/") + "/" + strings.Trim(endpoint, "/") + "/api/json"
	parsed, err := url.Parse(requestURL)
	if err != nil {
		return err
	}
	if query != nil {
		parsed.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if requester.BasicAuth != nil {
		req.SetBasicAuth(requester.BasicAuth.Username, requester.BasicAuth.Password)
	}
	client := requester.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return statusError{resp.StatusCode}
	}
	if remoteErr := resp.Header.Get("X-Error"); remoteErr != "" {
		return errors.New(remoteErr)
	}
	return json.NewDecoder(resp.Body).Decode(dst)
}

func snapshotsFromRuns(ctx context.Context, job *gojenkins.Job, jobURL string, runs []gojenkins.PipelineRun) ([]RunSnapshot, error) {
	result := make([]RunSnapshot, len(runs))
	for i := range runs {
		number, err := parseBuildNumber(runs[i].ID)
		if err != nil {
			return nil, classifySource("parse build ID", Target{Kind: TargetJobWindow, JobURL: jobURL}, false, err)
		}
		wire := buildstatus.PipelineStatus(strings.ToUpper(strings.TrimSpace(runs[i].Status)))
		status := wire.NormalizeQueueState(len(runs[i].Stages), runs[i].EndTime)
		activity := status.Activity(runs[i].EndTime)
		if strings.EqualFold(strings.TrimSpace(runs[i].Status), string(buildstatus.StatusNotExecuted)) {
			completed, confirmErr := confirmCompletion(ctx, job, number)
			if confirmErr != nil {
				return nil, classifySource("confirm completion", Target{Kind: TargetExactRun, JobURL: jobURL, BuildNumber: number}, true, confirmErr)
			}
			if completed {
				activity = buildstatus.ActivityFinished
			} else if len(runs[i].Stages) == 0 {
				status, activity = buildstatus.StatusQueued, buildstatus.ActivityQueued
			} else {
				status, activity = buildstatus.StatusInProgress, buildstatus.ActivityRunning
			}
		}
		stages := make([]StageSnapshot, len(runs[i].Stages))
		for j := range runs[i].Stages {
			stage := runs[i].Stages[j]
			stages[j] = StageSnapshot{ID: stage.ID, Name: stage.Name, Status: buildstatus.PipelineStatus(strings.ToUpper(strings.TrimSpace(stage.Status))), ExecNode: stage.ExecNode, StartTimeMillis: stage.StartTime, DurationMillis: stage.Duration}
		}
		result[i] = RunSnapshot{BuildNumber: number, Name: runs[i].Name, Status: status, Activity: activity, StartTimeMillis: runs[i].StartTime, EndTimeMillis: runs[i].EndTime, DurationMillis: runs[i].Duration, Stages: stages}
	}
	return result, nil
}

func parseBuildNumber(raw string) (BuildNumber, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
	if err != nil || n <= 0 {
		if err == nil {
			err = errors.New("build ID must be positive")
		}
		return 0, fmt.Errorf("unsupported Jenkins build ID %q: %w", raw, err)
	}
	return BuildNumber(n), nil
}

func confirmCompletion(ctx context.Context, job *gojenkins.Job, number BuildNumber) (bool, error) {
	var state struct {
		Building bool `json:"building"`
	}
	if err := fetchJSON(ctx, job, job.Base+"/"+strconv.FormatInt(int64(number), 10), nil, &state); err != nil {
		return false, err
	}
	return !state.Building, nil
}

func classifySource(operation string, target Target, exact bool, err error) error {
	kind := SourceErrorUnknown
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		kind = SourceErrorTimeout
	case errors.Is(err, context.Canceled):
		kind = SourceErrorCanceled
	default:
		var status statusError
		var syntax *json.SyntaxError
		var typeErr *json.UnmarshalTypeError
		switch {
		case errors.As(err, &status) && (status.code == http.StatusUnauthorized || status.code == http.StatusForbidden):
			kind = SourceErrorAuthentication
		case errors.As(err, &status) && status.code == http.StatusNotFound && exact:
			kind = SourceErrorNotFound
		case errors.As(err, &status) && status.code == http.StatusNotFound:
			kind = SourceErrorUnavailable
		case errors.As(err, &syntax), errors.As(err, &typeErr), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF), strings.Contains(err.Error(), "unsupported Jenkins build ID"):
			kind = SourceErrorMalformed
		}
	}
	return &SourceError{Kind: kind, Operation: operation, Target: target, Err: err}
}
