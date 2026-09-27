package store

const schema = `
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS projects (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  created_at TEXT NOT NULL,
  seed       INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS batches (
  id         TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  seq        INTEGER NOT NULL,
  status     TEXT NOT NULL,
  reason     TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sources (
  id         TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  batch_id   TEXT NOT NULL,
  kind       TEXT NOT NULL,
  version    INTEGER NOT NULL,
  parent_id  TEXT NOT NULL DEFAULT '',
  status     TEXT NOT NULL,
  raw_text   TEXT NOT NULL,
  diagnostic TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL,
  seq        INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS points (
  source_id  TEXT NOT NULL,
  idx        INTEGER NOT NULL,
  time_raw   TEXT NOT NULL,
  time_unit  TEXT NOT NULL,
  temp_raw   TEXT NOT NULL,
  temp_unit  TEXT NOT NULL,
  mass_raw   TEXT NOT NULL,
  mass_unit  TEXT NOT NULL,
  time_s     REAL NOT NULL DEFAULT 0,
  temp_c     REAL NOT NULL DEFAULT 0,
  mass_mg    REAL NOT NULL DEFAULT 0,
  time_ok    INTEGER NOT NULL,
  temp_ok    INTEGER NOT NULL,
  mass_ok    INTEGER NOT NULL,
  diagnostic TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (source_id, idx)
);

CREATE TABLE IF NOT EXISTS blank_points (
  source_id TEXT NOT NULL,
  idx       INTEGER NOT NULL,
  time_s    REAL NOT NULL DEFAULT 0,
  mass_mg   REAL NOT NULL DEFAULT 0,
  raw       TEXT NOT NULL,
  ok        INTEGER NOT NULL,
  PRIMARY KEY (source_id, idx)
);

CREATE TABLE IF NOT EXISTS program_steps (
  source_id    TEXT NOT NULL,
  idx          INTEGER NOT NULL,
  start_s      REAL NOT NULL DEFAULT 0,
  end_s        REAL NOT NULL DEFAULT 0,
  from_c       REAL NOT NULL DEFAULT 0,
  to_c         REAL NOT NULL DEFAULT 0,
  rate_c_min   REAL NOT NULL DEFAULT 0,
  raw          TEXT NOT NULL,
  PRIMARY KEY (source_id, idx)
);

CREATE TABLE IF NOT EXISTS valve_events (
  source_id TEXT NOT NULL,
  idx       INTEGER NOT NULL,
  at_s      REAL NOT NULL DEFAULT 0,
  gas       TEXT NOT NULL,
  flow_sccm REAL NOT NULL DEFAULT 0,
  raw       TEXT NOT NULL,
  ok        INTEGER NOT NULL,
  PRIMARY KEY (source_id, idx)
);

CREATE TABLE IF NOT EXISTS initial_masses (
  source_id    TEXT PRIMARY KEY,
  reported_mg  REAL NOT NULL DEFAULT 0,
  measured_mg  REAL NOT NULL DEFAULT 0,
  adopted_mg   REAL NOT NULL DEFAULT 0,
  reported_raw TEXT NOT NULL DEFAULT '',
  measured_raw TEXT NOT NULL DEFAULT '',
  reason       TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS diagnostics (
  id         TEXT PRIMARY KEY,
  batch_id   TEXT NOT NULL,
  project_id TEXT NOT NULL,
  section    TEXT NOT NULL,
  severity   TEXT NOT NULL,
  message    TEXT NOT NULL,
  raw_ref    TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS derived (
  id                TEXT PRIMARY KEY,
  project_id        TEXT NOT NULL,
  version           INTEGER NOT NULL,
  parent_id         TEXT NOT NULL DEFAULT '',
  status            TEXT NOT NULL,
  algo_version      TEXT NOT NULL,
  param_fingerprint TEXT NOT NULL,
  trigger           TEXT NOT NULL,
  sample_source_id  TEXT NOT NULL,
  blank_source_id   TEXT NOT NULL,
  program_source_id TEXT NOT NULL,
  valve_source_id   TEXT NOT NULL,
  mass_source_id    TEXT NOT NULL,
  gap_start_s       REAL NOT NULL DEFAULT 0,
  gap_end_s         REAL NOT NULL DEFAULT 0,
  has_gap           INTEGER NOT NULL DEFAULT 0,
  drift_mg          REAL NOT NULL DEFAULT 0,
  lag_s             REAL NOT NULL DEFAULT 0,
  temp_lag_s        REAL NOT NULL DEFAULT 0,
  created_at        TEXT NOT NULL,
  seq               INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS findings (
  id         TEXT PRIMARY KEY,
  derived_id TEXT NOT NULL,
  project_id TEXT NOT NULL,
  type       TEXT NOT NULL,
  severity   TEXT NOT NULL,
  at_s       REAL NOT NULL DEFAULT 0,
  end_s      REAL NOT NULL DEFAULT 0,
  message    TEXT NOT NULL,
  evidence   TEXT NOT NULL DEFAULT '',
  rule       TEXT NOT NULL DEFAULT '',
  sort_key   INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS peaks (
  id            TEXT PRIMARY KEY,
  derived_id    TEXT NOT NULL,
  at_s          REAL NOT NULL,
  at_reaction_s REAL NOT NULL,
  dtg           REAL NOT NULL,
  height        REAL NOT NULL,
  window_start  REAL NOT NULL,
  window_end    REAL NOT NULL,
  overlap_id    TEXT NOT NULL DEFAULT '',
  note          TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS overlap_groups (
  id        TEXT PRIMARY KEY,
  derived_id TEXT NOT NULL,
  peak_ids  TEXT NOT NULL,
  start_s   REAL NOT NULL,
  end_s     REAL NOT NULL
);

CREATE TABLE IF NOT EXISTS stage_candidates (
  id                TEXT PRIMARY KEY,
  derived_id        TEXT NOT NULL,
  project_id        TEXT NOT NULL,
  label             TEXT NOT NULL,
  start_s           REAL NOT NULL,
  end_s             REAL NOT NULL,
  reaction_start_s  REAL NOT NULL,
  reaction_end_s    REAL NOT NULL,
  start_c           REAL NOT NULL,
  end_c             REAL NOT NULL,
  mass_loss_mg      REAL NOT NULL,
  mass_loss_pct     REAL NOT NULL,
  status            TEXT NOT NULL,
  peak_id           TEXT NOT NULL DEFAULT '',
  overlap_id        TEXT NOT NULL DEFAULT '',
  reject_reason     TEXT NOT NULL DEFAULT '',
  source_refs       TEXT NOT NULL,
  algo_version      TEXT NOT NULL,
  param_fingerprint TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS residues (
  id                TEXT PRIMARY KEY,
  derived_id        TEXT NOT NULL,
  project_id        TEXT NOT NULL,
  label             TEXT NOT NULL,
  at_s              REAL NOT NULL,
  at_c              REAL NOT NULL,
  mass_mg           REAL NOT NULL,
  percent           REAL NOT NULL,
  status            TEXT NOT NULL,
  unknown_reason    TEXT NOT NULL DEFAULT '',
  algo_version      TEXT NOT NULL,
  param_fingerprint TEXT NOT NULL,
  direct_sources    TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS residue_chain (
  residue_id TEXT NOT NULL,
  step_id    TEXT NOT NULL,
  ord        INTEGER NOT NULL,
  op         TEXT NOT NULL,
  expression TEXT NOT NULL,
  value      TEXT NOT NULL,
  unit       TEXT NOT NULL,
  source_ref TEXT NOT NULL,
  PRIMARY KEY (residue_id, ord)
);

CREATE TABLE IF NOT EXISTS decisions (
  id          TEXT PRIMARY KEY,
  project_id  TEXT NOT NULL,
  seq         INTEGER NOT NULL,
  kind        TEXT NOT NULL,
  target_type TEXT NOT NULL,
  target_id   TEXT NOT NULL,
  verdict     TEXT NOT NULL,
  payload     TEXT NOT NULL DEFAULT '',
  parent_id   TEXT NOT NULL DEFAULT '',
  conflict_of TEXT NOT NULL DEFAULT '',
  resolves_id TEXT NOT NULL DEFAULT '',
  note        TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS decision_targets (
  -- Marks which derived version a decision was originally recorded against,
  -- so historical views restore the matching dependency version.
  decision_id TEXT PRIMARY KEY,
  derived_id  TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS ix_sources_project ON sources(project_id, kind, version);
CREATE INDEX IF NOT EXISTS ix_derived_project ON derived(project_id, version);
CREATE INDEX IF NOT EXISTS ix_decisions_project ON decisions(project_id, seq);
`
