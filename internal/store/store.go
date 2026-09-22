package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store is the SQLite-backed, append-only project store. All mutations go
// through the event log; materialised heads are an implementation detail that
// can always be rebuilt by replaying events.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) a SQLite database at dsn and migrates it.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	modernc, ok := db.Driver().(interface{ SetMaxOpenConns(int) })
	_ = modernc
	_ = ok
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if _, err := db.Exec(schemaSQL); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for tests/admin queries.
func (s *Store) DB() *sql.DB { return s.db }

const schemaSQL = `
CREATE TABLE IF NOT EXISTS projects (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  seed        INTEGER NOT NULL,
  created_ms  INTEGER NOT NULL,
  archived    INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS events (
  project_id   TEXT NOT NULL,
  seq          INTEGER NOT NULL,
  id           TEXT NOT NULL,
  kind         TEXT NOT NULL,
  occurred_ms  INTEGER NOT NULL,
  payload_json TEXT NOT NULL,
  superseded   INTEGER NOT NULL DEFAULT 0,
  undo_of_seq  INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (project_id, seq)
);
CREATE INDEX IF NOT EXISTS events_kind ON events(project_id, kind);
-- one row per committed source-layer version (append only)
CREATE TABLE IF NOT EXISTS layer_versions (
  project_id  TEXT NOT NULL,
  layer       TEXT NOT NULL,
  version     TEXT NOT NULL,
  event_seq   INTEGER NOT NULL,
  status      TEXT NOT NULL,             -- committed | failed
  parent_version TEXT NOT NULL DEFAULT '',
  payload_json  TEXT NOT NULL DEFAULT '',
  fragment    TEXT NOT NULL DEFAULT '',
  diagnostics_json TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (project_id, version)
);
-- one row per published derived analysis bundle (append only)
CREATE TABLE IF NOT EXISTS bundles (
  project_id   TEXT NOT NULL,
  version      TEXT NOT NULL,
  event_seq    INTEGER NOT NULL,
  param_version TEXT NOT NULL,
  bundle_json  TEXT NOT NULL,
  PRIMARY KEY (project_id, version)
);
-- one row per review decision (append only)
CREATE TABLE IF NOT EXISTS decisions (
  project_id TEXT NOT NULL,
  seq        INTEGER NOT NULL,
  event_id   TEXT NOT NULL,
  decision_json TEXT NOT NULL,
  PRIMARY KEY (project_id, seq)
);
CREATE TABLE IF NOT EXISTS project_meta (
  project_id TEXT PRIMARY KEY,
  event_counter INTEGER NOT NULL DEFAULT 0,
  id_counter INTEGER NOT NULL DEFAULT 0
);
`

// Ping verifies connectivity.
func (s *Store) Ping(ctx context.Context) error {
	return s.db.PingContext(ctx)
}
