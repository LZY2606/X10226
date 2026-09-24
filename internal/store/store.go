// Package store provides SQLite persistence: an append-only event log,
// immutable versioned input records, and derived records with staleness
// tracking. All reads support "as of event seq" queries so any historical
// state can be reconstructed without overwriting it.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"tgareview/internal/domain"

	_ "modernc.org/sqlite"
)

// Clock supplies virtual time (unix seconds). Tests inject a fixed clock.
type Clock func() int64

// Store wraps the SQLite database.
type Store struct {
	db    *sql.DB
	clock Clock
}

// Open opens (and migrates) a store at path. Use ":memory:" for tests.
func Open(path string, clock Clock) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if clock == nil {
		return nil, errors.New("store: clock is required (no wall-clock fallback)")
	}
	s := &Store{db: db, clock: clock}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  ts INTEGER NOT NULL,
  kind TEXT NOT NULL,
  payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS inputs (
  id TEXT NOT NULL,
  version INTEGER NOT NULL,
  kind TEXT NOT NULL,
  parent_version INTEGER NOT NULL DEFAULT 0,
  raw TEXT NOT NULL,
  doc TEXT NOT NULL,
  created_seq INTEGER NOT NULL,
  note TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (id, version)
);
CREATE TABLE IF NOT EXISTS derived (
  id TEXT NOT NULL,
  version INTEGER NOT NULL,
  kind TEXT NOT NULL,
  algo_version TEXT NOT NULL,
  param_fp TEXT NOT NULL,
  sources TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_seq INTEGER NOT NULL,
  stale INTEGER NOT NULL DEFAULT 0,
  stale_reason TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (id, version)
);
CREATE TABLE IF NOT EXISTS meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`)
	return err
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// Now returns the virtual clock reading.
func (s *Store) Now() int64 { return s.clock() }

// AppendEvent appends an event and returns its sequence number. The event ID
// is derived from the next sequence number, so IDs never collide even after
// an archive import restored historical events.
func (s *Store) AppendEvent(kind string, payload any) (int64, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	var next int64
	if err := s.db.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM events`).Scan(&next); err != nil {
		return 0, err
	}
	id := fmt.Sprintf("ev-%04d", next)
	res, err := s.db.Exec(`INSERT INTO events(seq, id, ts, kind, payload) VALUES(?,?,?,?,?)`,
		next, id, s.clock(), kind, string(b))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Events returns events with seq <= asOf (asOf<=0 means all), ordered by seq.
func (s *Store) Events(asOf int64) ([]domain.Event, error) {
	q := `SELECT seq, id, ts, kind, payload FROM events`
	args := []any{}
	if asOf > 0 {
		q += ` WHERE seq <= ?`
		args = append(args, asOf)
	}
	q += ` ORDER BY seq`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var p string
		if err := rows.Scan(&e.Seq, &e.ID, &e.TS, &e.Kind, &p); err != nil {
			return nil, err
		}
		e.Payload = json.RawMessage(p)
		out = append(out, e)
	}
	return out, rows.Err()
}

// HeadSeq returns the latest event sequence (0 if empty).
func (s *Store) HeadSeq() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM events`).Scan(&n)
	return n, err
}

// PutInput inserts a new immutable input version. Existing versions are
// never modified.
func (s *Store) PutInput(rec domain.InputRecord) error {
	_, err := s.db.Exec(`INSERT INTO inputs(id, version, kind, parent_version, raw, doc, created_seq, note)
		VALUES(?,?,?,?,?,?,?,?)`,
		rec.ID, rec.Version, rec.Kind, rec.ParentVersion, rec.Raw, string(rec.Doc), rec.CreatedSeq, rec.Note)
	return err
}

// LatestInputVersion returns the highest version of input id with
// created_seq <= asOf (asOf<=0 = latest overall).
func (s *Store) LatestInputVersion(id string, asOf int64) (domain.InputRecord, error) {
	q := `SELECT id, version, kind, parent_version, raw, doc, created_seq, note FROM inputs WHERE id=?`
	args := []any{id}
	if asOf > 0 {
		q += ` AND created_seq <= ?`
		args = append(args, asOf)
	}
	q += ` ORDER BY version DESC LIMIT 1`
	return s.scanInput(s.db.QueryRow(q, args...))
}

// InputVersion fetches an exact input version.
func (s *Store) InputVersion(id string, version int) (domain.InputRecord, error) {
	return s.scanInput(s.db.QueryRow(
		`SELECT id, version, kind, parent_version, raw, doc, created_seq, note FROM inputs WHERE id=? AND version=?`,
		id, version))
}

func (s *Store) scanInput(row *sql.Row) (domain.InputRecord, error) {
	var r domain.InputRecord
	var doc string
	err := row.Scan(&r.ID, &r.Version, &r.Kind, &r.ParentVersion, &r.Raw, &doc, &r.CreatedSeq, &r.Note)
	r.Doc = json.RawMessage(doc)
	return r, err
}

// LatestInputs returns the latest visible version (as of asOf) of every
// input, ordered by id.
func (s *Store) LatestInputs(asOf int64) ([]domain.InputRecord, error) {
	rows, err := s.db.Query(`SELECT id, MAX(version) FROM inputs GROUP BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.InputRecord
	for rows.Next() {
		var id string
		var v int
		if err := rows.Scan(&id, &v); err != nil {
			return nil, err
		}
		rec, err := s.InputVersion(id, v)
		if err != nil {
			return nil, err
		}
		if asOf > 0 && rec.CreatedSeq > asOf {
			rec, err = s.LatestInputVersion(id, asOf)
			if err != nil {
				continue // not yet existing at asOf
			}
		}
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, rows.Err()
}

// PutDerived inserts a new derived record version.
func (s *Store) PutDerived(rec domain.DerivedRecord) error {
	src, err := json.Marshal(rec.Sources)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(`INSERT INTO derived(id, version, kind, algo_version, param_fp, sources, payload, created_seq, stale, stale_reason)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		rec.ID, rec.Version, rec.Kind, rec.AlgoVersion, rec.ParamFP, string(src), string(rec.Payload),
		rec.CreatedSeq, boolInt(rec.Stale), rec.StaleReason)
	return err
}

// LatestDerived returns the newest version of derived id visible at asOf.
func (s *Store) LatestDerived(id string, asOf int64) (domain.DerivedRecord, error) {
	q := `SELECT id, version, kind, algo_version, param_fp, sources, payload, created_seq, stale, stale_reason
		FROM derived WHERE id=?`
	args := []any{id}
	if asOf > 0 {
		q += ` AND created_seq <= ?`
		args = append(args, asOf)
	}
	q += ` ORDER BY version DESC LIMIT 1`
	return s.scanDerived(s.db.QueryRow(q, args...))
}

// DerivedVersion fetches an exact derived version.
func (s *Store) DerivedVersion(id string, version int) (domain.DerivedRecord, error) {
	return s.scanDerived(s.db.QueryRow(
		`SELECT id, version, kind, algo_version, param_fp, sources, payload, created_seq, stale, stale_reason
		FROM derived WHERE id=? AND version=?`, id, version))
}

func (s *Store) scanDerived(row *sql.Row) (domain.DerivedRecord, error) {
	var r domain.DerivedRecord
	var src, payload string
	var stale int
	err := row.Scan(&r.ID, &r.Version, &r.Kind, &r.AlgoVersion, &r.ParamFP, &src, &payload,
		&r.CreatedSeq, &stale, &r.StaleReason)
	if err != nil {
		return r, err
	}
	r.Payload = json.RawMessage(payload)
	r.Stale = stale != 0
	return r, json.Unmarshal([]byte(src), &r.Sources)
}

// AllDerived returns every derived record version (for export).
func (s *Store) AllDerived() ([]domain.DerivedRecord, error) {
	rows, err := s.db.Query(`SELECT id, version, kind, algo_version, param_fp, sources, payload, created_seq, stale, stale_reason
		FROM derived ORDER BY id, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DerivedRecord
	for rows.Next() {
		var r domain.DerivedRecord
		var src, payload string
		var stale int
		if err := rows.Scan(&r.ID, &r.Version, &r.Kind, &r.AlgoVersion, &r.ParamFP, &src, &payload,
			&r.CreatedSeq, &stale, &r.StaleReason); err != nil {
			return nil, err
		}
		r.Payload = json.RawMessage(payload)
		r.Stale = stale != 0
		if err := json.Unmarshal([]byte(src), &r.Sources); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AllInputs returns every input version (for export), ordered by id, version.
func (s *Store) AllInputs() ([]domain.InputRecord, error) {
	rows, err := s.db.Query(`SELECT id, version, kind, parent_version, raw, doc, created_seq, note
		FROM inputs ORDER BY id, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.InputRecord
	for rows.Next() {
		var r domain.InputRecord
		var doc string
		if err := rows.Scan(&r.ID, &r.Version, &r.Kind, &r.ParentVersion, &r.Raw, &doc, &r.CreatedSeq, &r.Note); err != nil {
			return nil, err
		}
		r.Doc = json.RawMessage(doc)
		out = append(out, r)
	}
	return out, rows.Err()
}

// MarkStale flags all derived records that directly depend on input id at a
// version older than newVersion. Returns the affected records.
func (s *Store) MarkStale(inputID string, newVersion int, reason string) ([]domain.DerivedRecord, error) {
	all, err := s.AllDerived()
	if err != nil {
		return nil, err
	}
	latest := map[string]int{}
	for _, d := range all {
		if d.Version > latest[d.ID] {
			latest[d.ID] = d.Version
		}
	}
	var affected []domain.DerivedRecord
	for _, d := range all {
		if d.Stale || d.Version != latest[d.ID] {
			continue
		}
		for _, src := range d.Sources {
			if src.ID == inputID && src.Version < newVersion {
				if _, err := s.db.Exec(`UPDATE derived SET stale=1, stale_reason=? WHERE id=? AND version=?`,
					reason, d.ID, d.Version); err != nil {
					return nil, err
				}
				d.Stale = true
				d.StaleReason = reason
				affected = append(affected, d)
				break
			}
		}
	}
	return affected, nil
}

// SetMeta stores a metadata key.
func (s *Store) SetMeta(key, value string) error {
	_, err := s.db.Exec(`INSERT INTO meta(key, value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// GetMeta reads a metadata key.
func (s *Store) GetMeta(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	return v, err
}

// ---- Export / import ----

// Archive is the full portable export of a project.
type Archive struct {
	Format  string                `json:"format"` // "tgareview-archive"
	Version int                   `json:"version"`
	Events  []domain.Event        `json:"events"`
	Inputs  []domain.InputRecord  `json:"inputs"`
	Derived []domain.DerivedRecord `json:"derived"`
	Meta    map[string]string     `json:"meta,omitempty"`
}

// Export serializes the whole project preserving identity, order and
// provenance.
func (s *Store) Export() (*Archive, error) {
	evs, err := s.Events(0)
	if err != nil {
		return nil, err
	}
	inps, err := s.AllInputs()
	if err != nil {
		return nil, err
	}
	ders, err := s.AllDerived()
	if err != nil {
		return nil, err
	}
	meta := map[string]string{}
	rows, err := s.db.Query(`SELECT key, value FROM meta`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		meta[k] = v
	}
	return &Archive{Format: "tgareview-archive", Version: 1, Events: evs, Inputs: inps, Derived: ders, Meta: meta}, nil
}

// ImportArchive restores an archive into this (expected empty) store,
// preserving event sequence numbers, record identities and provenance.
func (s *Store) ImportArchive(a *Archive) error {
	if a.Format != "tgareview-archive" {
		return fmt.Errorf("unsupported archive format %q", a.Format)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range a.Events {
		if _, err := tx.Exec(`INSERT INTO events(seq, id, ts, kind, payload) VALUES(?,?,?,?,?)`,
			e.Seq, e.ID, e.TS, e.Kind, string(e.Payload)); err != nil {
			return fmt.Errorf("event seq %d: %w", e.Seq, err)
		}
	}
	for _, r := range a.Inputs {
		if _, err := tx.Exec(`INSERT INTO inputs(id, version, kind, parent_version, raw, doc, created_seq, note)
			VALUES(?,?,?,?,?,?,?,?)`,
			r.ID, r.Version, r.Kind, r.ParentVersion, r.Raw, string(r.Doc), r.CreatedSeq, r.Note); err != nil {
			return fmt.Errorf("input %s v%d: %w", r.ID, r.Version, err)
		}
	}
	for _, d := range a.Derived {
		src, _ := json.Marshal(d.Sources)
		if _, err := tx.Exec(`INSERT INTO derived(id, version, kind, algo_version, param_fp, sources, payload, created_seq, stale, stale_reason)
			VALUES(?,?,?,?,?,?,?,?,?,?)`,
			d.ID, d.Version, d.Kind, d.AlgoVersion, d.ParamFP, string(src), string(d.Payload),
			d.CreatedSeq, boolInt(d.Stale), d.StaleReason); err != nil {
			return fmt.Errorf("derived %s v%d: %w", d.ID, d.Version, err)
		}
	}
	for k, v := range a.Meta {
		if _, err := tx.Exec(`INSERT INTO meta(key, value) VALUES(?,?)
			ON CONFLICT(key) DO UPDATE SET value=excluded.value`, k, v); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// HasInput reports whether an input with this ID exists.
func (s *Store) HasInput(id string) bool {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(*) FROM inputs WHERE id=?`, id).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

// RecomputeStaleness refreshes the stale flag on the latest version of every
// derived record. A record is stale when any direct source advanced (newer
// input or derived version), when any direct source is itself stale, or when
// the current parameter fingerprint differs. Propagation runs to a fixpoint
// so local input changes expire exactly their transitive dependents.
// Returns the records newly marked stale.
func (s *Store) RecomputeStaleness(currentParamFP string) ([]domain.DerivedRecord, error) {
	all, err := s.AllDerived()
	if err != nil {
		return nil, err
	}
	latest := map[string]domain.DerivedRecord{}
	for _, d := range all {
		if cur, ok := latest[d.ID]; !ok || d.Version > cur.Version {
			latest[d.ID] = d
		}
	}
	staleSet := map[string]bool{}
	for id, d := range latest {
		if d.Stale {
			staleSet[id] = true
		}
	}
	var newly []domain.DerivedRecord
	changed := true
	for changed {
		changed = false
		for id, d := range latest {
			if staleSet[id] {
				continue
			}
			reason := ""
			if d.ParamFP != currentParamFP {
				reason = "parameter fingerprint changed"
			}
			for _, src := range d.Sources {
				if reason != "" {
					break
				}
				if s.HasInput(src.ID) {
					cur, err := s.LatestInputVersion(src.ID, 0)
					if err == nil && cur.Version > src.Version {
						reason = fmt.Sprintf("input %s advanced to v%d", src.ID, cur.Version)
					}
				} else if cur, ok := latest[src.ID]; ok {
					if cur.Version > src.Version {
						reason = fmt.Sprintf("derived %s advanced to v%d", src.ID, cur.Version)
					} else if staleSet[src.ID] {
						reason = fmt.Sprintf("derived %s is stale", src.ID)
					}
				}
			}
			if reason != "" {
				if _, err := s.db.Exec(`UPDATE derived SET stale=1, stale_reason=? WHERE id=? AND version=?`,
					reason, d.ID, d.Version); err != nil {
					return nil, err
				}
				d.Stale = true
				d.StaleReason = reason
				latest[id] = d
				staleSet[id] = true
				newly = append(newly, d)
				changed = true
			}
		}
	}
	sort.Slice(newly, func(i, j int) bool { return newly[i].ID < newly[j].ID })
	return newly, nil
}
