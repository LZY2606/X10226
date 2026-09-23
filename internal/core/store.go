package core

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

// Store is the SQLite persistence layer. All history is append-only; rows are
// content-addressed so an export/import round trip preserves identities,
// ordering and source relations.
type Store struct {
	db *sql.DB
}

// Open creates or opens the project database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.init(); err != nil {
		return nil, err
	}
	return s, nil
}

// Close releases the database handle.
func (s *Store) Close() error { return s.db.Close() }

// DB exposes the handle for the export/import machinery.
func (s *Store) DB() *sql.DB { return s.db }

const schema = `
CREATE TABLE IF NOT EXISTS inputs (
	id         TEXT PRIMARY KEY,
	layer      TEXT NOT NULL,
	version    INTEGER NOT NULL,
	parent_id  TEXT NOT NULL DEFAULT '',
	content    TEXT NOT NULL,
	units      TEXT NOT NULL DEFAULT '',
	created_at INTEGER NOT NULL,
	event_seq  INTEGER NOT NULL,
	UNIQUE(layer, version)
);
CREATE TABLE IF NOT EXISTS events (
	seq     INTEGER PRIMARY KEY,
	id      TEXT NOT NULL,
	kind    TEXT NOT NULL,
	ts      INTEGER NOT NULL,
	payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS derived (
	id           TEXT PRIMARY KEY,
	kind         TEXT NOT NULL,
	algo_version TEXT NOT NULL,
	params_fp    TEXT NOT NULL,
	sources      TEXT NOT NULL,
	content      TEXT NOT NULL,
	status       TEXT NOT NULL DEFAULT 'current',
	event_seq    INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS decisions (
	id         TEXT PRIMARY KEY,
	target_id  TEXT NOT NULL,
	action     TEXT NOT NULL,
	params     TEXT NOT NULL DEFAULT '',
	rationale  TEXT NOT NULL DEFAULT '',
	ts         INTEGER NOT NULL,
	undone     INTEGER NOT NULL DEFAULT 0,
	overruled  INTEGER NOT NULL DEFAULT 0,
	event_seq  INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_inputs_layer ON inputs(layer, version);
CREATE INDEX IF NOT EXISTS idx_derived_kind ON derived(kind, event_seq);
CREATE INDEX IF NOT EXISTS idx_dec_target ON decisions(target_id);
`

func (s *Store) init() error {
	_, err := s.db.Exec(schema)
	return err
}

// AppendEvent inserts an event with an explicit sequence (used by import) or
// the next auto sequence when seq is 0 (returns the assigned seq).
func (s *Store) AppendEvent(seq int64, id, kind string, ts int64, payload string) (int64, error) {
	if seq == 0 {
		res, err := s.db.Exec(
			`INSERT INTO events(id, kind, ts, payload) VALUES(?,?,?,?)`,
			id, kind, ts, payload)
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	}
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO events(seq, id, kind, ts, payload) VALUES(?,?,?,?,?)`,
		seq, id, kind, ts, payload)
	return seq, err
}

// MaxSeq returns the highest event sequence, or 0 when the history is empty.
func (s *Store) MaxSeq() (int64, error) {
	var seq sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(seq) FROM events`).Scan(&seq); err != nil {
		return 0, err
	}
	return seq.Int64, nil
}

// EventID derives the stable event identifier from its sequence.
func EventID(seq int64) string { return fmt.Sprintf("ev_%06d", seq) }

// Events returns all events up to and including beforeSeq (0 = all).
func (s *Store) Events(beforeSeq int64) ([]*Event, error) {
	q := `SELECT seq, id, kind, ts, payload FROM events`
	args := []any{}
	if beforeSeq > 0 {
		q += ` WHERE seq <= ?`
		args = append(args, beforeSeq)
	}
	q += ` ORDER BY seq`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Event
	for rows.Next() {
		e := &Event{}
		if err := rows.Scan(&e.Seq, &e.ID, &e.Kind, &e.Ts, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// InsertInput stores an immutable layer version.
func (s *Store) InsertInput(in *Input) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO inputs(id, layer, version, parent_id, content, units, created_at, event_seq)
		 VALUES(?,?,?,?,?,?,?,?)`,
		in.ID, in.Layer, in.Version, in.ParentID, in.Content, in.Units, in.CreatedAt, in.EventSeq)
	return err
}

// LatestInput returns the newest version of layer committed at or before
// beforeSeq (0 = latest overall).
func (s *Store) LatestInput(layer string, beforeSeq int64) (*Input, error) {
	q := `SELECT id, layer, version, parent_id, content, units, created_at, event_seq
	      FROM inputs WHERE layer = ?`
	args := []any{layer}
	if beforeSeq > 0 {
		q += ` AND event_seq <= ?`
		args = append(args, beforeSeq)
	}
	q += ` ORDER BY version DESC LIMIT 1`
	in := &Input{}
	err := s.db.QueryRow(q, args...).Scan(
		&in.ID, &in.Layer, &in.Version, &in.ParentID, &in.Content,
		&in.Units, &in.CreatedAt, &in.EventSeq)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return in, err
}

// InputByID fetches a layer version by stable identity.
func (s *Store) InputByID(id string) (*Input, error) {
	in := &Input{}
	err := s.db.QueryRow(
		`SELECT id, layer, version, parent_id, content, units, created_at, event_seq
		 FROM inputs WHERE id = ?`, id).Scan(
		&in.ID, &in.Layer, &in.Version, &in.ParentID, &in.Content,
		&in.Units, &in.CreatedAt, &in.EventSeq)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return in, err
}

// InsertDerived stores a content-addressed derived record (ignored if present).
func (s *Store) InsertDerived(d *Derived) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO derived(id, kind, algo_version, params_fp, sources, content, status, event_seq)
		 VALUES(?,?,?,?,?,?,?,?)`,
		d.ID, d.Kind, d.AlgoVersion, d.ParamsFP, d.Sources, d.Content, d.Status, d.EventSeq)
	return err
}

// DerivedVisible returns every derived record committed at or before beforeSeq.
func (s *Store) DerivedVisible(beforeSeq int64) ([]*Derived, error) {
	q := `SELECT id, kind, algo_version, params_fp, sources, content, status, event_seq
	      FROM derived WHERE event_seq <= ? ORDER BY id`
	rows, err := s.db.Query(q, beforeSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Derived
	for rows.Next() {
		d := &Derived{}
		if err := rows.Scan(&d.ID, &d.Kind, &d.AlgoVersion, &d.ParamsFP,
			&d.Sources, &d.Content, &d.Status, &d.EventSeq); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DerivedByID fetches a derived record by stable identity.
func (s *Store) DerivedByID(id string) (*Derived, error) {
	d := &Derived{}
	err := s.db.QueryRow(
		`SELECT id, kind, algo_version, params_fp, sources, content, status, event_seq
		 FROM derived WHERE id = ?`, id).Scan(
		&d.ID, &d.Kind, &d.AlgoVersion, &d.ParamsFP,
		&d.Sources, &d.Content, &d.Status, &d.EventSeq)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return d, err
}

// InsertDecision stores a human decision.
func (s *Store) InsertDecision(d *Decision) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO decisions(id, target_id, action, params, rationale, ts, undone, overruled, event_seq)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		d.ID, d.TargetID, d.Action, d.Params, d.Rationale, d.Ts,
		boolInt(d.Undone), boolInt(d.Overruled), d.EventSeq)
	return err
}

// Decisions returns decisions committed at or before beforeSeq, in order.
func (s *Store) Decisions(beforeSeq int64) ([]*Decision, error) {
	rows, err := s.db.Query(
		`SELECT id, target_id, action, params, rationale, ts, undone, overruled, event_seq
		 FROM decisions WHERE event_seq <= ? ORDER BY event_seq, id`, beforeSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Decision
	for rows.Next() {
		d := &Decision{}
		var u, o int
		if err := rows.Scan(&d.ID, &d.TargetID, &d.Action, &d.Params, &d.Rationale,
			&d.Ts, &u, &o, &d.EventSeq); err != nil {
			return nil, err
		}
		d.Undone = u == 1
		d.Overruled = o == 1
		out = append(out, d)
	}
	return out, rows.Err()
}

// DecisionByID fetches a decision by identity.
func (s *Store) DecisionByID(id string) (*Decision, error) {
	d := &Decision{}
	var u, o int
	err := s.db.QueryRow(
		`SELECT id, target_id, action, params, rationale, ts, undone, overruled, event_seq
		 FROM decisions WHERE id = ?`, id).Scan(
		&d.ID, &d.TargetID, &d.Action, &d.Params, &d.Rationale,
		&d.Ts, &u, &o, &d.EventSeq)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	d.Undone = u == 1
	d.Overruled = o == 1
	return d, nil
}

// SetDecisionFlags updates undo / overrule state for conflict resolution.
func (s *Store) SetDecisionFlags(id string, undone, overruled bool) error {
	_, err := s.db.Exec(
		`UPDATE decisions SET undone = ?, overruled = ? WHERE id = ?`,
		boolInt(undone), boolInt(overruled), id)
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
