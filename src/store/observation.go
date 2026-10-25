package store

import (
	"cmp"
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strconv"
	"time"
)

// ApplyRunObservation updates every already-tracked run in a job and optionally
// records newly discovered trigger-correlated runs in one transaction.
func (s *Store) ApplyRunObservation(ctx context.Context, batch RunObservationBatch) (ApplyRunObservationResult, error) {
	if err := s.errIfBroken(); err != nil {
		return ApplyRunObservationResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ApplyRunObservationResult{}, err
	}
	defer func() { _ = tx.Rollback() }()

	existing, err := s.runsForJobTx(ctx, tx, batch.JobURL)
	if err != nil {
		return ApplyRunObservationResult{}, err
	}
	fallbackMatches, err := s.queueLessTriggerFallbackTx(ctx, tx, batch, existing)
	if err != nil {
		return ApplyRunObservationResult{}, err
	}

	changed := false
	for _, observed := range batch.Runs {
		if record, ok := existing[observed.BuildID]; ok {
			observedState := RunRecord{Status: observed.Status, EndTime: observed.EndTime}
			completed := !record.IsTerminal() && observedState.IsTerminal()
			if record.IsTerminal() && !observedState.IsTerminal() {
				continue
			}
			startTime := record.StartTime
			if observed.StartTime > 0 {
				startTime = observed.StartTime
			}
			if record.Status == observed.Status && record.StartTime == startTime && record.EndTime == observed.EndTime && record.Duration == observed.Duration {
				continue
			}
			args := []any{batch.JobName, observed.Status, startTime, observed.EndTime, observed.Duration, record.ID}
			args = s.appendServerArg(args)
			if _, err := tx.ExecContext(ctx,
				`UPDATE runs SET job_name = CASE WHEN job_name = '' THEN ? ELSE job_name END,
				 status = ?, start_time = ?, end_time = ?, duration = ? WHERE id = ?`+s.serverFilter(),
				args...,
			); err != nil {
				return ApplyRunObservationResult{}, err
			}
			if completed {
				if err := s.insertCompletionTx(ctx, tx, batch, observed); err != nil {
					return ApplyRunObservationResult{}, err
				}
			}
			changed = true
			continue
		}

		intentID, triggerMatched := fallbackMatches[observed.BuildID]
		if !batch.TrackNewRuns && !triggerMatched {
			continue
		}
		candidate := RunRecord{Status: observed.Status, EndTime: observed.EndTime}
		if candidate.IsTerminal() && !buildIsAboveBaseline(observed.BuildID, batch.Baseline) {
			continue
		}
		createdAt := batch.ObservedAt
		if observed.StartTime > 0 {
			createdAt = observed.StartTime
		}
		record := s.withServer(RunRecord{
			ID:        fmt.Sprintf("%s#%s", batch.JobURL, observed.BuildID),
			JobName:   batch.JobName,
			JobURL:    batch.JobURL,
			BuildID:   observed.BuildID,
			Status:    observed.Status,
			StartTime: observed.StartTime,
			EndTime:   observed.EndTime,
			Duration:  observed.Duration,
			CreatedAt: createdAt,
		})
		result, err := tx.ExecContext(ctx,
			`INSERT INTO runs (id, server_id, job_name, job_url, build_id, status, start_time, end_time, duration, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id, server_id) DO NOTHING`,
			record.ID, record.ServerID, record.JobName, record.JobURL, record.BuildID,
			record.Status, record.StartTime, record.EndTime, record.Duration, record.CreatedAt,
		)
		if err != nil {
			return ApplyRunObservationResult{}, err
		}
		inserted, err := result.RowsAffected()
		if err != nil {
			return ApplyRunObservationResult{}, err
		}
		if inserted > 0 {
			if triggerMatched {
				if err := s.resolveTriggerTx(ctx, tx, intentID, observed.BuildID, batch.ObservedAt); err != nil {
					return ApplyRunObservationResult{}, err
				}
			}
			// Inserting an already-terminal run establishes state; it is not an
			// observed active-to-terminal transition and must not create an alert.
			changed = true
			existing[observed.BuildID] = record
		}
	}

	if err := tx.Commit(); err != nil {
		return ApplyRunObservationResult{}, err
	}
	completions, err := s.PendingCompletions(ctx, 64)
	if err != nil {
		return ApplyRunObservationResult{}, err
	}
	return ApplyRunObservationResult{Changed: changed, Completions: completions}, nil
}

func (s *Store) insertCompletionTx(ctx context.Context, tx *sql.Tx, batch RunObservationBatch, observed ObservedRun) error {
	createdAt := batch.ObservedAt
	if createdAt == 0 {
		createdAt = time.Now().UnixMilli()
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO completion_outbox (
			server_id, job_url, build_id, kind, job_name, status,
			start_time, end_time, duration, created_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(server_id, job_url, build_id, kind) DO NOTHING`,
		s.serverID, batch.JobURL, observed.BuildID, completionKindFinished, batch.JobName, observed.Status,
		observed.StartTime, observed.EndTime, observed.Duration, createdAt,
	)
	return err
}

func (s *Store) runsForJobTx(ctx context.Context, tx *sql.Tx, jobURL string) (map[string]RunRecord, error) {
	query := `SELECT id, server_id, job_name, job_url, build_id, status, start_time, end_time, duration, created_at
		FROM runs WHERE job_url = ?` + s.serverFilter()
	args := s.appendServerArg([]any{jobURL})
	rows, err := tx.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	runs := make(map[string]RunRecord)
	for rows.Next() {
		var record RunRecord
		if err := rows.Scan(
			&record.ID, &record.ServerID, &record.JobName, &record.JobURL, &record.BuildID,
			&record.Status, &record.StartTime, &record.EndTime, &record.Duration, &record.CreatedAt,
		); err != nil {
			return nil, err
		}
		runs[record.BuildID] = record
	}
	return runs, rows.Err()
}

// queueLessTriggerFallbackTx pairs queue-less prepared/accepted intents with
// newly observed builds. Script replay cannot return a Jenkins queue ID, so a
// burst of replays leaves several of these intents. Requiring a single
// unambiguous intent would then hide every new build from My Runs.
//
// Pairing is oldest-intent to lowest eligible build: a candidate must be
// untracked, numerically above that intent's baseline, and not start before
// the trigger (within clock-skew allowance). Leftover intents stay unresolved.
func (s *Store) queueLessTriggerFallbackTx(
	ctx context.Context,
	tx *sql.Tx,
	batch RunObservationBatch,
	existing map[string]RunRecord,
) (map[string]string, error) {
	query := `SELECT intent_id, baseline_build, created_at, accepted_at FROM trigger_intents
		WHERE job_url = ? AND queue_id IS NULL AND state IN ('prepared', 'accepted')` + s.serverFilter() + `
		ORDER BY created_at, intent_id`
	rows, err := tx.QueryContext(ctx, query, s.appendServerArg([]any{batch.JobURL})...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	type fallbackIntent struct {
		id         string
		baseline   int64
		createdAt  int64
		acceptedAt int64
	}
	var intents []fallbackIntent
	for rows.Next() {
		var intent fallbackIntent
		if err := rows.Scan(&intent.id, &intent.baseline, &intent.createdAt, &intent.acceptedAt); err != nil {
			return nil, err
		}
		intents = append(intents, intent)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(intents) == 0 {
		return nil, nil
	}

	type candidate struct {
		buildID string
		id      int64
		start   int64
	}
	var candidates []candidate
	for _, observed := range batch.Runs {
		if _, tracked := existing[observed.BuildID]; tracked {
			continue
		}
		id, err := strconv.ParseInt(observed.BuildID, 10, 64)
		if err != nil {
			continue
		}
		candidates = append(candidates, candidate{buildID: observed.BuildID, id: id, start: observed.StartTime})
	}
	slices.SortFunc(candidates, func(a, b candidate) int {
		return cmp.Compare(a.id, b.id)
	})

	used := make([]bool, len(candidates))
	matches := make(map[string]string, len(intents))
	for _, intent := range intents {
		for i, cand := range candidates {
			if used[i] || cand.id <= intent.baseline ||
				!buildIsPlausiblyAfterTrigger(cand.start, intent.createdAt, intent.acceptedAt) {
				continue
			}
			used[i] = true
			matches[cand.buildID] = intent.id
			break
		}
	}
	return matches, nil
}

func (s *Store) resolveTriggerTx(ctx context.Context, tx *sql.Tx, intentID, buildID string, at int64) error {
	query := `UPDATE trigger_intents SET state = ?, build_id = ?, resolved_at = ?
		WHERE intent_id = ? AND state IN ('prepared', 'accepted')` + s.serverFilter()
	args := s.appendServerArg([]any{TriggerResolved, buildID, at, intentID})
	_, err := tx.ExecContext(ctx, query, args...)
	return err
}

func buildIsPlausiblyAfterTrigger(startTime, createdAt, acceptedAt int64) bool {
	if startTime == 0 {
		return true
	}
	triggerAt := createdAt
	if acceptedAt > 0 {
		triggerAt = acceptedAt
	}
	const clockSkewAllowance = int64((5 * time.Minute) / time.Millisecond)
	return triggerAt == 0 || startTime >= triggerAt-clockSkewAllowance
}

func buildIsAboveBaseline(buildID string, baseline int64) bool {
	id, err := strconv.ParseInt(buildID, 10, 64)
	return err == nil && id > baseline
}
