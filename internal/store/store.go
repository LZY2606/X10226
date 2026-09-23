// Package store is the SQLite-backed persistence layer. All state is
// reproducible from the append-only event ledger: materialized tables are
// rebuildable via Replay().
package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL; PRAGMA busy_timeout=5000;"); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{DB: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS schema_meta(
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS projects(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_seq INTEGER NOT NULL,
  created_at INTEGER NOT NULL
);

-- append-only ledger; seq is the single global ordering within a project
CREATE TABLE IF NOT EXISTS events(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  seq INTEGER NOT NULL,
  type TEXT NOT NULL,
  at_step INTEGER NOT NULL,
  parent_id TEXT,
  payload TEXT NOT NULL,
  UNIQUE(project_id, seq)
);

CREATE TABLE IF NOT EXISTS sources(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  layer TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_id TEXT,
  event_id TEXT NOT NULL,
  submitted_at_step INTEGER NOT NULL,
  fingerprint TEXT NOT NULL,
  status TEXT NOT NULL,
  raw TEXT NOT NULL,
  diagnostics TEXT NOT NULL DEFAULT '[]',
  UNIQUE(project_id, layer, version)
);

CREATE TABLE IF NOT EXISTS runs(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  at_step INTEGER NOT NULL,
  basis TEXT NOT NULL,
  algo_vers TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS artifacts(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  run_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  algo_ver TEXT NOT NULL,
  param_fp TEXT NOT NULL,
  depends_on TEXT NOT NULL,
  current INTEGER NOT NULL,
  superseded_by TEXT,
  payload TEXT NOT NULL,
  diagnostics TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_art_proj_kind ON artifacts(project_id, kind, current);

CREATE TABLE IF NOT EXISTS decisions(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  at_step INTEGER NOT NULL,
  outcome TEXT NOT NULL,
  target_ref TEXT NOT NULL,
  payload TEXT NOT NULL,
  undone INTEGER NOT NULL DEFAULT 0,
  undone_by TEXT,
  note TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS conflicts(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  at_step INTEGER NOT NULL,
  object_ref TEXT NOT NULL,
  decision_ids TEXT NOT NULL,
  reason TEXT NOT NULL,
  resolved INTEGER NOT NULL DEFAULT 0,
  resolved_by TEXT,
  resolution TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS exports(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  at_step INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  event_count INTEGER NOT NULL
);
`

// Tx runs fn in a transaction.
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
	}()
	if err := fn(tx); err != nil {
		if rbErr := tx.Rollback(); rbErr != nil {
			return fmt.Errorf("%w (rollback: %v)", err, rbErr)
		}
		return err
	}
	return tx.Commit()
}
