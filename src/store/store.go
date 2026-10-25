package store

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/atm1020/jenktui/src/buildstatus"
	"github.com/atm1020/jenktui/src/jobref"

	_ "modernc.org/sqlite"
)

const (
	runsFileName         = "runs.db"
	currentSchemaVersion = 1
)

// Store manages run records persisted in a SQLite database at
// {dataDir}/runs.db. Methods with a query scope filter by the store's
// serverID when it was created with NewForServer.
type Store struct {
	db       *sql.DB
	serverID string
	openErr  error // deferred open/migrate failure, surfaced on first use
}

// New creates a new Store that persists to {dataDir}/runs.db.
func New(dataDir string) *Store { return NewForServer(dataDir, "") }

// NewForServer scopes reads and newly-added records to one Jenkins server.
func NewForServer(dataDir, serverID string) *Store {
	path := filepath.Join(dataDir, runsFileName)
	// _pragma applies to every connection the pool opens; a one-shot
	// db.Exec("PRAGMA ...") would only affect the connection that ran it.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_txlock=immediate", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		// sql.Open only validates the DSN; surface open errors lazily via
		// the first failing query, same as the previous JSON store surfaced
		// filesystem errors on first use.
		return &Store{db: nil, serverID: serverID, openErr: err}
	}
	// SQLite serializes writes at the file level. Keeping a single connection
	// also prevents concurrent pool connections from racing WAL checkpoints.
	db.SetMaxOpenConns(1)

	s := &Store{db: db, serverID: serverID}
	if err := s.migrate(); err != nil {
		s.openErr = err
	}
	return s
}

// Close releases the underlying database pool. Called on server switch so
// each switch doesn't leak connections and management goroutines.
func (s *Store) Close() error {
	if s.db == nil {
		return nil
	}
	return s.db.Close()
}

// migrate installs the current schema atomically. An empty schema_version table
// is version 0; this unreleased schema has no historical migrations to retain.
func (s *Store) migrate() error {
	ctx := context.Background()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_version (version INTEGER NOT NULL)`); err != nil {
		return err
	}

	var version int
	err = tx.QueryRowContext(ctx, `SELECT version FROM schema_version LIMIT 1`).Scan(&version)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if version > currentSchemaVersion {
		return fmt.Errorf("database schema version %d is newer than supported version %d", version, currentSchemaVersion)
	}
	if version == currentSchemaVersion {
		return tx.Commit()
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS runs (
			id          TEXT NOT NULL,
			server_id   TEXT NOT NULL DEFAULT '',
			job_name    TEXT NOT NULL,
			job_url     TEXT NOT NULL,
			build_id    TEXT NOT NULL,
			status      TEXT NOT NULL,
			start_time  INTEGER NOT NULL DEFAULT 0,
			end_time    INTEGER NOT NULL DEFAULT 0,
			duration    INTEGER NOT NULL DEFAULT 0,
			created_at  INTEGER NOT NULL,
			PRIMARY KEY (id, server_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_server_created ON runs(server_id, created_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_job_url ON runs(server_id, job_url)`,
		`CREATE INDEX IF NOT EXISTS idx_runs_status ON runs(server_id, status)`,
		`CREATE TABLE IF NOT EXISTS favorites (
			url         TEXT NOT NULL,
			server_id   TEXT NOT NULL DEFAULT '',
			name        TEXT NOT NULL,
			kind        TEXT NOT NULL,
			created_at  INTEGER NOT NULL,
			PRIMARY KEY (url, server_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_favorites_server_created ON favorites(server_id, created_at DESC)`,
		`CREATE TABLE IF NOT EXISTS completion_outbox (
			server_id    TEXT NOT NULL DEFAULT '',
			job_url      TEXT NOT NULL,
			build_id     TEXT NOT NULL,
			kind         TEXT NOT NULL,
			job_name     TEXT NOT NULL,
			status       TEXT NOT NULL,
			start_time   INTEGER NOT NULL DEFAULT 0,
			end_time     INTEGER NOT NULL DEFAULT 0,
			duration     INTEGER NOT NULL DEFAULT 0,
			created_at   INTEGER NOT NULL,
			delivered_at INTEGER,
			PRIMARY KEY (server_id, job_url, build_id, kind)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_completion_pending ON completion_outbox(server_id, delivered_at, created_at)`,
		`CREATE TABLE IF NOT EXISTS trigger_intents (
			intent_id       TEXT NOT NULL,
			server_id       TEXT NOT NULL DEFAULT '',
			job_url         TEXT NOT NULL,
			baseline_build  INTEGER NOT NULL DEFAULT 0,
			queue_id        INTEGER,
			build_id        TEXT NOT NULL DEFAULT '',
			state           TEXT NOT NULL,
			created_at      INTEGER NOT NULL,
			accepted_at     INTEGER NOT NULL DEFAULT 0,
			expires_at      INTEGER NOT NULL,
			resolved_at     INTEGER NOT NULL DEFAULT 0,
			PRIMARY KEY (intent_id, server_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_trigger_unresolved ON trigger_intents(server_id, state, expires_at)`,
		`CREATE INDEX IF NOT EXISTS idx_trigger_job ON trigger_intents(server_id, job_url, state)`,
		`DELETE FROM schema_version`,
		`INSERT INTO schema_version (version) VALUES (1)`,
	}
	for _, q := range stmts {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// errIfBroken surfaces a deferred open/migrate error on first use.
func (s *Store) errIfBroken() error {
	if s.openErr != nil {
		return s.openErr
	}
	if s.db == nil {
		return fmt.Errorf("store: database not open")
	}
	return nil
}

// withServer fills an empty record ServerID from the store's scope.
func (s *Store) withServer(rec RunRecord) RunRecord {
	if rec.ServerID == "" {
		rec.ServerID = s.serverID
	}
	return rec
}

// AddRun adds a new run record to the store. Returns an error if a record
// with the same (id, server_id) already exists.
func (s *Store) AddRun(record RunRecord) error {
	if err := s.errIfBroken(); err != nil {
		return err
	}
	record = s.withServer(record)
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO runs (id, server_id, job_name, job_url, build_id, status, start_time, end_time, duration, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		record.ID, record.ServerID, record.JobName, record.JobURL, record.BuildID,
		record.Status, record.StartTime, record.EndTime, record.Duration, record.CreatedAt,
	)
	return err
}

// AddRunIfNotExists adds a run record only if no record with the same ID exists.
// Returns true if the record was added, false if it already existed.
func (s *Store) AddRunIfNotExists(record RunRecord) (bool, error) {
	if err := s.errIfBroken(); err != nil {
		return false, err
	}
	record = s.withServer(record)
	res, err := s.db.ExecContext(context.Background(),
		`INSERT INTO runs (id, server_id, job_name, job_url, build_id, status, start_time, end_time, duration, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id, server_id) DO NOTHING`,
		record.ID, record.ServerID, record.JobName, record.JobURL, record.BuildID,
		record.Status, record.StartTime, record.EndTime, record.Duration, record.CreatedAt,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// UpdateRun updates an existing run record by ID. A zero startTime leaves the
// stored start time untouched (poll snapshots report 0 for queued builds).
// A missing ID is a no-op.
func (s *Store) UpdateRun(id string, status buildstatus.PipelineStatus, endTime int64, duration int64, startTime int64) error {
	if err := s.errIfBroken(); err != nil {
		return err
	}
	q := `UPDATE runs SET status = ?, end_time = ?, duration = ?`
	args := []any{status, endTime, duration}
	if startTime > 0 {
		q += `, start_time = ?`
		args = append(args, startTime)
	}
	q += ` WHERE id = ?` + s.serverFilter()
	args = append(args, id)
	args = s.appendServerArg(args)
	_, err := s.db.ExecContext(context.Background(), q, args...)
	return err
}

// UpdateRunFull updates a pending run with its real build ID and all fields.
// If a row with newID already exists (poll persisted the real build first),
// the stale pending row is deleted instead.
func (s *Store) UpdateRunFull(oldID string, newID string, buildID string, status buildstatus.PipelineStatus, startTime int64, endTime int64, duration int64) error {
	if err := s.errIfBroken(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	filter := s.serverFilter()
	updateArgs := append([]any{newID, buildID, status, startTime, endTime, duration}, s.appendServerArg([]any{oldID})...)
	_, err = tx.ExecContext(context.Background(),
		`UPDATE runs SET id = ?, build_id = ?, status = ?, start_time = ?, end_time = ?, duration = ?
		 WHERE id = ?`+filter,
		updateArgs...,
	)
	if err != nil {
		if isUniqueViolation(err) {
			// Real row already exists: the pending row is stale, drop it.
			if _, derr := tx.ExecContext(context.Background(), `DELETE FROM runs WHERE id = ?`+filter, s.appendServerArg([]any{oldID})...); derr != nil {
				return derr
			}
			return tx.Commit()
		}
		return err
	}
	return tx.Commit()
}

// DeleteRun removes a run record by ID. A missing ID is a no-op.
func (s *Store) DeleteRun(id string) error {
	if err := s.errIfBroken(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(context.Background(), `DELETE FROM runs WHERE id = ?`+s.serverFilter(), s.appendServerArg([]any{id})...)
	return err
}

// GetAll returns all run records, sorted newest first.
func (s *Store) GetAll() ([]RunRecord, error) {
	return s.QueryRuns(RunQuery{SortBy: RunSortDate, Direction: SortDescending})
}

// QueryRuns returns run records matching query, scoped to the store's
// server and ordered from an allow-listed sort field and direction. JobName
// and BuildID are always-present case-insensitive substring conditions (an
// empty value becomes the match-all pattern "%%"); Status is an exact,
// case-insensitive match appended only when non-empty, since "All" must mean
// "no condition" rather than "status equals empty string".
func (s *Store) QueryRuns(q RunQuery) ([]RunRecord, error) {
	if err := s.errIfBroken(); err != nil {
		return nil, err
	}

	query := `SELECT id, server_id, job_name, job_url, build_id, status, start_time, end_time, duration, created_at
		FROM runs WHERE 1=1`
	var args []any

	if s.serverID != "" {
		query += ` AND server_id = ?`
		args = append(args, s.serverID)
	}

	if q.JobName != "" {
		query += ` AND job_name LIKE ? ESCAPE '\'`
		args = append(args, likePattern(q.JobName))
	}

	if q.BuildID != "" {
		query += ` AND build_id LIKE ? ESCAPE '\'`
		args = append(args, likePattern(normalizeBuildIDFilter(q.BuildID)))
	}

	if status := strings.ToUpper(strings.TrimSpace(string(q.Status))); status != "" {
		query += ` AND status = ?`
		args = append(args, status)
	}

	query += ` ORDER BY ` + orderByClause(q.SortBy, q.Direction)

	rows, err := s.db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	return scanRuns(rows)
}

// GetInProgressRuns returns the server-scoped active tracking targets.
func (s *Store) GetInProgressRuns() ([]RunRecord, error) {
	return s.GetInProgressRunsContext(context.Background())
}

// GetInProgressRunsContext performs the active-run filter in SQLite so startup
// target collection does not scan all run history in application code.
func (s *Store) GetInProgressRunsContext(ctx context.Context) ([]RunRecord, error) {
	if err := s.errIfBroken(); err != nil {
		return nil, err
	}
	query := `SELECT id, server_id, job_name, job_url, build_id, status, start_time, end_time, duration, created_at
		FROM runs WHERE NOT (
			UPPER(status) IN ('SUCCESS', 'FAILED', 'FAILURE', 'UNSTABLE', 'ABORTED')
			OR (UPPER(status) = 'NOT_EXECUTED' AND end_time > 0)
		)`
	var args []any
	if s.serverID != "" {
		query += ` AND server_id = ?`
		args = append(args, s.serverID)
	}
	query += ` ORDER BY created_at DESC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	return scanRuns(rows)
}

// GetRunsByJobURL returns non-terminal runs for a job URL as a map keyed by BuildID.
func (s *Store) GetRunsByJobURL(jobURL string) (map[string]*RunRecord, error) {
	if err := s.errIfBroken(); err != nil {
		return nil, err
	}
	q := `SELECT id, server_id, job_name, job_url, build_id, status, start_time, end_time, duration, created_at
		 FROM runs WHERE job_url = ?`
	args := []any{jobURL}
	if w := s.serverWhere(); w != "" {
		q += ` AND ` + w[len(" WHERE "):]
		args = append(args, s.serverArgs()...)
	}
	rows, err := s.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		return nil, err
	}
	runs, err := scanRuns(rows)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*RunRecord)
	for i, r := range runs {
		if !r.IsTerminal() {
			result[r.BuildID] = &runs[i]
		}
	}
	return result, nil
}

// withServerFavorite fills an empty record ServerID from the store's scope.
func (s *Store) withServerFavorite(rec FavoriteRecord) FavoriteRecord {
	if rec.ServerID == "" {
		rec.ServerID = s.serverID
	}
	return rec
}

// AddFavorite adds a job/folder to the favorites list. Returns an error if a
// record with the same (url, server_id) already exists.
func (s *Store) AddFavorite(rec FavoriteRecord) error {
	if err := s.errIfBroken(); err != nil {
		return err
	}
	rec = s.withServerFavorite(rec)
	_, err := s.db.ExecContext(context.Background(),
		`INSERT INTO favorites (url, server_id, name, kind, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		rec.URL, rec.ServerID, rec.Name, rec.Kind, rec.CreatedAt,
	)
	return err
}

// RemoveFavorite removes a favorite by URL. A missing URL is a no-op.
func (s *Store) RemoveFavorite(url string) error {
	if err := s.errIfBroken(); err != nil {
		return err
	}
	_, err := s.db.ExecContext(context.Background(),
		`DELETE FROM favorites WHERE url = ?`+s.serverFilter(), s.appendServerArg([]any{url})...)
	return err
}

// IsFavorite reports whether url is favorited in the store's server scope.
func (s *Store) IsFavorite(url string) (bool, error) {
	if err := s.errIfBroken(); err != nil {
		return false, err
	}
	q := `SELECT 1 FROM favorites WHERE url = ?` + s.serverFilter()
	args := append([]any{url}, s.serverArgs()...)
	var exists int
	err := s.db.QueryRowContext(context.Background(), q, args...).Scan(&exists)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

// GetAllFavorites returns all favorites, sorted newest first, scoped to the
// store's server.
func (s *Store) GetAllFavorites() ([]FavoriteRecord, error) {
	if err := s.errIfBroken(); err != nil {
		return nil, err
	}
	q := `SELECT url, server_id, name, kind, created_at FROM favorites WHERE 1=1`
	var args []any
	if s.serverID != "" {
		q += ` AND server_id = ?`
		args = append(args, s.serverID)
	}
	q += ` ORDER BY created_at DESC`

	rows, err := s.db.QueryContext(context.Background(), q, args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var favs []FavoriteRecord
	for rows.Next() {
		var f FavoriteRecord
		if err := rows.Scan(&f.URL, &f.ServerID, &f.Name, &f.Kind, &f.CreatedAt); err != nil {
			return nil, err
		}
		favs = append(favs, f)
	}
	return favs, rows.Err()
}

// serverFilter returns " AND server_id = ?" for scoped stores, "" otherwise.
func (s *Store) serverFilter() string {
	if s.serverID == "" {
		return ""
	}
	return ` AND server_id = ?`
}

// serverWhere returns " WHERE server_id = ?" for scoped stores, "" otherwise.
func (s *Store) serverWhere() string {
	if s.serverID == "" {
		return ""
	}
	return ` WHERE server_id = ?`
}

func (s *Store) serverArgs() []any {
	if s.serverID == "" {
		return nil
	}
	return []any{s.serverID}
}

func (s *Store) appendServerArg(args []any) []any {
	if s.serverID == "" {
		return args
	}
	return append(args, s.serverID)
}

func scanRuns(rows *sql.Rows) ([]RunRecord, error) {
	defer func() { _ = rows.Close() }()
	var runs []RunRecord
	for rows.Next() {
		var r RunRecord
		if err := rows.Scan(&r.ID, &r.ServerID, &r.JobName, &r.JobURL, &r.BuildID,
			&r.Status, &r.StartTime, &r.EndTime, &r.Duration, &r.CreatedAt); err != nil {
			return nil, err
		}
		// Rows written before jobref.ToDisplayName learned to decode Jenkins'
		// percent-encoded branch names (e.g. a multibranch pipeline branch
		// "merge-queue/PR-58" stored as "merge-queue%2FPR-58") persisted the
		// raw encoded form. Decode on read so historical rows display
		// correctly too, without a DB migration.
		r.JobName = jobref.DecodeName(r.JobName)
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

func isUniqueViolation(err error) bool {
	// modernc.org/sqlite reports SQLITE_CONSTRAINT_UNIQUE as an error whose
	// message contains "constraint failed"; matching the message avoids a
	// hard dependency on the driver's internal error types.
	return err != nil && strings.Contains(err.Error(), "constraint failed")
}
