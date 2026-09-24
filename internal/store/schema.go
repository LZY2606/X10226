package store

const schemaSQL = `
PRAGMA journal_mode=WAL;
PRAGMA foreign_keys=ON;

CREATE TABLE IF NOT EXISTS projects (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  created_seq INTEGER NOT NULL DEFAULT 0,
  created_at  INTEGER NOT NULL,
  last_seq    INTEGER NOT NULL DEFAULT 0,
  last_hash   TEXT NOT NULL DEFAULT '',
  source_fp   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS events (
  seq         INTEGER NOT NULL,
  project     TEXT NOT NULL,
  kind        TEXT NOT NULL,
  occurred_at INTEGER NOT NULL,
  client_id   TEXT NOT NULL DEFAULT '',
  payload     TEXT NOT NULL,
  prev_hash   TEXT NOT NULL DEFAULT '',
  hash        TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (project, seq)
);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project, seq);

CREATE TABLE IF NOT EXISTS raw_docs (
  id          TEXT PRIMARY KEY,
  project     TEXT NOT NULL,
  kind        TEXT NOT NULL,
  layer       TEXT NOT NULL,
  version     INTEGER NOT NULL,
  parent_doc  TEXT NOT NULL DEFAULT '',
  origin      TEXT NOT NULL DEFAULT '',
  committed   INTEGER NOT NULL,
  raw_text    TEXT NOT NULL DEFAULT '',
  content     TEXT NOT NULL,
  superseded  INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_docs_project_layer ON raw_docs(project, layer, version);

CREATE TABLE IF NOT EXISTS import_batches (
  id          TEXT PRIMARY KEY,
  project     TEXT NOT NULL,
  opened_seq  INTEGER NOT NULL,
  closed_seq  INTEGER NOT NULL DEFAULT 0,
  status      TEXT NOT NULL DEFAULT '',
  detail      TEXT NOT NULL DEFAULT ''
);
`
