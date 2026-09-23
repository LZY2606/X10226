// Package store persists the append-only event log in SQLite.
// The log is the single source of truth: projections, staleness and
// time-travel views are all derived by replaying it.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"sort"

	"tga/internal/tga"

	_ "modernc.org/sqlite"
)

// Store is an append-only event store backed by SQLite.
type Store struct {
	db  *sql.DB
	now func() int64 // virtual clock, injected
	seq int64
}

// Open opens (or creates) the store at path. The clock is injected so
// tests can run on virtual time.
func Open(path string, now func() int64) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db, now: now}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) init() error {
	_, err := s.db.Exec(`CREATE TABLE IF NOT EXISTS events (
		seq INTEGER PRIMARY KEY,
		id TEXT NOT NULL UNIQUE,
		kind TEXT NOT NULL,
		at INTEGER NOT NULL,
		payload TEXT NOT NULL
	)`)
	if err != nil {
		return err
	}
	row := s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM events`)
	return row.Scan(&s.seq)
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Append adds one event. IDs are deterministic: same clock reading,
// same sequence, same payload -> same identity, which keeps replays
// and re-imports stable.
func (s *Store) Append(kind string, payload any) (tga.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return tga.Event{}, err
	}
	seq := s.seq + 1
	at := s.now()
	id := tga.Hash("event", fmt.Sprint(seq), kind, fmt.Sprint(at), string(raw))
	_, err = s.db.Exec(`INSERT INTO events(seq,id,kind,at,payload) VALUES(?,?,?,?,?)`,
		seq, string(id), kind, at, string(raw))
	if err != nil {
		return tga.Event{}, err
	}
	s.seq = seq
	return tga.Event{Seq: seq, ID: id, Kind: kind, At: at, Payload: raw}, nil
}

// Events returns events in sequence order, up to and including upTo
// (upTo <= 0 means all).
func (s *Store) Events(upTo int64) ([]tga.Event, error) {
	q := `SELECT seq,id,kind,at,payload FROM events`
	args := []any{}
	if upTo > 0 {
		q += ` WHERE seq <= ?`
		args = append(args, upTo)
	}
	q += ` ORDER BY seq`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []tga.Event
	for rows.Next() {
		var e tga.Event
		var payload string
		if err := rows.Scan(&e.Seq, &e.ID, &e.Kind, &e.At, &payload); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Len returns the current head sequence.
func (s *Store) Len() int64 { return s.seq }

// archive is the export document. Identities, order and provenance are
// all carried by the events themselves.
type archive struct {
	Format string      `json:"format"` // "tga-archive/1"
	Events []tga.Event `json:"events"`
}

// Export writes the whole log as a portable JSON document.
func (s *Store) Export(w io.Writer) error {
	evs, err := s.Events(0)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(archive{Format: "tga-archive/1", Events: evs})
}

// Reset deletes the local database contents (used before re-import).
func (s *Store) Reset() error {
	if _, err := s.db.Exec(`DELETE FROM events`); err != nil {
		return err
	}
	s.seq = 0
	return nil
}

// Import loads an exported archive into an empty store, preserving
// identities, order and provenance exactly.
func (s *Store) Import(r io.Reader) (int, error) {
	var a archive
	if err := json.NewDecoder(r).Decode(&a); err != nil {
		return 0, fmt.Errorf("bad archive: %w", err)
	}
	if a.Format != "tga-archive/1" {
		return 0, fmt.Errorf("unsupported archive format %q", a.Format)
	}
	if s.seq != 0 {
		return 0, fmt.Errorf("import requires an empty store (use reset first)")
	}
	sort.Slice(a.Events, func(i, j int) bool { return a.Events[i].Seq < a.Events[j].Seq })
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, e := range a.Events {
		if _, err := tx.Exec(`INSERT INTO events(seq,id,kind,at,payload) VALUES(?,?,?,?,?)`,
			e.Seq, string(e.ID), e.Kind, e.At, string(e.Payload)); err != nil {
			return 0, fmt.Errorf("import event seq=%d: %w", e.Seq, err)
		}
		if e.Seq > s.seq {
			s.seq = e.Seq
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(a.Events), nil
}
