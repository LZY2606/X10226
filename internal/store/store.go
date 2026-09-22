// Package store implements the SQLite-backed, append-only persistence layer.
// Raw submissions and parameter versions are immutable; every mutation is an
// event in a per-project hash chain, enabling replay to any past event.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"tga/internal/domain"
)

// Store wraps the database handle.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path and runs migrations.
func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

// Close releases the handle.
func (s *Store) Close() error { return s.db.Close() }

const schemaSQL = `
CREATE TABLE IF NOT EXISTS projects (
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	slug TEXT NOT NULL UNIQUE,
	created_ts TEXT NOT NULL,
	archived INTEGER NOT NULL DEFAULT 0,
	head_seq INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS raw_sources (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL REFERENCES projects(id),
	kind TEXT NOT NULL,
	version INTEGER NOT NULL,
	parent_id TEXT,
	payload TEXT NOT NULL,
	digest TEXT NOT NULL,
	import_ref TEXT,
	seq INTEGER NOT NULL,
	UNIQUE(project_id, kind, digest)
);
CREATE TABLE IF NOT EXISTS param_versions (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL REFERENCES projects(id),
	version INTEGER NOT NULL,
	parent_id TEXT,
	doc TEXT NOT NULL,
	digest TEXT NOT NULL,
	seq INTEGER NOT NULL,
	UNIQUE(project_id, digest)
);
CREATE TABLE IF NOT EXISTS derived_nodes (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL REFERENCES projects(id),
	algorithm_version TEXT NOT NULL,
	param_version TEXT NOT NULL,
	direct_sources TEXT NOT NULL,
	doc TEXT NOT NULL,
	digest TEXT NOT NULL,
	stale INTEGER NOT NULL DEFAULT 0,
	stale_reason TEXT,
	superseded_by TEXT,
	seq INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS decisions (
	id TEXT PRIMARY KEY,
	project_id TEXT NOT NULL REFERENCES projects(id),
	seq INTEGER NOT NULL,
	target_type TEXT NOT NULL,
	target_id TEXT NOT NULL,
	verdict TEXT NOT NULL,
	doc TEXT NOT NULL,
	undone_by TEXT,
	parent_event TEXT,
	UNIQUE(project_id, seq)
);
CREATE TABLE IF NOT EXISTS history (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	project_id TEXT NOT NULL,
	type TEXT NOT NULL,
	payload TEXT NOT NULL,
	ref_ids TEXT NOT NULL,
	virtual_ts TEXT NOT NULL,
	prev_hash TEXT NOT NULL,
	hash TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS import_batches (
	project_id TEXT NOT NULL,
	import_ref TEXT NOT NULL,
	seq INTEGER NOT NULL,
	doc TEXT NOT NULL,
	PRIMARY KEY(project_id, import_ref)
);
CREATE INDEX IF NOT EXISTS ix_raw_project ON raw_sources(project_id, kind, version);
CREATE INDEX IF NOT EXISTS ix_derived_project ON derived_nodes(project_id, seq);
CREATE INDEX IF NOT EXISTS ix_history_project ON history(project_id, seq);
`

func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

// appendEvent writes the next hash-chained history event and advances the
// project head inside the given transaction.
func appendEvent(tx *sql.Tx, projectID, typ string, payload any, refIDs []string, virtualTS string) (domain.HistoryEvent, error) {
	var prev struct {
		seq  int64
		hash string
	}
	row := tx.QueryRow(`SELECT head_seq, COALESCE((SELECT hash FROM history WHERE project_id=? ORDER BY seq DESC LIMIT 1),'') FROM projects WHERE id=?`, projectID, projectID)
	if err := row.Scan(&prev.seq, &prev.hash); err != nil {
		return domain.HistoryEvent{}, err
	}
	pb, err := json.Marshal(payload)
	if err != nil {
		return domain.HistoryEvent{}, err
	}
	rb, _ := json.Marshal(refIDs)
	next := prev.seq + 1
	h := domain.HistoryHash(next, projectID, typ, pb, prev.hash)
	if _, err := tx.Exec(`INSERT INTO history(seq,project_id,type,payload,ref_ids,virtual_ts,prev_hash,hash) VALUES(?,?,?,?,?,?,?,?)`,
		next, projectID, typ, string(pb), string(rb), virtualTS, prev.hash, h); err != nil {
		return domain.HistoryEvent{}, err
	}
	if _, err := tx.Exec(`UPDATE projects SET head_seq=? WHERE id=?`, next, projectID); err != nil {
		return domain.HistoryEvent{}, err
	}
	return domain.HistoryEvent{Seq: next, ProjectID: projectID, Type: typ, Payload: pb, RefIDs: refIDs, VirtualTS: virtualTS, PrevHash: prev.hash, Hash: h}, nil
}
