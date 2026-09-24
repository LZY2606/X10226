package tga

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	_ "modernc.org/sqlite"
)

// Event is one entry in the append-only history. Every reviewer action
// (import, revision, derivation, decision, undo, conflict resolution) is an
// event; returning to an old event restores the dependency versions and
// visible conclusions of that moment.
type Event struct {
	Seq     int64           `json:"seq"`
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Actor   string          `json:"actor"`
	At      string          `json:"at"`
	Payload json.RawMessage `json:"payload"`
}

// InputVersion is one immutable version of a stratified raw input layer.
type InputVersion struct {
	ID          string          `json:"id"`
	Layer       string          `json:"layer"`
	Version     int             `json:"version"`
	Parent      string          `json:"parent"` // previous version id, "" for v1
	Payload     json.RawMessage `json:"payload"`
	Diagnostics []Diagnostic    `json:"diagnostics,omitempty"`
	Seq         int64           `json:"seq"`
}

// Derived is a computed value with algorithm version, parameter fingerprint
// and direct sources. Stale values stay viewable but never pose as current.
type Derived struct {
	ID         string          `json:"id"`
	Kind       string          `json:"kind"`
	Algo       string          `json:"algo"`
	ParamFP    string          `json:"param_fp"`
	Sources    []string        `json:"sources"`
	Status     string          `json:"status"`
	Payload    json.RawMessage `json:"payload"`
	CreatedSeq int64           `json:"created_seq"`
	StaleSeq   int64           `json:"stale_seq"` // 0 while current
}

// Store is the SQLite-backed event store. The clock is injectable so tests
// run on virtual time.
type Store struct {
	db    *sql.DB
	clock func() time.Time
}

func OpenStore(path string, clock func() time.Time) (*Store, error) {
	if clock == nil {
		clock = time.Now
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
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
CREATE TABLE IF NOT EXISTS events(
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT UNIQUE NOT NULL,
  kind TEXT NOT NULL,
  actor TEXT NOT NULL,
  at TEXT NOT NULL,
  payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS inputs(
  id TEXT PRIMARY KEY,
  layer TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent TEXT NOT NULL,
  payload TEXT NOT NULL,
  diagnostics TEXT NOT NULL DEFAULT '[]',
  seq INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS derived(
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  algo TEXT NOT NULL,
  param_fp TEXT NOT NULL,
  sources TEXT NOT NULL,
  status TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_seq INTEGER NOT NULL,
  stale_seq INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
`)
	return err
}

// nextSeq returns the seq the next appended event will get.
func (s *Store) nextSeq() (int64, error) {
	var max sql.NullInt64
	if err := s.db.QueryRow(`SELECT MAX(seq) FROM events`).Scan(&max); err != nil {
		return 0, err
	}
	return max.Int64 + 1, nil
}

// AppendEvent records an event and returns it with its assigned seq.
func (s *Store) AppendEvent(kind, actor string, payload any) (*Event, error) {
	raw := canonicalJSON(payload)
	id := contentID("event", kind, actor, s.clock().UTC().Format(time.RFC3339Nano), string(raw), s.mustSeq())
	res, err := s.db.Exec(`INSERT INTO events(id,kind,actor,at,payload) VALUES(?,?,?,?,?)`,
		id, kind, actor, s.clock().UTC().Format(time.RFC3339), string(raw))
	if err != nil {
		return nil, fmt.Errorf("append event: %w", err)
	}
	seq, _ := res.LastInsertId()
	return &Event{Seq: seq, ID: id, Kind: kind, Actor: actor, At: s.clock().UTC().Format(time.RFC3339), Payload: raw}, nil
}

func (s *Store) mustSeq() int64 {
	n, err := s.nextSeq()
	if err != nil {
		return 0
	}
	return n
}

// appendEventRaw inserts an event with fixed identity (archive import).
func (s *Store) appendEventRaw(e Event) error {
	_, err := s.db.Exec(`INSERT INTO events(seq,id,kind,actor,at,payload) VALUES(?,?,?,?,?,?)`,
		e.Seq, e.ID, e.Kind, e.Actor, e.At, string(e.Payload))
	return err
}

// Events returns events in seq order; asof>0 limits to seq<=asof.
func (s *Store) Events(asof int64) ([]Event, error) {
	q := `SELECT seq,id,kind,actor,at,payload FROM events`
	args := []any{}
	if asof > 0 {
		q += ` WHERE seq<=?`
		args = append(args, asof)
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
		if err := rows.Scan(&e.Seq, &e.ID, &e.Kind, &e.Actor, &e.At, &p); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(p)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PutInputVersion stores an immutable input version. Existing ids are never
// overwritten: re-submitting identical content is a no-op.
func (s *Store) PutInputVersion(v InputVersion) error {
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM inputs WHERE id=?`, v.ID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		return nil // immutable: never overwrite
	}
	diag := "[]"
	if v.Diagnostics != nil {
		diag = string(canonicalJSON(v.Diagnostics))
	}
	_, err := s.db.Exec(`INSERT INTO inputs(id,layer,version,parent,payload,diagnostics,seq) VALUES(?,?,?,?,?,?,?)`,
		v.ID, v.Layer, v.Version, v.Parent, string(v.Payload), diag, v.Seq)
	return err
}

func (s *Store) InputByID(id string) (*InputVersion, error) {
	row := s.db.QueryRow(`SELECT id,layer,version,parent,payload,diagnostics,seq FROM inputs WHERE id=?`, id)
	return scanInput(row)
}

func scanInput(row *sql.Row) (*InputVersion, error) {
	var v InputVersion
	var p, d string
	if err := row.Scan(&v.ID, &v.Layer, &v.Version, &v.Parent, &p, &d, &v.Seq); err != nil {
		return nil, err
	}
	v.Payload = json.RawMessage(p)
	json.Unmarshal([]byte(d), &v.Diagnostics)
	return &v, nil
}

// LatestInput returns the newest version of a layer ("" if none).
func (s *Store) LatestInput(layer string, asof int64) (*InputVersion, error) {
	q := `SELECT id,layer,version,parent,payload,diagnostics,seq FROM inputs WHERE layer=?`
	args := []any{layer}
	if asof > 0 {
		q += ` AND seq<=?`
		args = append(args, asof)
	}
	q += ` ORDER BY version DESC LIMIT 1`
	row := s.db.QueryRow(q, args...)
	v, err := scanInput(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return v, err
}

// InputVersions lists all versions of a layer, oldest first.
func (s *Store) InputVersions(layer string) ([]InputVersion, error) {
	rows, err := s.db.Query(`SELECT id,layer,version,parent,payload,diagnostics,seq FROM inputs WHERE layer=? ORDER BY version`, layer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InputVersion
	for rows.Next() {
		var v InputVersion
		var p, d string
		if err := rows.Scan(&v.ID, &v.Layer, &v.Version, &v.Parent, &p, &d, &v.Seq); err != nil {
			return nil, err
		}
		v.Payload = json.RawMessage(p)
		json.Unmarshal([]byte(d), &v.Diagnostics)
		out = append(out, v)
	}
	return out, rows.Err()
}

// AllInputs lists every input version ordered by seq.
func (s *Store) AllInputs() ([]InputVersion, error) {
	rows, err := s.db.Query(`SELECT id,layer,version,parent,payload,diagnostics,seq FROM inputs ORDER BY seq, layer, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []InputVersion
	for rows.Next() {
		var v InputVersion
		var p, d string
		if err := rows.Scan(&v.ID, &v.Layer, &v.Version, &v.Parent, &p, &d, &v.Seq); err != nil {
			return nil, err
		}
		v.Payload = json.RawMessage(p)
		json.Unmarshal([]byte(d), &v.Diagnostics)
		out = append(out, v)
	}
	return out, rows.Err()
}

// PutDerived inserts a derived value (no-op if the id already exists).
func (s *Store) PutDerived(d Derived) error {
	var exists int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM derived WHERE id=?`, d.ID).Scan(&exists); err != nil {
		return err
	}
	if exists > 0 {
		// revive if it was stale and is current again
		_, err := s.db.Exec(`UPDATE derived SET status=?, stale_seq=0 WHERE id=? AND status=?`, StatusCurrent, d.ID, StatusStale)
		return err
	}
	_, err := s.db.Exec(`INSERT INTO derived(id,kind,algo,param_fp,sources,status,payload,created_seq,stale_seq) VALUES(?,?,?,?,?,?,?,?,0)`,
		d.ID, d.Kind, d.Algo, d.ParamFP, string(canonicalJSON(d.Sources)), d.Status, string(d.Payload), d.CreatedSeq)
	return err
}

// MarkStaleExcept marks every current derived not in keepIDs as stale.
func (s *Store) MarkStaleExcept(keepIDs map[string]bool, staleSeq int64) error {
	rows, err := s.db.Query(`SELECT id FROM derived WHERE status=?`, StatusCurrent)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if !keepIDs[id] {
			if _, err := s.db.Exec(`UPDATE derived SET status=?, stale_seq=? WHERE id=?`, StatusStale, staleSeq, id); err != nil {
				return err
			}
		}
	}
	return nil
}

// DerivedByID fetches one derived value.
func (s *Store) DerivedByID(id string) (*Derived, error) {
	row := s.db.QueryRow(`SELECT id,kind,algo,param_fp,sources,status,payload,created_seq,stale_seq FROM derived WHERE id=?`, id)
	return scanDerived(row)
}

func scanDerived(row *sql.Row) (*Derived, error) {
	var d Derived
	var src, p string
	if err := row.Scan(&d.ID, &d.Kind, &d.Algo, &d.ParamFP, &src, &d.Status, &p, &d.CreatedSeq, &d.StaleSeq); err != nil {
		return nil, err
	}
	json.Unmarshal([]byte(src), &d.Sources)
	d.Payload = json.RawMessage(p)
	return &d, nil
}

// DerivedOf returns derived values of a kind. When asof>0 it reconstructs
// the visible state at that event: created_seq<=asof and not stale before
// asof. Otherwise it returns currently-visible (current+stale) values.
func (s *Store) DerivedOf(kind string, asof int64) ([]Derived, error) {
	var q string
	var args []any
	if asof > 0 {
		q = `SELECT id,kind,algo,param_fp,sources,status,payload,created_seq,stale_seq FROM derived
		     WHERE kind=? AND created_seq<=? AND (stale_seq=0 OR stale_seq>?)`
		args = []any{kind, asof, asof}
	} else {
		q = `SELECT id,kind,algo,param_fp,sources,status,payload,created_seq,stale_seq FROM derived WHERE kind=?`
		args = []any{kind}
	}
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Derived
	for rows.Next() {
		var d Derived
		var src, p string
		if err := rows.Scan(&d.ID, &d.Kind, &d.Algo, &d.ParamFP, &src, &d.Status, &p, &d.CreatedSeq, &d.StaleSeq); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(src), &d.Sources)
		d.Payload = json.RawMessage(p)
		if asof > 0 && d.StaleSeq > asof {
			d.Status = StatusCurrent
			d.StaleSeq = 0
		}
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedSeq < out[j].CreatedSeq })
	return out, rows.Err()
}

// AllDerived lists every derived row (for export).
func (s *Store) AllDerived() ([]Derived, error) {
	rows, err := s.db.Query(`SELECT id,kind,algo,param_fp,sources,status,payload,created_seq,stale_seq FROM derived ORDER BY created_seq, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Derived
	for rows.Next() {
		var d Derived
		var src, p string
		if err := rows.Scan(&d.ID, &d.Kind, &d.Algo, &d.ParamFP, &src, &d.Status, &p, &d.CreatedSeq, &d.StaleSeq); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(src), &d.Sources)
		d.Payload = json.RawMessage(p)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) insertDerivedRaw(d Derived) error {
	_, err := s.db.Exec(`INSERT INTO derived(id,kind,algo,param_fp,sources,status,payload,created_seq,stale_seq) VALUES(?,?,?,?,?,?,?,?,?)`,
		d.ID, d.Kind, d.Algo, d.ParamFP, string(canonicalJSON(d.Sources)), d.Status, string(d.Payload), d.CreatedSeq, d.StaleSeq)
	return err
}

// Params handling: current params live in meta; every change is an event.
func (s *Store) CurrentParams() Params {
	var v string
	if err := s.db.QueryRow(`SELECT value FROM meta WHERE key='params'`).Scan(&v); err != nil {
		return DefaultParams()
	}
	var p Params
	if err := json.Unmarshal([]byte(v), &p); err != nil {
		return DefaultParams()
	}
	return p
}

func (s *Store) setParams(p Params) error {
	_, err := s.db.Exec(`INSERT INTO meta(key,value) VALUES('params',?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		string(canonicalJSON(p)))
	return err
}

// SetParams records a parameter change event and stores the new params.
func (s *Store) SetParams(actor string, p Params) (*Event, error) {
	old := s.CurrentParams()
	if err := s.setParams(p); err != nil {
		return nil, err
	}
	return s.AppendEvent(EvParamChange, actor, map[string]any{
		"old": old, "new": p, "old_fp": old.Fingerprint(), "new_fp": p.Fingerprint(),
	})
}
