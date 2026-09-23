// Package store is the SQLite persistence layer (pure-Go modernc.org/sqlite).
// All writes go through an append event; raw sources and derived artifacts are
// immutable. The DB file is fully portable for export/import round-trips.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

type Store struct {
	mu sync.Mutex
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) Lock()        { s.mu.Lock() }
func (s *Store) Unlock()      { s.mu.Unlock() }
func (s *Store) DB() *sql.DB  { return s.db }

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
