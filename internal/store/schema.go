// Package store persists the append-only event ledger and the stratified raw
// layers in SQLite. Current views are rebuilt by replaying events; nothing is
// ever overwritten, so returning to an old event restores that exact state.
package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

const schemaSQL = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  type TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  reviewer TEXT NOT NULL DEFAULT '',
  undoes TEXT NOT NULL DEFAULT '',
  conflict_of TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL,
  UNIQUE(project_id, seq)
);
CREATE TABLE IF NOT EXISTS batches (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  created_at TEXT NOT NULL,
  kind TEXT NOT NULL,
  source_ref TEXT NOT NULL,
  accepted_layers TEXT NOT NULL,
  rejected_layers TEXT NOT NULL,
  payload TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS samples (
  project_id TEXT NOT NULL, batch_id TEXT NOT NULL, id TEXT PRIMARY KEY,
  ordinal INTEGER NOT NULL, raw_t TEXT, unit_t TEXT, t REAL,
  raw_mass TEXT, unit_mass TEXT, mass REAL,
  raw_temp TEXT, unit_temp TEXT, temp REAL,
  convertible INTEGER NOT NULL, diagnostic TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS drift (
  project_id TEXT NOT NULL, batch_id TEXT NOT NULL, id TEXT PRIMARY KEY,
  ordinal INTEGER NOT NULL, kind TEXT NOT NULL,
  raw_t TEXT, unit_t TEXT, t REAL, raw_offset TEXT, unit_offset TEXT, offset_v REAL,
  convertible INTEGER NOT NULL, diagnostic TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS program (
  project_id TEXT NOT NULL, batch_id TEXT NOT NULL, id TEXT PRIMARY KEY,
  ordinal INTEGER NOT NULL, label TEXT NOT NULL DEFAULT '',
  t_start REAL, t_end REAL, temp_start REAL, temp_end REAL
);
CREATE TABLE IF NOT EXISTS gas (
  project_id TEXT NOT NULL, batch_id TEXT NOT NULL, id TEXT PRIMARY KEY,
  ordinal INTEGER NOT NULL, from_gas TEXT NOT NULL, to_gas TEXT NOT NULL,
  t REAL, flow REAL, raw_t TEXT, unit_t TEXT, convertible INTEGER NOT NULL, diagnostic TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS blank (
  project_id TEXT NOT NULL, batch_id TEXT NOT NULL, id TEXT PRIMARY KEY,
  ordinal INTEGER NOT NULL, t REAL, mass REAL,
  raw_t TEXT, unit_t TEXT, raw_mass TEXT, unit_mass TEXT,
  convertible INTEGER NOT NULL, diagnostic TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS initial_mass (
  project_id TEXT NOT NULL, batch_id TEXT NOT NULL, id TEXT PRIMARY KEY,
  parent_id TEXT NOT NULL DEFAULT '', ordinal INTEGER NOT NULL,
  raw_mass TEXT, unit_mass TEXT, mass REAL, reason TEXT NOT NULL DEFAULT '',
  convertible INTEGER NOT NULL, diagnostic TEXT NOT NULL DEFAULT ''
);
`

// Open creates/opens a database file and applies the schema.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, "PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL;"); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

type Store struct{ db *sql.DB }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Close() error { return s.db.Close() }
