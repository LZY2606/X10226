// Package store is the SQLite-backed, event-sourced persistence layer. Events
// are append-only; sources are immutable versions; artifacts carry algorithm
// version, parameter fingerprint and direct source inputs.
package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"tgareview/internal/clock"
)

// Store wraps one SQLite database.
type Store struct {
	db    *sql.DB
	clock clock.Clock
}

// Open opens (and migrates) a database file. Use ":memory:" for tests.
func Open(ctx context.Context, dsn string, c clock.Clock) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;"); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, clock: c}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for the export/import helpers.
func (s *Store) DB() *sql.DB { return s.db }

// Clock returns the configured clock.
func (s *Store) Clock() clock.Clock { return s.clock }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

const schema = `
CREATE TABLE IF NOT EXISTS meta(k TEXT PRIMARY KEY, v TEXT NOT NULL);

CREATE TABLE IF NOT EXISTS projects(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events(
  project_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  id TEXT NOT NULL,
  parent_id TEXT NOT NULL,
  type TEXT NOT NULL,
  at TEXT NOT NULL,
  actor TEXT NOT NULL DEFAULT '',
  summary TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL DEFAULT '{}',
  PRIMARY KEY(project_id, seq)
);
CREATE INDEX IF NOT EXISTS events_proj_time ON events(project_id, seq);

CREATE TABLE IF NOT EXISTS sources(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_version INTEGER NOT NULL DEFAULT 0,
  filename TEXT NOT NULL,
  header TEXT NOT NULL DEFAULT '[]',
  unit_decl TEXT NOT NULL DEFAULT '{}',
  accepted INTEGER NOT NULL DEFAULT 0,
  rejected INTEGER NOT NULL DEFAULT 0,
  commit_event_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(project_id, kind, version)
);

CREATE TABLE IF NOT EXISTS source_rows(
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id TEXT NOT NULL,
  line INTEGER NOT NULL,
  raw TEXT NOT NULL,
  cells TEXT NOT NULL DEFAULT '{}',
  accepted INTEGER NOT NULL,
  diagnostic TEXT NOT NULL DEFAULT '',
  UNIQUE(source_id, line)
);

CREATE TABLE IF NOT EXISTS artifacts(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  algo_version TEXT NOT NULL,
  param_fp TEXT NOT NULL,
  inputs TEXT NOT NULL,
  deriv_event TEXT NOT NULL,
  seq INTEGER NOT NULL,
  data TEXT NOT NULL,
  candidates TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS artifacts_proj_kind ON artifacts(project_id, kind);

CREATE TABLE IF NOT EXISTS decisions(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  event_seq INTEGER NOT NULL,
  event_id TEXT NOT NULL,
  candidate_id TEXT NOT NULL,
  candidate_kind TEXT NOT NULL,
  action TEXT NOT NULL,
  rationale TEXT NOT NULL DEFAULT '',
  params TEXT NOT NULL DEFAULT '{}',
  active INTEGER NOT NULL DEFAULT 1,
  revoked_by_event TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS decisions_cand ON decisions(project_id, candidate_id, active);

CREATE TABLE IF NOT EXISTS kv(
  project_id TEXT NOT NULL,
  k TEXT NOT NULL,
  v TEXT NOT NULL,
  seq INTEGER NOT NULL,
  PRIMARY KEY(project_id, k)
);
`
