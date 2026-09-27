package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store wraps a SQLite connection. The schema is append-only: raw sources and
// history events are never UPDATEd or DELETEd; only lifecycle flags such as
// runs.current / decisions.revoked are mutable, and every such transition is
// itself recorded as an event.
type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec("PRAGMA foreign_keys = ON; PRAGMA journal_mode=WAL;"); err != nil {
		return nil, fmt.Errorf("pragma: %w", err)
	}
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

const ddl = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  note TEXT DEFAULT '',
  seed INTEGER NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sources (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  kind TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_source_id TEXT DEFAULT '',
  label TEXT NOT NULL,
  raw_name TEXT NOT NULL,
  raw_text TEXT NOT NULL,
  raw_encoding TEXT NOT NULL,
  checksum TEXT NOT NULL,
  status TEXT NOT NULL,
  rejected_rows INTEGER NOT NULL DEFAULT 0,
  accepted_rows INTEGER NOT NULL DEFAULT 0,
  seq INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(project_id, kind, version)
);

CREATE TABLE IF NOT EXISTS source_rows (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  source_id TEXT NOT NULL REFERENCES sources(id),
  row_index INTEGER NOT NULL,
  raw_line TEXT NOT NULL,
  fields_json TEXT DEFAULT '',
  parse_ok INTEGER NOT NULL,
  diagnostic TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_rows_src ON source_rows(source_id, row_index);

CREATE TABLE IF NOT EXISTS diagnostics (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id TEXT NOT NULL,
  source_id TEXT DEFAULT '',
  run_id TEXT DEFAULT '',
  row_index INTEGER DEFAULT -1,
  severity TEXT NOT NULL,
  code TEXT NOT NULL,
  message TEXT NOT NULL,
  raw_ref TEXT DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_diag_proj ON diagnostics(project_id);

CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id TEXT NOT NULL,
  type TEXT NOT NULL,
  at TEXT NOT NULL,
  actor TEXT NOT NULL,
  subject_id TEXT DEFAULT '',
  payload_json TEXT DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_events_proj ON events(project_id, seq);

CREATE TABLE IF NOT EXISTS parameters (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  version INTEGER NOT NULL,
  parent_id TEXT DEFAULT '',
  fingerprint TEXT NOT NULL,
  algorithm_version TEXT NOT NULL,
  atmos_delay_s REAL NOT NULL,
  thermal_lag_s REAL NOT NULL,
  dtg_peak_prominence REAL NOT NULL,
  dtg_peak_min_sep_s REAL NOT NULL,
  smooth_window_s REAL NOT NULL,
  allow_overlap INTEGER NOT NULL,
  gap_max_interp_s REAL NOT NULL,
  note TEXT DEFAULT '',
  created_at TEXT NOT NULL,
  UNIQUE(project_id, version)
);

CREATE TABLE IF NOT EXISTS runs (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  seq INTEGER NOT NULL,
  algorithm_version TEXT NOT NULL,
  param_id TEXT NOT NULL,
  param_fingerprint TEXT NOT NULL,
  source_set TEXT NOT NULL,
  current INTEGER NOT NULL DEFAULT 0,
  superseded_by TEXT DEFAULT '',
  note TEXT DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_runs_proj ON runs(project_id, seq);

CREATE TABLE IF NOT EXISTS run_sources (
  run_id TEXT NOT NULL REFERENCES runs(id),
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  source_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  row_indexes_json TEXT DEFAULT '[]'
);

CREATE TABLE IF NOT EXISTS stage_candidates (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES runs(id),
  idx INTEGER NOT NULL,
  start_s REAL NOT NULL,
  end_s REAL NOT NULL,
  peak_s REAL NOT NULL,
  peak_dtg REAL NOT NULL,
  mass_loss_pct REAL NOT NULL,
  mass_loss_err_pct REAL NOT NULL,
  overlaps_json TEXT DEFAULT '[]',
  forced_sum_100 INTEGER NOT NULL,
  state TEXT NOT NULL,
  rejected_reason TEXT DEFAULT '',
  unknown INTEGER NOT NULL DEFAULT 0,
  unknown_reason TEXT DEFAULT '',
  atmos_reaction_s REAL NOT NULL,
  valve_s REAL NOT NULL DEFAULT 0,
  sources_json TEXT DEFAULT '[]'
);
CREATE INDEX IF NOT EXISTS idx_stage_run ON stage_candidates(run_id);

CREATE TABLE IF NOT EXISTS residues (
  id TEXT PRIMARY KEY,
  run_id TEXT NOT NULL REFERENCES runs(id),
  at_s REAL NOT NULL,
  residue_pct REAL NOT NULL,
  err_pct REAL NOT NULL,
  unknown INTEGER NOT NULL,
  unknown_reason TEXT DEFAULT '',
  chain_json TEXT NOT NULL,
  sources_json TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_res_run ON residues(run_id);

CREATE TABLE IF NOT EXISTS rule_evidence (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id TEXT NOT NULL,
  code TEXT NOT NULL,
  statement TEXT NOT NULL,
  subject_id TEXT DEFAULT '',
  run_id TEXT DEFAULT '',
  data_json TEXT DEFAULT '{}',
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_rule_proj ON rule_evidence(project_id);

CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  run_id TEXT NOT NULL,
  target_kind TEXT NOT NULL,
  target_id TEXT NOT NULL,
  verdict TEXT NOT NULL,
  note TEXT DEFAULT '',
  param_id TEXT DEFAULT '',
  revoked INTEGER NOT NULL DEFAULT 0,
  revokes_id TEXT DEFAULT '',
  conflict_of TEXT DEFAULT '',
  seq INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_dec_proj ON decisions(project_id, seq);

CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
`

func (s *Store) migrate() error {
	_, err := s.DB.Exec(ddl)
	return err
}
