package poll

import (
	"context"
	"sort"
	"time"

	"charm.land/log/v2"
)

func (m *Manager) publishSnapshot(snapshot Snapshot) {
	if !m.watches.recordSnapshot(snapshot) {
		return
	}
	m.aggregateMu.Lock()
	m.pendingSnapshots[snapshot.Target] = cloneSnapshot(snapshot)
	delete(m.pendingFailures, snapshot.Target)
	m.aggregateMu.Unlock()
	m.signalDelivery()
}

func (m *Manager) recordFailure(target Target, err error) {
	m.aggregateMu.Lock()
	failure := m.pendingFailures[target]
	failure.Target, failure.At, failure.Err = target, time.Now(), err
	failure.Occurrences++
	m.pendingFailures[target] = failure
	m.aggregateMu.Unlock()
	m.signalDelivery()
}

func (m *Manager) markRunsChanged() {
	m.aggregateMu.Lock()
	m.pendingRunsChanged = true
	m.aggregateMu.Unlock()
	m.signalDelivery()
}

func (m *Manager) signalDelivery() {
	select {
	case m.deliveryReady <- struct{}{}:
	default:
	}
}

func (m *Manager) takeAggregate() Batch {
	m.aggregateMu.Lock()
	batch := Batch{RunsChanged: m.pendingRunsChanged}
	for _, snapshot := range m.pendingSnapshots {
		batch.Snapshots = append(batch.Snapshots, cloneSnapshot(snapshot))
	}
	for _, failure := range m.pendingFailures {
		batch.Failures = append(batch.Failures, failure)
	}
	clear(m.pendingSnapshots)
	clear(m.pendingFailures)
	m.pendingRunsChanged = false
	m.aggregateMu.Unlock()
	sort.Slice(batch.Snapshots, func(i, j int) bool { return targetLess(batch.Snapshots[i].Target, batch.Snapshots[j].Target) })
	sort.Slice(batch.Failures, func(i, j int) bool { return targetLess(batch.Failures[i].Target, batch.Failures[j].Target) })
	return batch
}

func targetLess(a, b Target) bool {
	if a.JobURL != b.JobURL {
		return a.JobURL < b.JobURL
	}
	if a.Kind != b.Kind {
		return a.Kind < b.Kind
	}
	return a.BuildNumber < b.BuildNumber
}

func (m *Manager) pendingCompletionPage(ctx context.Context) []Completion {
	if m.repo == nil {
		return nil
	}
	records, err := m.repo.pendingCompletions(ctx, 64)
	if err != nil {
		if ctx.Err() == nil {
			log.Error("poll: load completion outbox", "error", err)
		}
		return nil
	}
	m.completionMu.Lock()
	defer m.completionMu.Unlock()
	result := make([]Completion, 0, len(records))
	for _, completion := range records {
		if _, busy := m.inFlightCompletions[completion.Key]; busy {
			continue
		}
		m.inFlightCompletions[completion.Key] = struct{}{}
		result = append(result, completion)
	}
	return result
}

func (m *Manager) deliveryLoop() {
	defer m.deliveryWG.Done()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-m.deliveryReady:
		}
		batch := m.takeAggregate()
		batch.Completions = m.pendingCompletionPage(m.ctx)
		if len(batch.Snapshots) == 0 && len(batch.Failures) == 0 && len(batch.Completions) == 0 && !batch.RunsChanged {
			continue
		}
		delivery := Delivery{Batch: batch, Ack: m.ackCallback(batch.Completions)}
		select {
		case m.deliveries <- delivery:
		case <-m.ctx.Done():
			m.releaseCompletionPage(batch.Completions)
			return
		}
	}
}

func (m *Manager) ackCallback(page []Completion) func(context.Context, []CompletionKey) error {
	return func(ctx context.Context, successful []CompletionKey) error {
		if m.repo != nil && len(successful) > 0 {
			if err := m.repo.ackCompletions(ctx, successful); err != nil {
				m.releaseCompletionPage(page)
				m.signalDelivery()
				return err
			}
		}
		m.releaseCompletionPage(page)
		m.signalDelivery()
		return nil
	}
}

func (m *Manager) releaseCompletionPage(page []Completion) {
	m.completionMu.Lock()
	for _, completion := range page {
		delete(m.inFlightCompletions, completion.Key)
	}
	m.completionMu.Unlock()
}

// Results returns the aggregate stream. The app shell is its sole consumer.
func (m *Manager) Results() <-chan Delivery { return m.deliveries }
