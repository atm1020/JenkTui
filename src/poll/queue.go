package poll

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/atm1020/gojenkins"
)

type queueState uint8

const (
	queueWaiting queueState = iota
	queueExecutable
	queueCanceled
	queueDeleted
)

type queueReceipt struct {
	state  queueState
	number BuildNumber
}
type queueResolver interface {
	resolve(context.Context, int64) (queueReceipt, error)
}
type jenkinsQueueResolver struct{ jenkins *gojenkins.Jenkins }

func (r jenkinsQueueResolver) resolve(ctx context.Context, queueID int64) (queueReceipt, error) {
	if r.jenkins == nil || r.jenkins.Requester == nil {
		return queueReceipt{}, errors.New("jenkins client is not initialized")
	}
	requester, ok := r.jenkins.Requester.(*gojenkins.Requester)
	if !ok {
		return queueReceipt{}, errors.New("jenkins requester does not expose its HTTP transport")
	}
	endpoint := strings.TrimRight(requester.Base, "/") + "/queue/item/" + strconv.FormatInt(queueID, 10) + "/api/json"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return queueReceipt{}, err
	}
	if requester.BasicAuth != nil {
		req.SetBasicAuth(requester.BasicAuth.Username, requester.BasicAuth.Password)
	}
	client := requester.Client
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return queueReceipt{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusGone {
		_, _ = io.Copy(io.Discard, resp.Body)
		return queueReceipt{state: queueDeleted}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return queueReceipt{}, fmt.Errorf("queue item %d returned HTTP %d", queueID, resp.StatusCode)
	}
	var payload struct {
		Canceled   bool `json:"cancelled"`
		Executable struct {
			Number int64 `json:"number"`
		} `json:"executable"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return queueReceipt{}, err
	}
	if payload.Executable.Number > 0 {
		return queueReceipt{state: queueExecutable, number: BuildNumber(payload.Executable.Number)}, nil
	}
	if payload.Canceled {
		return queueReceipt{state: queueCanceled}, nil
	}
	return queueReceipt{state: queueWaiting}, nil
}

func (m *Manager) reconcileTriggers(ctx context.Context, jobURL string) error {
	if m.repo == nil {
		return nil
	}
	intents, err := m.repo.unresolvedTriggers(ctx)
	if err != nil {
		return err
	}
	now := time.Now()
	for _, intent := range intents {
		if intent.JobURL != jobURL {
			continue
		}
		if !intent.ExpiresAt.IsZero() && !now.Before(intent.ExpiresAt) {
			if err := m.repo.finishTrigger(ctx, intent.IntentID, TriggerExpired, 0, now); err != nil {
				return err
			}
			continue
		}
		if intent.State != TriggerAccepted || intent.QueueID == nil {
			continue
		}
		receipt, resolveErr := m.queue.resolve(ctx, *intent.QueueID)
		if resolveErr != nil {
			return resolveErr
		}
		switch receipt.state {
		case queueWaiting:
			continue
		case queueExecutable:
			if err := m.repo.finishTrigger(ctx, intent.IntentID, TriggerResolved, receipt.number, now); err != nil {
				return err
			}
			m.ensureInternal(Target{Kind: TargetExactRun, JobURL: jobURL, BuildNumber: receipt.number})
		case queueCanceled:
			if err := m.repo.finishTrigger(ctx, intent.IntentID, TriggerCanceled, 0, now); err != nil {
				return err
			}
		case queueDeleted:
			if err := m.repo.finishTrigger(ctx, intent.IntentID, TriggerDeleted, 0, now); err != nil {
				return err
			}
		}
	}
	remaining, loadErr := m.repo.unresolvedTriggers(ctx)
	if loadErr != nil {
		return loadErr
	}
	for _, intent := range remaining {
		if intent.JobURL == jobURL {
			return nil
		}
	}
	// This releases only manager-owned trigger demand; an application watch
	// remains an independent owner.
	m.releaseInternal(Target{Kind: TargetJobWindow, JobURL: jobURL})
	return nil
}
