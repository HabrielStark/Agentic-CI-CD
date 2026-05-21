// Package store provides a tiny SQLite-backed local history of runs,
// fingerprints and classifications used by the flake detector and diagnoser
// for trend insights. Pure Go implementation via modernc.org/sqlite.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// Store is a thin wrapper around a SQLite connection.
type Store struct {
	db *sql.DB
}

// Open opens (and migrates) a store at the given path. The directory must
// already exist or will be created.
func Open(path string) (*Store, error) {
	if path == "" {
		return nil, errors.New("store: empty path")
	}
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		// best effort
	}
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS runs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			repo TEXT NOT NULL,
			provider TEXT NOT NULL,
			workflow TEXT NOT NULL,
			job TEXT NOT NULL,
			run_id INTEGER NOT NULL,
			commit_sha TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			category TEXT,
			confidence REAL,
			created_at TEXT NOT NULL,
			capsule_path TEXT
		);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_fingerprint ON runs(fingerprint);`,
		`CREATE INDEX IF NOT EXISTS idx_runs_repo ON runs(repo);`,
		`CREATE TABLE IF NOT EXISTS flake (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			fingerprint TEXT NOT NULL,
			target TEXT NOT NULL,
			runs INTEGER NOT NULL,
			passed INTEGER NOT NULL,
			failed INTEGER NOT NULL,
			skipped INTEGER NOT NULL,
			mode TEXT,
			created_at TEXT NOT NULL
		);`,
		`CREATE INDEX IF NOT EXISTS idx_flake_fingerprint ON flake(fingerprint);`,
		`CREATE TABLE IF NOT EXISTS replays (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			fingerprint TEXT NOT NULL,
			mode TEXT NOT NULL,
			network TEXT NOT NULL,
			exit_code INTEGER NOT NULL,
			reproduced INTEGER NOT NULL,
			duration_ms INTEGER NOT NULL,
			created_at TEXT NOT NULL
		);`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// RunRecord is the shape stored in `runs`.
type RunRecord struct {
	ID          int64
	Repo        string
	Provider    string
	Workflow    string
	Job         string
	RunID       int64
	CommitSHA   string
	Fingerprint string
	Category    string
	Confidence  float64
	CreatedAt   time.Time
	CapsulePath string
}

// Insert records a diagnosis result.
func (s *Store) Insert(ctx context.Context, r RunRecord) (int64, error) {
	r.CreatedAt = nonZeroTime(r.CreatedAt)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO runs (repo, provider, workflow, job, run_id, commit_sha, fingerprint, category, confidence, created_at, capsule_path) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		r.Repo, r.Provider, r.Workflow, r.Job, r.RunID, r.CommitSHA, r.Fingerprint, r.Category, r.Confidence, r.CreatedAt.Format(time.RFC3339Nano), r.CapsulePath)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// CountByFingerprint returns total times a fingerprint has been observed.
func (s *Store) CountByFingerprint(ctx context.Context, fp string) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM runs WHERE fingerprint = ?`, fp)
	var n int
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// FlakeStat is the shape stored in `flake`.
type FlakeStat struct {
	ID          int64
	Fingerprint string
	Target      string
	Runs        int
	Passed      int
	Failed      int
	Skipped     int
	Mode        string
	CreatedAt   time.Time
}

// InsertFlake stores a flake detection summary.
func (s *Store) InsertFlake(ctx context.Context, f FlakeStat) (int64, error) {
	f.CreatedAt = nonZeroTime(f.CreatedAt)
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO flake (fingerprint, target, runs, passed, failed, skipped, mode, created_at) VALUES (?,?,?,?,?,?,?,?)`,
		f.Fingerprint, f.Target, f.Runs, f.Passed, f.Failed, f.Skipped, f.Mode, f.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ReplayRecord is the shape stored in `replays`.
type ReplayRecord struct {
	ID          int64
	Fingerprint string
	Mode        string
	Network     string
	ExitCode    int
	Reproduced  bool
	DurationMs  int64
	CreatedAt   time.Time
}

// InsertReplay records a replay outcome.
func (s *Store) InsertReplay(ctx context.Context, r ReplayRecord) (int64, error) {
	r.CreatedAt = nonZeroTime(r.CreatedAt)
	repro := 0
	if r.Reproduced {
		repro = 1
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO replays (fingerprint, mode, network, exit_code, reproduced, duration_ms, created_at) VALUES (?,?,?,?,?,?,?)`,
		r.Fingerprint, r.Mode, r.Network, r.ExitCode, repro, r.DurationMs, r.CreatedAt.Format(time.RFC3339Nano))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// HistoryByFingerprint returns recent runs for a fingerprint (most recent first, limited).
func (s *Store) HistoryByFingerprint(ctx context.Context, fp string, limit int) ([]RunRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo, provider, workflow, job, run_id, commit_sha, fingerprint, category, confidence, created_at, capsule_path FROM runs WHERE fingerprint = ? ORDER BY id DESC LIMIT ?`,
		fp, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunRecord
	for rows.Next() {
		var r RunRecord
		var ts string
		if err := rows.Scan(&r.ID, &r.Repo, &r.Provider, &r.Workflow, &r.Job, &r.RunID, &r.CommitSHA, &r.Fingerprint, &r.Category, &r.Confidence, &ts, &r.CapsulePath); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339Nano, ts)
		r.CreatedAt = t
		out = append(out, r)
	}
	return out, rows.Err()
}

// Stats returns aggregate stats for a repository.
type Stats struct {
	TotalRuns int
	Categories map[string]int
}

// Aggregate returns aggregate counts by category for a repo.
func (s *Store) Aggregate(ctx context.Context, repo string) (Stats, error) {
	stats := Stats{Categories: map[string]int{}}
	rows, err := s.db.QueryContext(ctx, `SELECT category, COUNT(*) FROM runs WHERE repo = ? GROUP BY category`, repo)
	if err != nil {
		return stats, err
	}
	defer rows.Close()
	for rows.Next() {
		var cat string
		var n int
		if err := rows.Scan(&cat, &n); err != nil {
			return stats, err
		}
		stats.Categories[cat] = n
		stats.TotalRuns += n
	}
	return stats, rows.Err()
}

func nonZeroTime(t time.Time) time.Time {
	if t.IsZero() {
		return time.Now().UTC()
	}
	return t.UTC()
}


// HistoryByCategory returns recent runs for a repo and category (newest first).
func (s *Store) HistoryByCategory(ctx context.Context, repo, category string, limit int) ([]RunRecord, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, repo, provider, workflow, job, run_id, commit_sha, fingerprint, category, confidence, created_at, capsule_path FROM runs WHERE repo = ? AND category = ? ORDER BY id DESC LIMIT ?`,
		repo, category, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunRecord
	for rows.Next() {
		var r RunRecord
		var ts string
		if err := rows.Scan(&r.ID, &r.Repo, &r.Provider, &r.Workflow, &r.Job, &r.RunID, &r.CommitSHA, &r.Fingerprint, &r.Category, &r.Confidence, &ts, &r.CapsulePath); err != nil {
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339Nano, ts)
		r.CreatedAt = t
		out = append(out, r)
	}
	return out, rows.Err()
}
