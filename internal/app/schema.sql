PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS projects (
  project_id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  material_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  clock_family TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
  project_id TEXT NOT NULL REFERENCES projects(project_id),
  seq INTEGER NOT NULL,
  event_id TEXT NOT NULL,
  event_type TEXT NOT NULL,
  actor TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  previous_hash TEXT NOT NULL,
  event_hash TEXT NOT NULL,
  PRIMARY KEY (project_id, seq),
  UNIQUE (project_id, event_id)
);

CREATE INDEX IF NOT EXISTS events_project_time ON events(project_id, seq);

CREATE TABLE IF NOT EXISTS raw_records (
  project_id TEXT NOT NULL,
  record_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_version INTEGER,
  source_type TEXT NOT NULL,
  material_id TEXT NOT NULL,
  submitted_at TEXT NOT NULL,
  original_text TEXT NOT NULL,
  normalized_json TEXT NOT NULL,
  status TEXT NOT NULL,
  diagnostics_json TEXT NOT NULL DEFAULT '[]',
  event_id TEXT NOT NULL,
  PRIMARY KEY (project_id, record_id, version)
);

CREATE TABLE IF NOT EXISTS active_records (
  project_id TEXT NOT NULL,
  source_type TEXT NOT NULL,
  record_id TEXT NOT NULL,
  version INTEGER NOT NULL,
  PRIMARY KEY (project_id, source_type, record_id)
);

CREATE TABLE IF NOT EXISTS derived_snapshots (
  project_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  node_id TEXT NOT NULL,
  node_kind TEXT NOT NULL,
  algorithm_version TEXT NOT NULL,
  parameter_fingerprint TEXT NOT NULL,
  dependency_fingerprint TEXT NOT NULL,
  created_at TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  PRIMARY KEY (project_id, node_id)
);

CREATE TABLE IF NOT EXISTS key_values (
  project_id TEXT NOT NULL,
  key TEXT NOT NULL,
  value TEXT NOT NULL,
  PRIMARY KEY (project_id, key)
);
