package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL,
  clock TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY,
  id TEXT NOT NULL UNIQUE,
  project_id TEXT NOT NULL,
  type TEXT NOT NULL,
  at TEXT NOT NULL,
  actor TEXT NOT NULL,
  prev_hash TEXT NOT NULL,
  hash TEXT NOT NULL,
  payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, seq);
CREATE TABLE IF NOT EXISTS sources (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  layer TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_id TEXT,
  filename TEXT NOT NULL,
  media_type TEXT NOT NULL,
  raw_text TEXT NOT NULL,
  parsed TEXT NOT NULL,
  import_batch TEXT NOT NULL,
  committed_at TEXT NOT NULL,
  superseded INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_sources_project ON sources(project_id, layer, version);
CREATE TABLE IF NOT EXISTS import_batches (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  committed_at TEXT NOT NULL,
  status TEXT NOT NULL,
  detail TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS params (
  fingerprint TEXT PRIMARY KEY,
  body TEXT NOT NULL,
  first_seen_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS analyses (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  source_ids TEXT NOT NULL,
  params_fingerprint TEXT NOT NULL,
  body TEXT NOT NULL,
  created_at TEXT NOT NULL,
  current INTEGER NOT NULL DEFAULT 1
);
CREATE INDEX IF NOT EXISTS idx_analyses_project ON analyses(project_id, seq);
CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  at_event_seq INTEGER NOT NULL,
  target_kind TEXT NOT NULL,
  target_id TEXT NOT NULL,
  analysis_id TEXT NOT NULL,
  action TEXT NOT NULL,
  reason TEXT NOT NULL,
  reviewer TEXT NOT NULL,
  adjust TEXT NOT NULL,
  undone INTEGER NOT NULL DEFAULT 0,
  undone_by TEXT,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_decisions_target ON decisions(project_id, target_id, at_event_seq);
`

// Open opens/creates a SQLite database at path ("" => shared memory is not
// used; callers pass a temp path in tests) and ensures the schema exists.
func Open(path string) (*Store, error) {
	dsn := path
	if path != ":memory:" {
		dsn = "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	s := &Store{db: db}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
