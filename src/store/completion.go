package store

import (
	"context"
	"time"
)

const completionKindFinished = "finished"

// PendingCompletions returns an ordered, bounded page of unacknowledged facts.
func (s *Store) PendingCompletions(ctx context.Context, limit int) ([]CompletionRecord, error) {
	if err := s.errIfBroken(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		limit = 64
	}
	query := `SELECT server_id, job_url, build_id, kind, job_name, status, start_time, end_time, duration, created_at
		FROM completion_outbox WHERE delivered_at IS NULL`
	var args []any
	if s.serverID != "" {
		query += ` AND server_id = ?`
		args = append(args, s.serverID)
	}
	query += ` ORDER BY created_at, job_url, build_id LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	var completions []CompletionRecord
	for rows.Next() {
		var completion CompletionRecord
		if err := rows.Scan(
			&completion.Key.ServerID, &completion.Key.JobURL, &completion.Key.BuildID, &completion.Key.Kind,
			&completion.JobName, &completion.Status, &completion.StartTime, &completion.EndTime,
			&completion.Duration, &completion.CreatedAt,
		); err != nil {
			return nil, err
		}
		completions = append(completions, completion)
	}
	return completions, rows.Err()
}

func (s *Store) PendingCompletionCount(ctx context.Context) (int, error) {
	if err := s.errIfBroken(); err != nil {
		return 0, err
	}
	query := `SELECT COUNT(*) FROM completion_outbox WHERE delivered_at IS NULL`
	var args []any
	if s.serverID != "" {
		query += ` AND server_id = ?`
		args = append(args, s.serverID)
	}
	var count int
	err := s.db.QueryRowContext(ctx, query, args...).Scan(&count)
	return count, err
}

// AckCompletions marks successfully handled facts delivered.
func (s *Store) AckCompletions(ctx context.Context, keys []CompletionKey) error {
	if len(keys) == 0 {
		return nil
	}
	if err := s.errIfBroken(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	deliveredAt := time.Now().UnixMilli()
	for _, key := range keys {
		serverID := key.ServerID
		if serverID == "" {
			serverID = s.serverID
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE completion_outbox SET delivered_at = ?
			 WHERE server_id = ? AND job_url = ? AND build_id = ? AND kind = ? AND delivered_at IS NULL`,
			deliveredAt, serverID, key.JobURL, key.BuildID, key.Kind,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}
