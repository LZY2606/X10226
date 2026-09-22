// Package store implements the append-only event log, immutable raw layers,
// derived-value registry and human-decision journal for the TGA review system.
//
// Design rules honored here:
//   - Raw input, once committed, is never overwritten; revisions are new
//     versions linked to their parent version.
//   - Everything that happens is an event in a single ordered log (seq).
//   - Derived values carry algorithm version, parameter fingerprint and the
//     direct sources they were computed from.
//   - Historical state at any event seq can be reconstructed (time travel).
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	_ "modernc.org/sqlite"
)

// Layer kinds, imported as raw text and kept in separate strata.
const (
	KindSamples  = "samples"  // 原始采样点 time_s,mass_mg,temp_c
	KindDrift    = "drift"    // 天平漂移校正 time_s,drift_mg
	KindFurnace  = "furnace"  // 炉程序 time_s,setpoint_c
	KindValves   = "valves"   // 气体阀事件 time_s,gas
	KindBlank    = "blank"    // 空白曲线 temp_c,apparent_mg
	KindInitMass = "initmass" // 样品初重分层 stratum,mass_mg
)

// LayerKinds lists every stratum in canonical order.
var LayerKinds = []string{KindSamples, KindDrift, KindFurnace, KindValves, KindBlank, KindInitMass}

// LayerUnits documents the unit / coordinate basis of each column. Every
// domain quantity carries its unit; normalization happens only in derive.
var LayerUnits = map[string][3]string{
	KindSamples:  {"s", "mg", "degC"},
	KindDrift:    {"s", "mg", ""},
	KindFurnace:  {"s", "degC", ""},
	KindValves:   {"s", "gas-name", ""},
	KindBlank:    {"degC", "mg", ""},
	KindInitMass: {"stratum", "mg", ""},
}

// Layer status values.
const (
	LayerOK     = "ok"
	LayerFailed = "failed"
)

// Batch status values.
const (
	BatchOK      = "ok"
	BatchPartial = "partial" // 导入半失败: some strata committed, some failed
	BatchFailed  = "failed"
)

// Derived status values.
const (
	DerivedCurrent = "current"
	DerivedStale   = "stale" // expired by a newer input; viewable, never current
)

// Event kinds.
const (
	EvBatchCreated     = "batch_created"
	EvLayerCommitted   = "layer_committed"
	EvLayerFailed      = "layer_failed"
	EvParamsCommitted  = "params_committed"
	EvDerivedComputed  = "derived_computed"
	EvDerivedStaled    = "derived_staled"
	EvJudgment         = "judgment" // machine-readable domain judgment
	EvDecisionMade     = "decision_made"
	EvDecisionUndone   = "decision_undone"
	EvConflictDetected = "conflict_detected"
	EvConflictResolved = "conflict_resolved"
	EvExported         = "exported"
	EvDBImported       = "db_imported"
)

// Judgment sub-types (payload.jtype of EvJudgment events).
const (
	JAtmosphereDelay   = "atmosphere_delay"    // 气氛延迟: 阀切换时间 != 样品反应时间
	JTemperatureLag    = "temperature_lag"     // 温度滞后
	JBaselineDrift     = "baseline_drift"      // 天平基线漂移校正已应用
	JOverlapAllowed    = "overlap_allowed"     // 阶段可重叠
	JNoPercentSum      = "no_percent_sum"      // 不强制总和为百分之百
	JGapNoInterp       = "gap_no_interp"       // 数据缺口不跨越插值
	JNonMonotonicNoise = "nonmonotonic_noise"  // 非单调噪声
	JInitMassStrata    = "initmass_strata"     // 初重分层取值依据
	JInitMassRevised   = "initmass_revised"    // 初重修订
	JImportPartial     = "import_partial"      // 导入半失败
)

// Event is one ordered entry in the append-only log.
type Event struct {
	Seq     int64           `json:"seq"`
	ID      string          `json:"id"`
	TS      int64           `json:"ts"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// Batch groups the strata of one import action.
type Batch struct {
	ID         string `json:"id"`
	Note       string `json:"note"`
	Status     string `json:"status"`
	CreatedSeq int64  `json:"created_seq"`
}

// Layer is one immutable version of one raw stratum.
type Layer struct {
	ID         string `json:"id"`
	BatchID    string `json:"batch_id"`
	Kind       string `json:"kind"`
	Version    int    `json:"version"`
	ParentID   string `json:"parent_id"`
	Status     string `json:"status"`
	Meta       string `json:"meta"` // units / coordinate basis, JSON
	Raw        string `json:"raw"`  // original representation, never normalized
	CreatedSeq int64  `json:"created_seq"`
}

// Row is one parsed record of a layer. Unconvertible values stay in place
// (OK=false) with a diagnostic; they are never dropped for plotting.
type Row struct {
	ID      string `json:"id"`
	LayerID string `json:"layer_id"`
	Idx     int    `json:"idx"`
	Raw     string `json:"raw"`
	S1      string `json:"s1"`
	T       float64 `json:"t"`
	V1      float64 `json:"v1"`
	V2      float64 `json:"v2"`
	OK      bool   `json:"ok"`
	Diag    string `json:"diag"`
}

// Params is one immutable version of a named parameter set.
type Params struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Version    int    `json:"version"`
	ParentID   string `json:"parent_id"`
	JSON       string `json:"json"`
	CreatedSeq int64  `json:"created_seq"`
}

// Derived is a computed value with full provenance.
type Derived struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Algo        string   `json:"algo"`         // algorithm version
	Fingerprint string   `json:"fingerprint"`  // parameter fingerprint
	Sources     []string `json:"sources"`      // direct source ids
	Status      string   `json:"status"`
	Payload     string   `json:"payload"`
	CreatedSeq  int64    `json:"created_seq"`
	StaleSeq    int64    `json:"stale_seq"`
}

// Decision is a human ruling on a target (usually a stage candidate).
type Decision struct {
	ID         string   `json:"id"`
	Target     string   `json:"target"`
	Action     string   `json:"action"` // accept | reject | adjust
	Actor      string   `json:"actor"`
	Reason     string   `json:"reason"`
	Supersedes []string `json:"supersedes"`
	CreatedSeq int64    `json:"created_seq"`
	UndoneSeq  int64    `json:"undone_seq"`
}

// Store wraps the SQLite database. Now is injectable virtual time.
type Store struct {
	db  *sql.DB
	Now func() int64
}

// Open opens (and if needed initializes) the database at path.
func Open(path string) (*Store, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, Now: func() int64 { return 0 }}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS events(
  seq INTEGER PRIMARY KEY,
  id TEXT NOT NULL,
  ts INTEGER NOT NULL,
  kind TEXT NOT NULL,
  payload TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS batches(
  id TEXT PRIMARY KEY, note TEXT, status TEXT, created_seq INTEGER);
CREATE TABLE IF NOT EXISTS layers(
  id TEXT PRIMARY KEY, batch_id TEXT, kind TEXT, version INTEGER,
  parent_id TEXT, status TEXT, meta TEXT, raw TEXT, created_seq INTEGER);
CREATE TABLE IF NOT EXISTS rows(
  id TEXT PRIMARY KEY, layer_id TEXT, idx INTEGER, raw TEXT, s1 TEXT,
  t REAL, v1 REAL, v2 REAL, ok INTEGER, diag TEXT);
CREATE TABLE IF NOT EXISTS params(
  id TEXT PRIMARY KEY, name TEXT, version INTEGER, parent_id TEXT,
  json TEXT, created_seq INTEGER);
CREATE TABLE IF NOT EXISTS derived(
  id TEXT PRIMARY KEY, kind TEXT, algo TEXT, fingerprint TEXT, sources TEXT,
  status TEXT, payload TEXT, created_seq INTEGER, stale_seq INTEGER);
CREATE TABLE IF NOT EXISTS decisions(
  id TEXT PRIMARY KEY, target TEXT, action TEXT, actor TEXT, reason TEXT,
  supersedes TEXT, created_seq INTEGER, undone_seq INTEGER);
CREATE INDEX IF NOT EXISTS idx_rows_layer ON rows(layer_id, idx);
CREATE INDEX IF NOT EXISTS idx_derived_kind ON derived(kind, status);
CREATE INDEX IF NOT EXISTS idx_decisions_target ON decisions(target);
`)
	return err
}

// Seq returns the current maximum event sequence (0 when empty).
func (s *Store) Seq() int64 {
	var n sql.NullInt64
	_ = s.db.QueryRow(`SELECT MAX(seq) FROM events`).Scan(&n)
	return n.Int64
}

func (s *Store) emitTx(tx *sql.Tx, kind string, payload any) (Event, error) {
	var n sql.NullInt64
	if err := tx.QueryRow(`SELECT MAX(seq) FROM events`).Scan(&n); err != nil {
		return Event{}, err
	}
	seq := n.Int64 + 1
	raw, err := json.Marshal(payload)
	if err != nil {
		return Event{}, err
	}
	ev := Event{Seq: seq, ID: fmt.Sprintf("evt-%d", seq), TS: s.Now(), Kind: kind, Payload: raw}
	_, err = tx.Exec(`INSERT INTO events(seq,id,ts,kind,payload) VALUES(?,?,?,?,?)`,
		ev.Seq, ev.ID, ev.TS, ev.Kind, string(raw))
	return ev, err
}

// Emit appends an event outside of a larger operation.
func (s *Store) Emit(kind string, payload any) (Event, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return Event{}, err
	}
	defer tx.Rollback()
	ev, err := s.emitTx(tx, kind, payload)
	if err != nil {
		return Event{}, err
	}
	return ev, tx.Commit()
}

// Events returns events with seq > after, ordered by seq, up to limit.
func (s *Store) Events(after int64, limit int) ([]Event, error) {
	q := `SELECT seq,id,ts,kind,payload FROM events WHERE seq>? ORDER BY seq`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	rows, err := s.db.Query(q, after)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		var p string
		if err := rows.Scan(&e.Seq, &e.ID, &e.TS, &e.Kind, &p); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(p)
		out = append(out, e)
	}
	return out, rows.Err()
}

// Event returns a single event by seq.
func (s *Store) Event(seq int64) (Event, error) {
	var e Event
	var p string
	err := s.db.QueryRow(`SELECT seq,id,ts,kind,payload FROM events WHERE seq=?`, seq).
		Scan(&e.Seq, &e.ID, &e.TS, &e.Kind, &p)
	e.Payload = json.RawMessage(p)
	return e, err
}

// Judgments returns all machine-readable judgment events, optionally filtered
// by judgment sub-type, up to and including seq at (0 = no bound).
func (s *Store) Judgments(jtype string, at int64) ([]Event, error) {
	q := `SELECT seq,id,ts,kind,payload FROM events WHERE kind=?`
	args := []any{EvJudgment}
	if at > 0 {
		q += ` AND seq<=?`
		args = append(args, at)
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
		if err := rows.Scan(&e.Seq, &e.ID, &e.TS, &e.Kind, &p); err != nil {
			return nil, err
		}
		if jtype != "" {
			var pl struct {
				JType string `json:"jtype"`
			}
			if json.Unmarshal([]byte(p), &pl) != nil || pl.JType != jtype {
				continue
			}
		}
		e.Payload = json.RawMessage(p)
		out = append(out, e)
	}
	return out, rows.Err()
}
