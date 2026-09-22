// Package store persists layers, versions, derived results, decisions and
// the append-only event log in SQLite. All timestamps come from an
// injectable clock so tests can use virtual time.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"tga/internal/model"
)

const schema = `
CREATE TABLE IF NOT EXISTS layers (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL UNIQUE,
  created_seq INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS layer_versions (
  id TEXT PRIMARY KEY,
  layer_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  parent_id TEXT NOT NULL DEFAULT '',
  seq INTEGER NOT NULL,
  payload TEXT NOT NULL,
  units TEXT NOT NULL DEFAULT '{}',
  diagnostic TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_lv_layer ON layer_versions(layer_id, seq);
CREATE TABLE IF NOT EXISTS results (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  algo_version TEXT NOT NULL,
  param_fp TEXT NOT NULL,
  sources TEXT NOT NULL,
  seq INTEGER NOT NULL,
  status TEXT NOT NULL,
  payload TEXT NOT NULL,
  diagnostic TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_results_kind ON results(kind, seq);
CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY,
  seq INTEGER NOT NULL,
  target_id TEXT NOT NULL,
  action TEXT NOT NULL,
  params TEXT NOT NULL DEFAULT '',
  rationale TEXT NOT NULL DEFAULT '',
  parent_id TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY,
  id TEXT NOT NULL,
  type TEXT NOT NULL,
  payload TEXT NOT NULL,
  at TEXT NOT NULL
);
`

// Store wraps the SQLite database.
type Store struct {
	db  *sql.DB
	now func() time.Time
}

// Open opens (or creates) the database at path. Use ":memory:" for tests.
func Open(path string, now func() time.Time) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db, now: now}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// NextSeq returns the next event sequence number.
func (s *Store) NextSeq() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM events`).Scan(&n)
	return n, err
}

// AppendEvent inserts an event with an explicit seq (idempotent on seq).
func (s *Store) AppendEvent(seq int64, id, typ string, payload any) (*model.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	at := s.now().UTC()
	_, err = s.db.Exec(`INSERT OR IGNORE INTO events(seq,id,type,payload,at) VALUES(?,?,?,?,?)`,
		seq, id, typ, string(raw), at.Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	return &model.Event{Seq: seq, ID: id, Type: typ, Payload: raw, At: at}, nil
}

// AddLayerVersion inserts a new immutable version for a layer kind.
// The layer row is created on first use. Ids are stable and seq-derived.
func (s *Store) AddLayerVersion(seq int64, kind model.LayerKind, parentID string, payload json.RawMessage, units map[string]string, diagnostic string) (*model.LayerVersion, error) {
	layerID := "layer:" + string(kind)
	if _, err := s.db.Exec(`INSERT OR IGNORE INTO layers(id,kind,created_seq) VALUES(?,?,?)`, layerID, string(kind), seq); err != nil {
		return nil, err
	}
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM layer_versions WHERE layer_id=?`, layerID).Scan(&n); err != nil {
		return nil, err
	}
	verID := fmt.Sprintf("%s:v%d", layerID, n+1)
	unitsRaw, _ := json.Marshal(units)
	lv := &model.LayerVersion{
		ID: verID, LayerID: layerID, Kind: kind, ParentID: parentID, Seq: seq,
		Payload: payload, Units: units, Diagnostic: diagnostic,
	}
	_, err := s.db.Exec(`INSERT INTO layer_versions(id,layer_id,kind,parent_id,seq,payload,units,diagnostic)
		VALUES(?,?,?,?,?,?,?,?)`,
		lv.ID, lv.LayerID, string(kind), parentID, seq, string(payload), string(unitsRaw), diagnostic)
	if err != nil {
		return nil, err
	}
	return lv, nil
}

// LayerVersions returns all versions of a kind ordered by seq.
func (s *Store) LayerVersions(kind model.LayerKind) ([]*model.LayerVersion, error) {
	rows, err := s.db.Query(`SELECT id,layer_id,kind,parent_id,seq,payload,units,diagnostic
		FROM layer_versions WHERE kind=? ORDER BY seq`, string(kind))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVersions(rows)
}

// AllLayerVersions returns every version of every layer ordered by seq.
func (s *Store) AllLayerVersions() ([]*model.LayerVersion, error) {
	rows, err := s.db.Query(`SELECT id,layer_id,kind,parent_id,seq,payload,units,diagnostic
		FROM layer_versions ORDER BY seq, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanVersions(rows)
}

func scanVersions(rows *sql.Rows) ([]*model.LayerVersion, error) {
	var out []*model.LayerVersion
	for rows.Next() {
		var lv model.LayerVersion
		var units, payload string
		if err := rows.Scan(&lv.ID, &lv.LayerID, (*string)(&lv.Kind), &lv.ParentID, &lv.Seq, &payload, &units, &lv.Diagnostic); err != nil {
			return nil, err
		}
		lv.Payload = json.RawMessage(payload)
		_ = json.Unmarshal([]byte(units), &lv.Units)
		out = append(out, &lv)
	}
	return out, rows.Err()
}

// CurrentVersion returns the latest version of a kind at or before atSeq
// (atSeq <= 0 means latest overall). ok is false when none exists.
func (s *Store) CurrentVersion(kind model.LayerKind, atSeq int64) (*model.LayerVersion, bool, error) {
	q := `SELECT id,layer_id,kind,parent_id,seq,payload,units,diagnostic FROM layer_versions WHERE kind=?`
	args := []any{string(kind)}
	if atSeq > 0 {
		q += ` AND seq<=?`
		args = append(args, atSeq)
	}
	q += ` ORDER BY seq DESC LIMIT 1`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, false, err
	}
	defer rows.Close()
	vs, err := scanVersions(rows)
	if err != nil || len(vs) == 0 {
		return nil, false, err
	}
	return vs[0], true, nil
}

// InsertResult stores a derived result.
func (s *Store) InsertResult(r *model.Result) error {
	src, _ := json.Marshal(r.Sources)
	_, err := s.db.Exec(`INSERT OR REPLACE INTO results(id,kind,algo_version,param_fp,sources,seq,status,payload,diagnostic)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.Kind, r.AlgoVersion, r.ParamFP, string(src), r.Seq, r.Status, string(r.Payload), r.Diagnostic)
	return err
}

// Results returns results of a kind ("" = all) recorded at or before atSeq
// (atSeq <= 0 = no bound), ordered by seq then id.
func (s *Store) Results(kind string, atSeq int64) ([]*model.Result, error) {
	q := `SELECT id,kind,algo_version,param_fp,sources,seq,status,payload,diagnostic FROM results WHERE 1=1`
	var args []any
	if kind != "" {
		q += ` AND kind=?`
		args = append(args, kind)
	}
	if atSeq > 0 {
		q += ` AND seq<=?`
		args = append(args, atSeq)
	}
	q += ` ORDER BY seq, id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Result
	for rows.Next() {
		var r model.Result
		var src, payload string
		if err := rows.Scan(&r.ID, &r.Kind, &r.AlgoVersion, &r.ParamFP, &src, &r.Seq, &r.Status, &payload, &r.Diagnostic); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(src), &r.Sources)
		r.Payload = json.RawMessage(payload)
		out = append(out, &r)
	}
	return out, rows.Err()
}

// GetResult fetches one result by id.
func (s *Store) GetResult(id string) (*model.Result, bool, error) {
	all, err := s.Results("", 0)
	if err != nil {
		return nil, false, err
	}
	for _, r := range all {
		if r.ID == id {
			return r, true, nil
		}
	}
	return nil, false, nil
}

// SetResultStatus updates the cached status of a result.
func (s *Store) SetResultStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE results SET status=? WHERE id=?`, status, id)
	return err
}

// AddDecision records a human decision.
func (s *Store) AddDecision(seq int64, targetID, action, params, rationale, parentID string) (*model.Decision, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM decisions`).Scan(&n); err != nil {
		return nil, err
	}
	d := &model.Decision{
		ID: fmt.Sprintf("dec-%d", n+1), Seq: seq, TargetID: targetID,
		Action: action, Params: params, Rationale: rationale, ParentID: parentID,
	}
	_, err := s.db.Exec(`INSERT INTO decisions(id,seq,target_id,action,params,rationale,parent_id)
		VALUES(?,?,?,?,?,?,?)`, d.ID, d.Seq, d.TargetID, d.Action, d.Params, d.Rationale, d.ParentID)
	if err != nil {
		return nil, err
	}
	return d, nil
}

// Decisions returns decisions at or before atSeq (<=0 = all), ordered by seq.
func (s *Store) Decisions(atSeq int64) ([]*model.Decision, error) {
	q := `SELECT id,seq,target_id,action,params,rationale,parent_id FROM decisions`
	var args []any
	if atSeq > 0 {
		q += ` WHERE seq<=?`
		args = append(args, atSeq)
	}
	q += ` ORDER BY seq, id`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Decision
	for rows.Next() {
		var d model.Decision
		if err := rows.Scan(&d.ID, &d.Seq, &d.TargetID, &d.Action, &d.Params, &d.Rationale, &d.ParentID); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, rows.Err()
}

// Events returns events at or before atSeq (<=0 = all), ordered by seq.
func (s *Store) Events(atSeq int64) ([]*model.Event, error) {
	q := `SELECT seq,id,type,payload,at FROM events`
	var args []any
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
	var out []*model.Event
	for rows.Next() {
		var e model.Event
		var at string
		if err := rows.Scan(&e.Seq, &e.ID, &e.Type, &e.Payload, &at); err != nil {
			return nil, err
		}
		e.At, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, &e)
	}
	return out, rows.Err()
}

// IsEmpty reports whether any event has been recorded.
func (s *Store) IsEmpty() (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&n)
	return n == 0, err
}
