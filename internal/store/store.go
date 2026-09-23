// Package store persists sources, derived values and the append-only event
// log in SQLite. All state transitions go through events so that any past
// review state can be reconstructed by folding events up to a sequence.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

// Clock supplies virtual time; tests inject a fixed clock.
type Clock interface {
	Now() time.Time
}

type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now().UTC() }

// FixedClock is a deterministic clock for tests and replays.
type FixedClock struct{ T time.Time }

func (c FixedClock) Now() time.Time { return c.T }

func (c *FixedClock) Advance(d time.Duration) { c.T = c.T.Add(d) }

type Store struct {
	db    *sql.DB
	clock Clock
}

func Open(path string, clock Clock) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, clock: clock}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func OpenMemory(clock Clock) (*Store, error) {
	db, err := sql.Open("sqlite", "file::memory:?_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, clock: clock}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS sources (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_id TEXT,
  payload TEXT NOT NULL,
  diagnostics TEXT NOT NULL DEFAULT '[]',
  created_seq INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS derived (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  algo_version TEXT NOT NULL,
  param_fingerprint TEXT NOT NULL,
  source_ids TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_seq INTEGER NOT NULL,
  superseded_seq INTEGER
);
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL,
  at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS counters (name TEXT PRIMARY KEY, value INTEGER NOT NULL);
`)
	return err
}

// NextID allocates a deterministic, monotonically increasing id with the
// given prefix (e.g. "src", "evt", "drv"). Ids survive export/import because
// counters are part of the archive.
func (s *Store) NextID(prefix string) (string, error) {
	var v int64
	err := s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`INSERT INTO counters(name,value) VALUES(?,0) ON CONFLICT(name) DO NOTHING`, prefix); err != nil {
			return err
		}
		if _, err := tx.Exec(`UPDATE counters SET value=value+1 WHERE name=?`, prefix); err != nil {
			return err
		}
		return tx.QueryRow(`SELECT value FROM counters WHERE name=?`, prefix).Scan(&v)
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s-%06d", prefix, v), nil
}

func (s *Store) withTx(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}
