// Package store persists the append-only event log in SQLite. The log is the
// only source of truth; the artifacts projection is rebuilt by deterministic
// replay, which is what export -> delete database -> import verifies.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store wraps one SQLite database file (or :memory:).
type Store struct{ db *sql.DB }

// Open opens (creating the schema if needed) a database at dsn.
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	st := &Store{db: db}
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  created_at  TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  project_id  TEXT NOT NULL,
  seq         INTEGER NOT NULL,
  id          TEXT NOT NULL,
  at          TEXT NOT NULL,
  type        TEXT NOT NULL,
  parent_seq  INTEGER NOT NULL DEFAULT 0,
  payload     TEXT NOT NULL,
  PRIMARY KEY (project_id, seq)
);
CREATE INDEX IF NOT EXISTS events_id ON events(project_id, id);
-- projection, rebuilt by replay; kept so stale identities remain queryable
CREATE TABLE IF NOT EXISTS artifacts (
  project_id  TEXT NOT NULL,
  value_id    TEXT NOT NULL,
  kind        TEXT NOT NULL,
  first_seq   INTEGER NOT NULL,
  latest_seq  INTEGER NOT NULL,
  PRIMARY KEY (project_id, value_id)
);
CREATE TABLE IF NOT EXISTS meta (k TEXT PRIMARY KEY, v TEXT NOT NULL);
`

// CountEvents is used by persistence tests.
func (s *Store) CountEvents(projectID string) (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events WHERE project_id=?`, projectID).Scan(&n)
	return n, err
}

func (s *Store) assert() {
	if s.db == nil {
		panic("store closed")
	}
}

var _ = fmt.Sprintf
