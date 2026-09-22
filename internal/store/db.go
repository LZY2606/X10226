// Package store owns the SQLite-backed, immutable history store for the TGA
// review system. Raw inputs are never overwritten; revisions are expressed as
// new source versions with a parent link. Derived objects belong to runs and
// carry their direct sources.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"
)

// Clock lets tests run on virtual/fixed time.
type Clock func() time.Time

// Store wraps the database handle plus a clock.
type Store struct {
	DB    *sql.DB
	now   Clock
	idSeq atomic.Uint64
}

// Open opens (creating if needed) the database at dsn.
func Open(dsn string, clock Clock) (*Store, error) {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// Single connection keeps in-memory and file databases coherent without WAL surprises.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{DB: db, now: clock}, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.DB.Close() }

// Now returns the store clock time.
func (s *Store) Now() time.Time { return s.now() }

// SetClock replaces the clock (used by replay tests with virtual time).
func (s *Store) SetClock(c Clock) { s.now = c }

// NewID returns a process-unique suffix for stable ids; id stability across
// export/import does not rely on this value (object ids are deterministic).
func (s *Store) NewID(prefix string) string {
	n := s.idSeq.Add(1)
	return fmt.Sprintf("%s_%d_%d", prefix, s.now().UnixNano(), n)
}

// Tx runs fn inside a transaction.
func (s *Store) Tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
