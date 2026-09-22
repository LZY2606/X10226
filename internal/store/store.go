// Package store persists the event log, materialized snapshots and import
// archives in SQLite. The event log is the source of truth; snapshots make
// time travel cheap and are rebuildable by replay.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite database.
type Store struct {
	db *sql.DB
}

// Open opens (creating the schema) a database at dsn.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the underlying handle for backups/PRAGMAs.
func (s *Store) DB() *sql.DB { return s.db }

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  project_id TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  origin     TEXT NOT NULL DEFAULT 'api',
  seed       INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  project_id TEXT NOT NULL,
  seq        INTEGER NOT NULL,
  event_id   TEXT NOT NULL,
  at         TEXT NOT NULL,
  actor      TEXT NOT NULL,
  type       TEXT NOT NULL,
  parent_id  TEXT NOT NULL DEFAULT '',
  payload    BLOB NOT NULL,
  PRIMARY KEY (project_id, seq)
);
CREATE UNIQUE INDEX IF NOT EXISTS events_eid ON events(project_id, event_id);
CREATE TABLE IF NOT EXISTS snapshots (
  project_id TEXT NOT NULL,
  seq        INTEGER NOT NULL,
  view       BLOB NOT NULL,
  state      BLOB NOT NULL,
  PRIMARY KEY (project_id, seq)
);
CREATE TABLE IF NOT EXISTS head (
  project_id TEXT PRIMARY KEY,
  seq        INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS archives (
  archive_id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  imported_at TEXT NOT NULL,
  payload    BLOB NOT NULL
);
`
