package store

const schema = `
CREATE TABLE IF NOT EXISTS meta(
  key TEXT PRIMARY KEY,
  val TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS projects(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS batches(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  created_at TEXT NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS source_versions(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  kind TEXT NOT NULL,
  parent_id TEXT NOT NULL DEFAULT '',
  batch_id TEXT NOT NULL REFERENCES batches(id),
  revision INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  parse_ok INTEGER NOT NULL,
  diagnostic TEXT NOT NULL DEFAULT '',
  active INTEGER NOT NULL,
  raw TEXT NOT NULL DEFAULT '',
  format TEXT NOT NULL DEFAULT 'csv',
  sha256 TEXT NOT NULL DEFAULT '',
  UNIQUE(project_id, kind, revision)
);

CREATE TABLE IF NOT EXISTS sample_points(
  version_id TEXT NOT NULL REFERENCES source_versions(id),
  seq INTEGER NOT NULL,
  time_raw TEXT NOT NULL, temp_raw TEXT NOT NULL, mass_raw TEXT NOT NULL,
  time_v REAL, temp_v REAL, mass_v REAL,
  time_p INTEGER NOT NULL, temp_p INTEGER NOT NULL, mass_p INTEGER NOT NULL,
  unit_time TEXT NOT NULL, unit_temp TEXT NOT NULL, unit_mass TEXT NOT NULL,
  row_note TEXT NOT NULL DEFAULT '',
  PRIMARY KEY(version_id, seq)
);

CREATE TABLE IF NOT EXISTS sample_gaps(
  version_id TEXT NOT NULL REFERENCES source_versions(id),
  seq INTEGER NOT NULL,
  from_v REAL NOT NULL, to_v REAL NOT NULL,
  unit TEXT NOT NULL, raw TEXT NOT NULL,
  PRIMARY KEY(version_id, seq)
);

CREATE TABLE IF NOT EXISTS drift_points(
  version_id TEXT NOT NULL, seq INTEGER NOT NULL,
  time_raw TEXT NOT NULL, drift_raw TEXT NOT NULL,
  time_v REAL, drift_v REAL,
  time_p INTEGER NOT NULL, drift_p INTEGER NOT NULL,
  unit_time TEXT NOT NULL, unit_drift TEXT NOT NULL,
  PRIMARY KEY(version_id, seq)
);

CREATE TABLE IF NOT EXISTS furnace_segs(
  version_id TEXT NOT NULL, seq INTEGER NOT NULL,
  time_raw TEXT NOT NULL, temp_raw TEXT NOT NULL,
  time_v REAL, temp_v REAL,
  time_p INTEGER NOT NULL, temp_p INTEGER NOT NULL,
  unit_time TEXT NOT NULL, unit_temp TEXT NOT NULL,
  seg_kind TEXT NOT NULL,
  PRIMARY KEY(version_id, seq)
);

CREATE TABLE IF NOT EXISTS gas_events(
  version_id TEXT NOT NULL, seq INTEGER NOT NULL,
  time_raw TEXT NOT NULL, gas_raw TEXT NOT NULL, action_raw TEXT NOT NULL,
  time_v REAL, time_p INTEGER NOT NULL, unit_time TEXT NOT NULL,
  gas TEXT NOT NULL, action TEXT NOT NULL,
  PRIMARY KEY(version_id, seq)
);

CREATE TABLE IF NOT EXISTS blank_points(
  version_id TEXT NOT NULL, seq INTEGER NOT NULL,
  time_raw TEXT NOT NULL, mass_raw TEXT NOT NULL,
  time_v REAL, mass_v REAL,
  time_p INTEGER NOT NULL, mass_p INTEGER NOT NULL,
  unit_time TEXT NOT NULL, unit_mass TEXT NOT NULL,
  PRIMARY KEY(version_id, seq)
);

CREATE TABLE IF NOT EXISTS initial_mass(
  version_id TEXT NOT NULL, seq INTEGER NOT NULL,
  mass_raw TEXT NOT NULL, mass_v REAL, mass_p INTEGER NOT NULL,
  unit TEXT NOT NULL, basis TEXT NOT NULL,
  PRIMARY KEY(version_id, seq)
);

CREATE TABLE IF NOT EXISTS param_sets(
  version_id TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL,
  PRIMARY KEY(version_id, key)
);

-- Derived layer -----------------------------------------------------------

CREATE TABLE IF NOT EXISTS runs(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  created_event_id TEXT NOT NULL,
  algorithm_version TEXT NOT NULL,
  param_fingerprint TEXT NOT NULL,
  input_fingerprint TEXT NOT NULL,
  created_at TEXT NOT NULL,
  superseded_by TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS run_inputs(
  run_id TEXT NOT NULL REFERENCES runs(id),
  kind TEXT NOT NULL,
  version_id TEXT NOT NULL,
  PRIMARY KEY(run_id, kind)
);

CREATE TABLE IF NOT EXISTS objects(
  id TEXT NOT NULL,
  run_id TEXT NOT NULL REFERENCES runs(id),
  project_id TEXT NOT NULL REFERENCES projects(id),
  kind TEXT NOT NULL,
  seq INTEGER NOT NULL,
  label TEXT NOT NULL,
  value TEXT NOT NULL DEFAULT '',
  unit TEXT NOT NULL DEFAULT '',
  basis TEXT NOT NULL DEFAULT '',
  status TEXT NOT NULL DEFAULT 'ok', -- ok|unknown|error
  detail TEXT NOT NULL DEFAULT ''   -- JSON, kind-specific payload
);

CREATE TABLE IF NOT EXISTS object_sources(
  run_id TEXT NOT NULL,
  object_id TEXT NOT NULL,
  source_kind TEXT NOT NULL,       -- 'src:samples' | 'obj:...' | 'param'
  source_ref TEXT NOT NULL,        -- version id or object id
  seq INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY(run_id, object_id, source_kind, source_ref, seq)
);

-- Review layer ------------------------------------------------------------

CREATE TABLE IF NOT EXISTS events(
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  project_id TEXT NOT NULL REFERENCES projects(id),
  at TEXT NOT NULL,
  actor TEXT NOT NULL,
  etype TEXT NOT NULL,             -- import|decision|conflict|undo|note|finding...
  summary TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '{}',
  undone_by TEXT NOT NULL DEFAULT '',
  supersedes_event_id TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS decisions(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id),
  event_id TEXT NOT NULL REFERENCES events(id),
  run_id TEXT NOT NULL,
  target_type TEXT NOT NULL,       -- candidate|residue|run
  target_id TEXT NOT NULL,
  verdict TEXT NOT NULL,           -- accept|reject|adjust
  reason TEXT NOT NULL DEFAULT '',
  params_json TEXT NOT NULL DEFAULT '{}',
  undone_by_event_id TEXT NOT NULL DEFAULT '',
  conflicts_with TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS findings(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  run_id TEXT NOT NULL DEFAULT '',
  event_id TEXT NOT NULL DEFAULT '',
  code TEXT NOT NULL,              -- machine-readable finding code
  severity TEXT NOT NULL,          -- info|warn|reject
  subject TEXT NOT NULL DEFAULT '',
  message TEXT NOT NULL,
  detail_json TEXT NOT NULL DEFAULT '{}'
);

CREATE TABLE IF NOT EXISTS counterexample_archive(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  event_id TEXT NOT NULL,
  mutation_code TEXT NOT NULL,
  direct_source TEXT NOT NULL,
  accepted_run_id TEXT NOT NULL DEFAULT '',
  rejected_run_id TEXT NOT NULL DEFAULT '',
  scope_json TEXT NOT NULL DEFAULT '[]',
  created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_sv_project ON source_versions(project_id);
CREATE INDEX IF NOT EXISTS idx_objects_run ON objects(run_id);
CREATE INDEX IF NOT EXISTS idx_objects_project ON objects(project_id);
CREATE INDEX IF NOT EXISTS idx_osrc ON object_sources(run_id, object_id);
CREATE INDEX IF NOT EXISTS idx_decisions_target ON decisions(project_id, target_type, target_id);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, seq);
CREATE INDEX IF NOT EXISTS idx_findings_project ON findings(project_id);
`
