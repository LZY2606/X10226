// Package store implements the SQLite-backed event log and read projections.
// Events are append-only and hash-chained; every projection is rebuilt by
// replaying events, so deleting the database and re-importing an exported
// bundle restores identities, ordering and provenance exactly.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

// Clock is injectable so tests run on virtual time.
type Clock func() time.Time

// Store is a single SQLite database handle.
type Store struct {
	db  *sql.DB
	now Clock
	mu  sync.Mutex
}

func realNow() time.Time { return time.Now().UTC() }

// Open opens (creating the schema in) path.
func Open(ctx context.Context, path string) (*Store, error) {
	return OpenAt(ctx, path, realNow)
}

// OpenAt opens with a custom virtual clock.
func OpenAt(ctx context.Context, path string, now Clock) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, now: now}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS projects(
			id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at TEXT NOT NULL,
			seed INTEGER NOT NULL DEFAULT 0, note TEXT DEFAULT '')`,
		`CREATE TABLE IF NOT EXISTS events(
			seq INTEGER PRIMARY KEY AUTOINCREMENT,
			id TEXT NOT NULL UNIQUE, project_id TEXT NOT NULL, at TEXT NOT NULL,
			type TEXT NOT NULL, payload TEXT NOT NULL,
			prev_hash TEXT NOT NULL, hash TEXT NOT NULL)`,
		`CREATE INDEX IF NOT EXISTS events_project ON events(project_id, seq)`,
		`CREATE TABLE IF NOT EXISTS attempts(
			project_id TEXT NOT NULL, id TEXT NOT NULL, at TEXT NOT NULL,
			ok INTEGER NOT NULL, partial INTEGER NOT NULL,
			bundle_checksum TEXT, error TEXT, payload TEXT NOT NULL,
			PRIMARY KEY(project_id, id))`,
		`CREATE TABLE IF NOT EXISTS batches(
			project_id TEXT NOT NULL, id TEXT NOT NULL, kind TEXT NOT NULL,
			version INTEGER NOT NULL, parent_id TEXT DEFAULT '',
			committed_at TEXT NOT NULL, event_seq INTEGER NOT NULL,
			payload TEXT NOT NULL,
			PRIMARY KEY(project_id, id))`,
		`CREATE TABLE IF NOT EXISTS params_versions(
			project_id TEXT NOT NULL, fingerprint TEXT NOT NULL,
			version_no INTEGER NOT NULL, created_event_seq INTEGER NOT NULL,
			payload TEXT NOT NULL,
			PRIMARY KEY(project_id, fingerprint))`,
		`CREATE TABLE IF NOT EXISTS nodes(
			project_id TEXT NOT NULL, id TEXT NOT NULL, event_seq INTEGER NOT NULL,
			set_id TEXT NOT NULL, kind TEXT NOT NULL,
			algo TEXT, param_fp TEXT, superseded_event_seq INTEGER,
			payload TEXT NOT NULL,
			PRIMARY KEY(project_id, id))`,
		`CREATE INDEX IF NOT EXISTS nodes_set ON nodes(project_id, set_id)`,
		`CREATE TABLE IF NOT EXISTS decisions(
			project_id TEXT NOT NULL, id TEXT NOT NULL, event_seq INTEGER NOT NULL,
			at TEXT NOT NULL, by_name TEXT NOT NULL,
			target_kind TEXT NOT NULL, target_id TEXT NOT NULL,
			verdict TEXT NOT NULL, rationale TEXT, adjustment TEXT,
			supersedes TEXT DEFAULT '', undone_event_seq INTEGER,
			conflict_id TEXT DEFAULT '',
			PRIMARY KEY(project_id, id))`,
		`CREATE TABLE IF NOT EXISTS conflicts(
			project_id TEXT NOT NULL, id TEXT NOT NULL, at TEXT NOT NULL,
			decision_a TEXT NOT NULL, decision_b TEXT NOT NULL,
			target_id TEXT NOT NULL, resolved_event_seq INTEGER,
			winner TEXT DEFAULT '', reason TEXT DEFAULT '',
			PRIMARY KEY(project_id, id))`,
		`CREATE TABLE IF NOT EXISTS undos(
			project_id TEXT NOT NULL, id TEXT NOT NULL, at TEXT NOT NULL,
			event_seq INTEGER NOT NULL, undone_event_seq INTEGER NOT NULL,
			kind TEXT NOT NULL, reason TEXT,
			PRIMARY KEY(project_id, id))`,
		`CREATE TABLE IF NOT EXISTS rule_events(
			project_id TEXT NOT NULL, node_id TEXT NOT NULL, seq_in_set INTEGER,
			code TEXT, subject TEXT, severity TEXT, message TEXT, refs TEXT,
			set_id TEXT,
			PRIMARY KEY(project_id, node_id, seq_in_set))`,
	}
	for _, st := range stmts {
		if _, err := s.db.ExecContext(ctx, st); err != nil {
			return fmt.Errorf("migrate: %w: %s", err, st)
		}
	}
	return nil
}

var _ = json.RawMessage(nil)
