// Package store is the SQLite-backed, append-only persistence layer.
//
// Tables model the layering contract:
//
//	projects, inputs (immutable raw revisions), runs (derived),
//	candidates (first-class proposed/accepted/rejected/indeterminate),
//	events (append-only history), batches (import attempts incl. half-fail),
//	counters (stable id/seq allocation), meta (key/value).
package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store wraps one SQLite database.
type Store struct {
	DB *sql.DB
}

// Open opens (creating if needed) the database at dsn.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{DB: db}, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.DB.Close() }

// WithTx runs fn inside a transaction.
func (s *Store) WithTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

const schema = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS projects (
  id          TEXT PRIMARY KEY,
  title       TEXT NOT NULL,
  conflict    INTEGER NOT NULL DEFAULT 0,
  archived_of TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS inputs (
  id             TEXT PRIMARY KEY,
  project_id     TEXT NOT NULL REFERENCES projects(id),
  kind           TEXT NOT NULL,
  version        INTEGER NOT NULL,
  parent_version INTEGER NOT NULL DEFAULT 0,
  content_type   TEXT NOT NULL,
  raw_text       TEXT NOT NULL,
  import_batch   TEXT NOT NULL DEFAULT '',
  valid          INTEGER NOT NULL DEFAULT 1,
  seed           INTEGER NOT NULL DEFAULT 0,
  parsed_json    TEXT NOT NULL DEFAULT 'null',
  diagnostics    TEXT NOT NULL DEFAULT '[]',
  created_at     TEXT NOT NULL,
  UNIQUE(project_id, kind, version)
);

CREATE TABLE IF NOT EXISTS runs (
  id                TEXT PRIMARY KEY,
  project_id        TEXT NOT NULL REFERENCES projects(id),
  algo_version      TEXT NOT NULL,
  param_fingerprint TEXT NOT NULL,
  sources_json      TEXT NOT NULL,
  current           INTEGER NOT NULL DEFAULT 1,
  diagnostics_json  TEXT NOT NULL DEFAULT '[]',
  payload_json      TEXT NOT NULL,
  created_at        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS candidates (
  id         TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  run_id     TEXT NOT NULL,
  type       TEXT NOT NULL,
  alt_group  TEXT NOT NULL DEFAULT '',
  parent_id  TEXT NOT NULL DEFAULT '',
  status     TEXT NOT NULL,
  summary    TEXT NOT NULL DEFAULT '',
  detail_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
  project_id TEXT NOT NULL REFERENCES projects(id),
  seq        INTEGER NOT NULL,
  id         TEXT NOT NULL,
  type       TEXT NOT NULL,
  undone     INTEGER NOT NULL DEFAULT 0,
  undoes     INTEGER NOT NULL DEFAULT 0,
  actor      TEXT NOT NULL DEFAULT '',
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(project_id, seq)
);

CREATE TABLE IF NOT EXISTS batches (
  id         TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  label      TEXT NOT NULL DEFAULT '',
  sections_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS counters (
  project_id TEXT NOT NULL,
  name       TEXT NOT NULL,
  value      INTEGER NOT NULL,
  PRIMARY KEY(project_id, name)
);

CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_inputs_proj ON inputs(project_id, kind);
CREATE INDEX IF NOT EXISTS idx_runs_proj ON runs(project_id);
CREATE INDEX IF NOT EXISTS idx_cand_proj ON candidates(project_id);
CREATE INDEX IF NOT EXISTS idx_events_proj ON events(project_id, seq);
`
