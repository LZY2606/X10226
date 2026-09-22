// Package store implements the append-only SQLite event log.
// Every fact in the system is an event; projections are derived.
// Export/import preserves event order and entity identities.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"

	_ "modernc.org/sqlite"

	"tgaview/internal/clock"
)

// Event is one immutable fact in the log.
type Event struct {
	Seq     int64           `json:"seq"`
	At      int64           `json:"at"`
	Kind    string          `json:"kind"`
	Entity  string          `json:"entity"`
	Payload json.RawMessage `json:"payload"`
}

// Store wraps the SQLite-backed event log.
type Store struct {
	db  *sql.DB
	clk *clock.Clock
}

// Open opens (or creates) the event log at path. Use ":memory:" for tests.
func Open(path string, clk *clock.Clock) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(`PRAGMA journal_mode=WAL; PRAGMA foreign_keys=ON;`); err != nil {
		return nil, err
	}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS events(
		seq INTEGER PRIMARY KEY,
		at INTEGER NOT NULL,
		kind TEXT NOT NULL,
		entity TEXT NOT NULL,
		payload TEXT NOT NULL
	);`); err != nil {
		return nil, err
	}
	s := &Store{db: db, clk: clk}
	var maxAt sql.NullInt64
	if err := db.QueryRow(`SELECT MAX(at) FROM events`).Scan(&maxAt); err != nil {
		return nil, err
	}
	if maxAt.Valid {
		clk.Sync(maxAt.Int64)
	}
	return s, nil
}

// Close closes the underlying database.
func (s *Store) Close() error { return s.db.Close() }

// Append adds an event and returns its sequence number.
func (s *Store) Append(kind, entity string, payload any) (int64, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`INSERT INTO events(at,kind,entity,payload) VALUES(?,?,?,?)`,
		s.clk.Now(), kind, entity, string(raw))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Events returns events with seq <= upTo (upTo <= 0 means all), ordered by seq.
func (s *Store) Events(upTo int64) ([]Event, error) {
	q := `SELECT seq,at,kind,entity,payload FROM events`
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
	var out []Event
	for rows.Next() {
		var e Event
		var p string
		if err := rows.Scan(&e.Seq, &e.At, &e.Kind, &e.Entity, &p); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(p)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Head returns the latest sequence number (0 when empty).
func (s *Store) Head() (int64, error) {
	var v sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(seq) FROM events`).Scan(&v); err != nil {
		return 0, err
	}
	if !v.Valid {
		return 0, nil
	}
	return v.Int64, nil
}

// ExportLine is the on-disk export format (one JSON object per line).
type ExportLine struct {
	Seq     int64           `json:"seq"`
	At      int64           `json:"at"`
	Kind    string          `json:"kind"`
	Entity  string          `json:"entity"`
	Payload json.RawMessage `json:"payload"`
}

// Export writes the full log as JSON lines. Order and identities are preserved.
func (s *Store) Export(w io.Writer) error {
	evs, err := s.Events(0)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(w)
	for _, e := range evs {
		if err := enc.Encode(ExportLine(e)); err != nil {
			return err
		}
	}
	return nil
}

// Import reads JSON lines into an empty store, preserving seq, order and ids.
func (s *Store) Import(r io.Reader) (int, error) {
	head, err := s.Head()
	if err != nil {
		return 0, err
	}
	if head != 0 {
		return 0, fmt.Errorf("import requires an empty database (head=%d)", head)
	}
	dec := json.NewDecoder(r)
	n := 0
	for {
		var line ExportLine
		if err := dec.Decode(&line); err != nil {
			if err == io.EOF {
				break
			}
			return n, fmt.Errorf("import line %d: %w", n+1, err)
		}
		if _, err := s.db.Exec(`INSERT INTO events(seq,at,kind,entity,payload) VALUES(?,?,?,?,?)`,
			line.Seq, line.At, line.Kind, line.Entity, string(line.Payload)); err != nil {
			return n, fmt.Errorf("import line %d: %w", n+1, err)
		}
		s.clk.Sync(line.At)
		n++
	}
	return n, nil
}
