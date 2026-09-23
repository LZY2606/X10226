package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store is the SQLite-backed event log and derived cache.
type Store struct{ DB *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_ts TEXT NOT NULL,
  meta TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  project_id TEXT NOT NULL REFERENCES projects(id),
  kind TEXT NOT NULL,
  payload TEXT NOT NULL,
  revokes TEXT,
  actor TEXT,
  note TEXT,
  ts TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, seq);
CREATE TABLE IF NOT EXISTS documents (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  label TEXT,
  source TEXT,
  doc_seq INTEGER NOT NULL,
  parent_id TEXT,
  born_seq INTEGER NOT NULL DEFAULT 0,
  raw_text TEXT NOT NULL,
  payload TEXT NOT NULL,
  diags TEXT NOT NULL DEFAULT '[]',
  ingest_ts TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_docs_project_kind ON documents(project_id, kind, doc_seq);
CREATE TABLE IF NOT EXISTS batches (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  event_seq INTEGER NOT NULL,
  partial INTEGER NOT NULL,
  accepted TEXT NOT NULL,
  rejected TEXT NOT NULL,
  raw_text TEXT NOT NULL,
  ts TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS derived (
  id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  json TEXT NOT NULL,
  born_seq INTEGER NOT NULL,
  stale_seq INTEGER,
  stale_reason TEXT,
  PRIMARY KEY(project_id, id)
);
CREATE INDEX IF NOT EXISTS idx_derived_project ON derived(project_id, born_seq);
CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  event_seq INTEGER NOT NULL,
  subject_type TEXT NOT NULL,
  subject_id TEXT NOT NULL,
  verdict TEXT NOT NULL,
  reason TEXT,
  revoked INTEGER NOT NULL DEFAULT 0,
  conflict_with TEXT NOT NULL DEFAULT '[]',
  resolved_by TEXT,
  winner INTEGER NOT NULL DEFAULT 0,
  adjustment TEXT,
  ts TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_decisions_subject ON decisions(project_id, subject_id);
CREATE TABLE IF NOT EXISTS kv (
  project_id TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY(project_id, key)
);
`

func (s *Store) migrate() error {
	_, err := s.DB.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
