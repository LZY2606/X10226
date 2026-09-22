// Package store persists projects, append-only events, immutable raw sources
// and content-addressed derived nodes in SQLite.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store wraps one SQLite database file.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path. Use
// "file:/...?mode=memory" or ":memory:" for ephemeral tests.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys=ON; PRAGMA busy_timeout=5000;"); err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  note TEXT DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER NOT NULL,
  id TEXT NOT NULL,
  project_id TEXT NOT NULL REFERENCES projects(id),
  type TEXT NOT NULL,
  payload TEXT NOT NULL,
  actor TEXT DEFAULT '',
  created_at TEXT NOT NULL,
  PRIMARY KEY(project_id, seq)
);
CREATE TABLE IF NOT EXISTS raw_sources (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_id TEXT DEFAULT '',
  batch_id TEXT NOT NULL,
  filename TEXT DEFAULT '',
  text TEXT NOT NULL,
  payload TEXT NOT NULL,
  diags TEXT DEFAULT '[]',
  revision TEXT DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(project_id, kind, version)
);
CREATE TABLE IF NOT EXISTS batches (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  label TEXT DEFAULT '',
  status TEXT NOT NULL,
  accepted TEXT DEFAULT '[]',
  accepted_kinds TEXT DEFAULT '[]',
  rejected TEXT DEFAULT '[]',
  diag_count INTEGER DEFAULT 0,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS derived_nodes (
  id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  slot TEXT NOT NULL,
  kind TEXT NOT NULL,
  params_id TEXT NOT NULL,
  json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(project_id, id)
);
CREATE TABLE IF NOT EXISTS keyvals (
  project_id TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY(project_id, key)
);
`

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

func jget(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func mustb(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Errorf("marshal: %w", err))
	}
	return b
}
