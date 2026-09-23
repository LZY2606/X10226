package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"tgareview/internal/domain"
)

// Source is one immutable version of a raw input layer.
type Source struct {
	ID          string              `json:"id"`
	Kind        string              `json:"kind"`
	Version     int                 `json:"version"`
	ParentID    string              `json:"parent_id,omitempty"`
	Payload     json.RawMessage     `json:"payload"`
	Diagnostics []domain.Diagnostic `json:"diagnostics"`
	CreatedSeq  int64               `json:"created_seq"`
}

// Event is one append-only history entry.
type Event struct {
	Seq     int64           `json:"seq"`
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
	At      time.Time       `json:"at"`
}

// Derived is one derived value with full provenance.
type Derived struct {
	ID               string          `json:"id"`
	Kind             string          `json:"kind"`
	AlgoVersion      string          `json:"algo_version"`
	ParamFingerprint string          `json:"param_fingerprint"`
	SourceIDs        []string        `json:"source_ids"`
	Payload          json.RawMessage `json:"payload"`
	CreatedSeq       int64           `json:"created_seq"`
	SupersededSeq    *int64          `json:"superseded_seq,omitempty"`
}

// AppendEvent writes an event and returns it with its assigned sequence.
func (s *Store) AppendEvent(typ string, payload any) (Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	id, err := s.NextID("evt")
	if err != nil {
		return Event{}, err
	}
	at := s.clock.Now().UTC()
	res, err := s.db.Exec(`INSERT INTO events(id,type,payload,at) VALUES(?,?,?,?)`,
		id, typ, string(raw), at.Format(time.RFC3339Nano))
	if err != nil {
		return Event{}, err
	}
	seq, _ := res.LastInsertId()
	return Event{Seq: seq, ID: id, Type: typ, Payload: raw, At: at}, nil
}

// PutSource stores a new immutable source version and emits source.imported
// or source.revised. The raw payload is preserved verbatim.
func (s *Store) PutSource(kind string, payload json.RawMessage, diags []domain.Diagnostic, parentID string) (Source, error) {
	if diags == nil {
		diags = []domain.Diagnostic{}
	}
	version := 1
	if parentID != "" {
		var pv int
		if err := s.db.QueryRow(`SELECT version FROM sources WHERE id=?`, parentID).Scan(&pv); err != nil {
			return Source{}, fmt.Errorf("parent source %s: %w", parentID, err)
		}
		version = pv + 1
	}
	id, err := s.NextID("src")
	if err != nil {
		return Source{}, err
	}
	diagRaw, _ := json.Marshal(diags)
	// created_seq is the next event seq; use current max+1 so ordering holds.
	var maxSeq int64
	s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM events`).Scan(&maxSeq)
	createdSeq := maxSeq + 1
	if _, err := s.db.Exec(`INSERT INTO sources(id,kind,version,parent_id,payload,diagnostics,created_seq) VALUES(?,?,?,?,?,?,?)`,
		id, kind, version, nullStr(parentID), string(payload), string(diagRaw), createdSeq); err != nil {
		return Source{}, err
	}
	typ := "source.imported"
	if parentID != "" {
		typ = "source.revised"
	}
	ev, err := s.AppendEvent(typ, map[string]any{
		"source_id": id, "kind": kind, "version": version, "parent_id": parentID,
		"diagnostics": diags,
	})
	if err != nil {
		return Source{}, err
	}
	return Source{ID: id, Kind: kind, Version: version, ParentID: parentID,
		Payload: payload, Diagnostics: diags, CreatedSeq: ev.Seq}, nil
}

func nullStr(v string) any {
	if v == "" {
		return nil
	}
	return v
}

// HeadSource returns the newest version of a kind visible at seq (0 = now).
func (s *Store) HeadSource(kind string, atSeq int64) (*Source, error) {
	q := `SELECT id,kind,version,COALESCE(parent_id,''),payload,diagnostics,created_seq
	      FROM sources WHERE kind=?`
	args := []any{kind}
	if atSeq > 0 {
		q += ` AND created_seq<=?`
		args = append(args, atSeq)
	}
	q += ` ORDER BY version DESC, created_seq DESC LIMIT 1`
	return s.scanOneSource(q, args...)
}

func (s *Store) scanOneSource(q string, args ...any) (*Source, error) {
	var src Source
	var diags string
	err := s.db.QueryRow(q, args...).Scan(&src.ID, &src.Kind, &src.Version, &src.ParentID,
		&src.Payload, &diags, &src.CreatedSeq)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	src.Diagnostics = []domain.Diagnostic{}
	json.Unmarshal([]byte(diags), &src.Diagnostics)
	return &src, nil
}

// Sources returns every version of every kind, ordered for display.
func (s *Store) Sources() ([]Source, error) {
	rows, err := s.db.Query(`SELECT id,kind,version,COALESCE(parent_id,''),payload,diagnostics,created_seq
		FROM sources ORDER BY kind, version, created_seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Source
	for rows.Next() {
		var src Source
		var diags string
		if err := rows.Scan(&src.ID, &src.Kind, &src.Version, &src.ParentID, &src.Payload, &diags, &src.CreatedSeq); err != nil {
			return nil, err
		}
		src.Diagnostics = []domain.Diagnostic{}
		json.Unmarshal([]byte(diags), &src.Diagnostics)
		out = append(out, src)
	}
	return out, rows.Err()
}

// PutDerived stores a derived value. Older current records of the same kind
// are superseded at the same sequence so history stays reconstructable.
func (s *Store) PutDerived(kind, algoVersion, fingerprint string, sourceIDs []string, payload any) (Derived, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Derived{}, err
	}
	ids, _ := json.Marshal(sourceIDs)
	id, err := s.NextID("drv")
	if err != nil {
		return Derived{}, err
	}
	var maxSeq int64
	s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM events`).Scan(&maxSeq)
	createdSeq := maxSeq + 1
	err = s.withTx(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE derived SET superseded_seq=? WHERE kind=? AND superseded_seq IS NULL`, createdSeq, kind); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT INTO derived(id,kind,algo_version,param_fingerprint,source_ids,payload,created_seq) VALUES(?,?,?,?,?,?,?)`,
			id, kind, algoVersion, fingerprint, string(ids), string(raw), createdSeq)
		return err
	})
	if err != nil {
		return Derived{}, err
	}
	return Derived{ID: id, Kind: kind, AlgoVersion: algoVersion, ParamFingerprint: fingerprint,
		SourceIDs: sourceIDs, Payload: raw, CreatedSeq: createdSeq}, nil
}

// DerivedAt returns the derived record of a kind visible at seq (0 = now).
func (s *Store) DerivedAt(kind string, atSeq int64) (*Derived, error) {
	q := `SELECT id,kind,algo_version,param_fingerprint,source_ids,payload,created_seq,superseded_seq
	      FROM derived WHERE kind=?`
	args := []any{kind}
	if atSeq > 0 {
		q += ` AND created_seq<=? AND (superseded_seq IS NULL OR superseded_seq>?)`
		args = append(args, atSeq, atSeq)
	} else {
		q += ` AND superseded_seq IS NULL`
	}
	q += ` ORDER BY created_seq DESC LIMIT 1`
	return s.scanOneDerived(q, args...)
}

// AllDerived returns every derived record (including superseded), newest first.
func (s *Store) AllDerived(kind string) ([]Derived, error) {
	q := `SELECT id,kind,algo_version,param_fingerprint,source_ids,payload,created_seq,superseded_seq
	      FROM derived WHERE kind=? ORDER BY created_seq DESC`
	rows, err := s.db.Query(q, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanDerivedRows(rows)
}

func (s *Store) scanOneDerived(q string, args ...any) (*Derived, error) {
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list, err := scanDerivedRows(rows)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

func scanDerivedRows(rows *sql.Rows) ([]Derived, error) {
	var out []Derived
	for rows.Next() {
		var d Derived
		var ids string
		var sup sql.NullInt64
		if err := rows.Scan(&d.ID, &d.Kind, &d.AlgoVersion, &d.ParamFingerprint, &ids, &d.Payload, &d.CreatedSeq, &sup); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(ids), &d.SourceIDs)
		if sup.Valid {
			v := sup.Int64
			d.SupersededSeq = &v
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// Events returns events with seq<=atSeq (0 = all), ordered by seq.
func (s *Store) Events(atSeq int64) ([]Event, error) {
	q := `SELECT seq,id,type,payload,at FROM events`
	args := []any{}
	if atSeq > 0 {
		q += ` WHERE seq<=?`
		args = append(args, atSeq)
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
		var at string
		if err := rows.Scan(&e.Seq, &e.ID, &e.Type, &e.Payload, &at); err != nil {
			return nil, err
		}
		e.At, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// MaxSeq returns the latest event sequence (0 when empty).
func (s *Store) MaxSeq() int64 {
	var v int64
	s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM events`).Scan(&v)
	return v
}
