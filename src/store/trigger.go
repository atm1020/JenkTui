package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

func (s *Store) TrackTriggerIntent(ctx context.Context, intent TriggerIntentRecord) error {
	if err := s.errIfBroken(); err != nil {
		return err
	}
	intent.IntentID = strings.TrimSpace(intent.IntentID)
	intent.JobURL = strings.TrimSpace(intent.JobURL)
	if intent.IntentID == "" || intent.JobURL == "" {
		return errors.New("trigger intent requires intent ID and job URL")
	}
	if intent.ServerID == "" {
		intent.ServerID = s.serverID
	}
	if intent.State == "" {
		intent.State = TriggerPrepared
	}
	switch intent.State {
	case TriggerPrepared, TriggerAccepted, TriggerFailed:
	default:
		return fmt.Errorf("invalid tracked trigger state %q", intent.State)
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO trigger_intents (
			intent_id, server_id, job_url, baseline_build, queue_id, build_id,
			state, created_at, accepted_at, expires_at, resolved_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(intent_id, server_id) DO UPDATE SET
			queue_id = excluded.queue_id,
			state = excluded.state,
			accepted_at = excluded.accepted_at,
			expires_at = excluded.expires_at,
			resolved_at = excluded.resolved_at
		WHERE trigger_intents.state = 'prepared'
		   OR (trigger_intents.state = 'accepted' AND excluded.state = 'accepted')`,
		intent.IntentID, intent.ServerID, intent.JobURL, intent.BaselineBuild,
		intent.QueueID, intent.BuildID, intent.State, intent.CreatedAt,
		intent.AcceptedAt, intent.ExpiresAt, intent.ResolvedAt,
	)
	return err
}

func (s *Store) ActiveTriggeredRuns(ctx context.Context) ([]RunRecord, error) {
	if err := s.errIfBroken(); err != nil {
		return nil, err
	}
	query := `SELECT r.id, r.server_id, r.job_name, r.job_url, r.build_id,
		r.status, r.start_time, r.end_time, r.duration, r.created_at
		FROM runs r
		JOIN trigger_intents t
		  ON t.server_id = r.server_id AND t.job_url = r.job_url AND t.build_id = r.build_id
		WHERE t.state = 'resolved' AND NOT (
			UPPER(r.status) IN ('SUCCESS', 'FAILED', 'FAILURE', 'UNSTABLE', 'ABORTED')
			OR (UPPER(r.status) = 'NOT_EXECUTED' AND r.end_time > 0)
		)`
	var args []any
	if s.serverID != "" {
		query += ` AND r.server_id = ?`
		args = append(args, s.serverID)
	}
	query += ` ORDER BY r.created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return scanRuns(rows)
}

func (s *Store) UnresolvedTriggerIntents(ctx context.Context) ([]TriggerIntentRecord, error) {
	if err := s.errIfBroken(); err != nil {
		return nil, err
	}
	query := `SELECT intent_id, server_id, job_url, baseline_build, queue_id, build_id,
		state, created_at, accepted_at, expires_at, resolved_at
		FROM trigger_intents WHERE state IN ('prepared', 'accepted')`
	var args []any
	if s.serverID != "" {
		query += ` AND server_id = ?`
		args = append(args, s.serverID)
	}
	query += ` ORDER BY created_at, intent_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var intents []TriggerIntentRecord
	for rows.Next() {
		var intent TriggerIntentRecord
		if err := rows.Scan(
			&intent.IntentID, &intent.ServerID, &intent.JobURL, &intent.BaselineBuild,
			&intent.QueueID, &intent.BuildID, &intent.State, &intent.CreatedAt,
			&intent.AcceptedAt, &intent.ExpiresAt, &intent.ResolvedAt,
		); err != nil {
			return nil, err
		}
		intents = append(intents, intent)
	}
	return intents, rows.Err()
}

// FinishTriggerIntent makes an unresolved intent terminal. Resolving to a
// build and creating its durable run target happen in one transaction.
func (s *Store) FinishTriggerIntent(ctx context.Context, intentID string, state TriggerState, buildID string, at int64) error {
	if err := s.errIfBroken(); err != nil {
		return err
	}
	switch state {
	case TriggerResolved, TriggerFailed, TriggerCanceled, TriggerExpired, TriggerDeleted:
	default:
		return fmt.Errorf("invalid terminal trigger state %q", state)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	query := `SELECT server_id, job_url FROM trigger_intents
		WHERE intent_id = ? AND state IN ('prepared', 'accepted')`
	args := []any{intentID}
	if s.serverID != "" {
		query += ` AND server_id = ?`
		args = append(args, s.serverID)
	}
	var serverID, jobURL string
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&serverID, &jobURL); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return tx.Commit()
		}
		return err
	}
	if state == TriggerResolved {
		if buildID == "" {
			return errors.New("resolved trigger requires build ID")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO runs (
			id, server_id, job_name, job_url, build_id, status,
			start_time, end_time, duration, created_at
		) VALUES (?, ?, ?, ?, ?, 'QUEUED', 0, 0, 0, ?)
		ON CONFLICT(id, server_id) DO NOTHING`,
			jobURL+"#"+buildID, serverID, "", jobURL, buildID, at)
		if err != nil {
			return err
		}
	}
	update := `UPDATE trigger_intents SET state = ?, build_id = ?, resolved_at = ?
		WHERE intent_id = ? AND server_id = ? AND state IN ('prepared', 'accepted')`
	if _, err := tx.ExecContext(ctx, update, state, buildID, at, intentID, serverID); err != nil {
		return err
	}
	return tx.Commit()
}

// QueueID returns a pointer to id for TriggerIntentRecord.QueueID literals.
func QueueID(id int64) *int64 {
	return &id
}
