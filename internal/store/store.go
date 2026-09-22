// Package store persists the review workspace in SQLite.
// Raw inputs are immutable once committed; revisions are new versions with a
// parent link. Derived values carry algorithm version, parameter fingerprint
// and direct sources. Every action appends a machine-readable event, so any
// past state can be reconstructed with as-of queries.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// Source is one immutable version of a raw input layer.
type Source struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Version int    `json:"version"`
	Parent  string `json:"parent,omitempty"`
	Raw     string `json:"raw"`
	Parsed  string `json:"parsed"` // JSON domain.SourcePayload
	Diag    string `json:"diag,omitempty"`
	Seq     int64  `json:"seq"`
}

// Derived is one computed value with full provenance.
type Derived struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Algo        string   `json:"algo"`
	Fingerprint string   `json:"fingerprint"`
	Deps        []string `json:"deps"`
	Payload     string   `json:"payload"` // JSON
	Status      string   `json:"status"`  // current | stale | error
	Seq         int64    `json:"seq"`
	StaleSeq    int64    `json:"stale_seq,omitempty"`
}

// Decision is a human adjudication (accept / reject / adjust) on a target.
type Decision struct {
	ID        string `json:"id"`
	Target    string `json:"target"`
	Action    string `json:"action"` // accept | reject | adjust
	Params    string `json:"params,omitempty"`
	Rationale string `json:"rationale,omitempty"`
	Actor     string `json:"actor,omitempty"`
	Resolves  string `json:"resolves,omitempty"` // conflict event id this resolves
	Seq       int64  `json:"seq"`
	UndoneSeq int64  `json:"undone_seq,omitempty"`
}

// Event is one entry of the append-only machine-readable history.
type Event struct {
	Seq     int64           `json:"seq"`
	ID      string          `json:"id"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
	Ts      int64           `json:"ts"`
}

type Store struct {
	db  *sql.DB
	now func() int64 // virtual clock, injected
}

const schema = `
CREATE TABLE IF NOT EXISTS sources(
  id TEXT PRIMARY KEY, kind TEXT NOT NULL, version INTEGER NOT NULL,
  parent TEXT, raw TEXT NOT NULL, parsed TEXT NOT NULL, diag TEXT,
  seq INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS derived(
  id TEXT PRIMARY KEY, kind TEXT NOT NULL, algo TEXT NOT NULL,
  fingerprint TEXT NOT NULL, deps TEXT NOT NULL, payload TEXT NOT NULL,
  status TEXT NOT NULL, seq INTEGER NOT NULL, stale_seq INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS decisions(
  id TEXT PRIMARY KEY, target TEXT NOT NULL, action TEXT NOT NULL,
  params TEXT, rationale TEXT, actor TEXT, resolves TEXT,
  seq INTEGER NOT NULL, undone_seq INTEGER NOT NULL DEFAULT 0);
CREATE TABLE IF NOT EXISTS events(
  seq INTEGER PRIMARY KEY AUTOINCREMENT, id TEXT NOT NULL,
  kind TEXT NOT NULL, payload TEXT NOT NULL, ts INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS meta(key TEXT PRIMARY KEY, value TEXT NOT NULL);
`

// Open opens (or creates) the store at path. now supplies virtual time.
func Open(path string, now func() int64) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	return &Store{db: db, now: now}, nil
}

func (s *Store) Close() error { return s.db.Close() }

// HeadSeq returns the latest event sequence (0 when empty).
func (s *Store) HeadSeq() int64 {
	var n int64
	_ = s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM events`).Scan(&n)
	return n
}

// Emit appends an event and returns its sequence number.
func (s *Store) Emit(id, kind string, payload any) (int64, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	res, err := s.db.Exec(`INSERT INTO events(id,kind,payload,ts) VALUES(?,?,?,?)`,
		id, kind, string(b), s.now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// CommitSource stores a new immutable source version. Existing rows are
// never modified; a revision is a new row linked by Parent.
func (s *Store) CommitSource(src Source) error {
	_, err := s.db.Exec(`INSERT INTO sources(id,kind,version,parent,raw,parsed,diag,seq)
	  VALUES(?,?,?,?,?,?,?,?)`,
		src.ID, src.Kind, src.Version, src.Parent, src.Raw, src.Parsed, src.Diag, src.Seq)
	return err
}

// Sources returns all versions of a kind, ordered by version.
func (s *Store) Sources(kind string) ([]Source, error) {
	rows, err := s.db.Query(`SELECT id,kind,version,COALESCE(parent,''),raw,parsed,COALESCE(diag,''),seq
	  FROM sources WHERE kind=? ORDER BY version`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		var s0 Source
		if err := rows.Scan(&s0.ID, &s0.Kind, &s0.Version, &s0.Parent, &s0.Raw, &s0.Parsed, &s0.Diag, &s0.Seq); err != nil {
			return nil, err
		}
		out = append(out, s0)
	}
	return out, rows.Err()
}

// LatestSource returns the highest committed version of a kind, as of seq
// (seq<=0 means head). Returns nil when none exists.
func (s *Store) LatestSource(kind string, asOf int64) (*Source, error) {
	all, err := s.Sources(kind)
	if err != nil {
		return nil, err
	}
	var best *Source
	for i := range all {
		if asOf > 0 && all[i].Seq > asOf {
			continue
		}
		if best == nil || all[i].Version > best.Version {
			cp := all[i]
			best = &cp
		}
	}
	return best, nil
}

// PutDerived inserts a derived node.
func (s *Store) PutDerived(d Derived) error {
	deps, _ := json.Marshal(d.Deps)
	_, err := s.db.Exec(`INSERT INTO derived(id,kind,algo,fingerprint,deps,payload,status,seq,stale_seq)
	  VALUES(?,?,?,?,?,?,?,?,?)`,
		d.ID, d.Kind, d.Algo, d.Fingerprint, string(deps), d.Payload, d.Status, d.Seq, d.StaleSeq)
	return err
}

// MarkStale flags a derived node stale as of seq. The row stays readable.
func (s *Store) MarkStale(id string, seq int64) error {
	_, err := s.db.Exec(`UPDATE derived SET status='stale', stale_seq=? WHERE id=? AND stale_seq=0`, seq, id)
	return err
}

// DerivedByKind lists derived nodes of a kind, newest first.
func (s *Store) DerivedByKind(kind string) ([]Derived, error) {
	rows, err := s.db.Query(`SELECT id,kind,algo,fingerprint,deps,payload,status,seq,stale_seq
	  FROM derived WHERE kind=? ORDER BY seq DESC`, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDerived(rows)
}

// DerivedAll lists every derived node.
func (s *Store) DerivedAll() ([]Derived, error) {
	rows, err := s.db.Query(`SELECT id,kind,algo,fingerprint,deps,payload,status,seq,stale_seq
	  FROM derived ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDerived(rows)
}

func scanDerived(rows *sql.Rows) ([]Derived, error) {
	var out []Derived
	for rows.Next() {
		var d Derived
		var deps string
		if err := rows.Scan(&d.ID, &d.Kind, &d.Algo, &d.Fingerprint, &deps, &d.Payload, &d.Status, &d.Seq, &d.StaleSeq); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(deps), &d.Deps)
		out = append(out, d)
	}
	return out, rows.Err()
}

// GetDerived fetches one node by id.
func (s *Store) GetDerived(id string) (*Derived, error) {
	row := s.db.QueryRow(`SELECT id,kind,algo,fingerprint,deps,payload,status,seq,stale_seq
	  FROM derived WHERE id=?`, id)
	var d Derived
	var deps string
	if err := row.Scan(&d.ID, &d.Kind, &d.Algo, &d.Fingerprint, &deps, &d.Payload, &d.Status, &d.Seq, &d.StaleSeq); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(deps), &d.Deps)
	return &d, nil
}

// CurrentDerived returns the node visible as of seq: created at or before
// seq and not staled before seq. seq<=0 means head.
func (s *Store) CurrentDerived(kind string, asOf int64) (*Derived, error) {
	all, err := s.DerivedByKind(kind)
	if err != nil {
		return nil, err
	}
	for i := range all {
		d := all[i]
		if asOf > 0 && d.Seq > asOf {
			continue
		}
		if d.StaleSeq != 0 && (asOf <= 0 || d.StaleSeq <= asOf) {
			continue
		}
		return &d, nil
	}
	return nil, nil
}

// PutDecision stores a decision.
func (s *Store) PutDecision(d Decision) error {
	_, err := s.db.Exec(`INSERT INTO decisions(id,target,action,params,rationale,actor,resolves,seq,undone_seq)
	  VALUES(?,?,?,?,?,?,?,?,0)`,
		d.ID, d.Target, d.Action, d.Params, d.Rationale, d.Actor, d.Resolves, d.Seq)
	return err
}

// UndoDecision marks a decision undone (history is kept).
func (s *Store) UndoDecision(id string, seq int64) error {
	_, err := s.db.Exec(`UPDATE decisions SET undone_seq=? WHERE id=? AND undone_seq=0`, seq, id)
	return err
}

// Decisions lists all decisions, oldest first.
func (s *Store) Decisions() ([]Decision, error) {
	rows, err := s.db.Query(`SELECT id,target,action,COALESCE(params,''),COALESCE(rationale,''),
	  COALESCE(actor,''),COALESCE(resolves,''),seq,undone_seq FROM decisions ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Decision
	for rows.Next() {
		var d Decision
		if err := rows.Scan(&d.ID, &d.Target, &d.Action, &d.Params, &d.Rationale, &d.Actor, &d.Resolves, &d.Seq, &d.UndoneSeq); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ActiveDecision returns the latest non-undone decision on target as of seq.
func (s *Store) ActiveDecision(target string, asOf int64) (*Decision, error) {
	all, err := s.Decisions()
	if err != nil {
		return nil, err
	}
	var best *Decision
	for i := range all {
		d := all[i]
		if d.Target != target {
			continue
		}
		if asOf > 0 && d.Seq > asOf {
			continue
		}
		if d.UndoneSeq != 0 && (asOf <= 0 || d.UndoneSeq <= asOf) {
			continue
		}
		cp := d
		best = &cp
	}
	return best, nil
}

// Events returns events up to asOf (seq<=0 = all), ordered by seq.
func (s *Store) Events(asOf int64) ([]Event, error) {
	q := `SELECT seq,id,kind,payload,ts FROM events`
	args := []any{}
	if asOf > 0 {
		q += ` WHERE seq<=?`
		args = append(args, asOf)
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
		var payload string
		if err := rows.Scan(&e.Seq, &e.ID, &e.Kind, &payload, &e.Ts); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(payload)
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetMeta stores a metadata value.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key,value) VALUES(?,?)
	  ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// GetMeta reads a metadata value.
func (s *Store) GetMeta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// Dump is the portable export form. Identity, order and provenance are
// preserved verbatim so a fresh import reproduces the same workspace.
type Dump struct {
	Sources   []Source   `json:"sources"`
	Derived   []Derived  `json:"derived"`
	Decisions []Decision `json:"decisions"`
	Events    []Event    `json:"events"`
	Meta      [][2]string `json:"meta"`
}

// Export serializes the whole workspace.
func (s *Store) Export() (*Dump, error) {
	d := &Dump{}
	for _, kind := range []string{"sample_points","drift_correction","furnace_program","gas_valve_events","blank_curve","initial_weight"} {
		srcs, err := s.Sources(kind)
		if err != nil {
			return nil, err
		}
		d.Sources = append(d.Sources, srcs...)
	}
	var err error
	if d.Derived, err = s.DerivedAll(); err != nil {
		return nil, err
	}
	if d.Decisions, err = s.Decisions(); err != nil {
		return nil, err
	}
	if d.Events, err = s.Events(0); err != nil {
		return nil, err
	}
	rows, err := s.db.Query(`SELECT key,value FROM meta ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		d.Meta = append(d.Meta, [2]string{k, v})
	}
	return d, rows.Err()
}

// Import loads a dump into an empty store, keeping ids, order and seqs.
func (s *Store) Import(d *Dump) error {
	if s.HeadSeq() != 0 {
		return fmt.Errorf("import requires an empty workspace")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, src := range d.Sources {
		if _, err := tx.Exec(`INSERT INTO sources(id,kind,version,parent,raw,parsed,diag,seq)
		  VALUES(?,?,?,?,?,?,?,?)`, src.ID, src.Kind, src.Version, src.Parent, src.Raw, src.Parsed, src.Diag, src.Seq); err != nil {
			return fmt.Errorf("import source %s: %w", src.ID, err)
		}
	}
	for _, dv := range d.Derived {
		deps, _ := json.Marshal(dv.Deps)
		if _, err := tx.Exec(`INSERT INTO derived(id,kind,algo,fingerprint,deps,payload,status,seq,stale_seq)
		  VALUES(?,?,?,?,?,?,?,?,?)`, dv.ID, dv.Kind, dv.Algo, dv.Fingerprint, string(deps), dv.Payload, dv.Status, dv.Seq, dv.StaleSeq); err != nil {
			return fmt.Errorf("import derived %s: %w", dv.ID, err)
		}
	}
	for _, dc := range d.Decisions {
		if _, err := tx.Exec(`INSERT INTO decisions(id,target,action,params,rationale,actor,resolves,seq,undone_seq)
		  VALUES(?,?,?,?,?,?,?,?,?)`, dc.ID, dc.Target, dc.Action, dc.Params, dc.Rationale, dc.Actor, dc.Resolves, dc.Seq, dc.UndoneSeq); err != nil {
			return fmt.Errorf("import decision %s: %w", dc.ID, err)
		}
	}
	for _, e := range d.Events {
		if _, err := tx.Exec(`INSERT INTO events(seq,id,kind,payload,ts) VALUES(?,?,?,?,?)`,
			e.Seq, e.ID, e.Kind, string(e.Payload), e.Ts); err != nil {
			return fmt.Errorf("import event %d: %w", e.Seq, err)
		}
	}
	for _, kv := range d.Meta {
		if _, err := tx.Exec(`INSERT INTO meta(key,value) VALUES(?,?)`, kv[0], kv[1]); err != nil {
			return err
		}
	}
	return tx.Commit()
}

