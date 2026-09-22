// Package storage persists projects, the append-only event log, immutable raw
// source records and content-addressed artifacts in SQLite.
package storage

import (
	"database/sql"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  sample_id TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id TEXT NOT NULL,
  type TEXT NOT NULL,
  at TEXT NOT NULL,
  actor TEXT NOT NULL,
  payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, seq);
CREATE TABLE IF NOT EXISTS source_records (
  id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  layer_index INTEGER NOT NULL,
  line_no INTEGER NOT NULL,
  raw_text TEXT NOT NULL,
  fields TEXT NOT NULL,
  parent_id TEXT NOT NULL DEFAULT '',
  revision INTEGER NOT NULL,
  status TEXT NOT NULL,
  diagnostic TEXT NOT NULL DEFAULT '',
  import_seq INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY(project_id, id)
);
CREATE INDEX IF NOT EXISTS idx_src_project ON source_records(project_id, import_seq);
CREATE TABLE IF NOT EXISTS artifacts (
  id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  family TEXT NOT NULL,
  label TEXT NOT NULL,
  algo_version TEXT NOT NULL,
  param_fp TEXT NOT NULL,
  source_ids TEXT NOT NULL,
  depends_on TEXT NOT NULL,
  created_seq INTEGER NOT NULL,
  payload TEXT NOT NULL,
  PRIMARY KEY(project_id, id)
);
CREATE INDEX IF NOT EXISTS idx_art_project ON artifacts(project_id, family, created_seq);
CREATE TABLE IF NOT EXISTS archive_runs (
  id TEXT PRIMARY KEY,
  baseline_project_id TEXT NOT NULL,
  perturbation TEXT NOT NULL,
  changed_source TEXT NOT NULL,
  project_id TEXT NOT NULL,
  report TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS kv (
  k TEXT PRIMARY KEY,
  v TEXT NOT NULL
);
`

func Open(dsn string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}
