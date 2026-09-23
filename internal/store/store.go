// Package store is the append-only SQLite persistence for events, immutable
// layer versions and derived values.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"tga/internal/model"
)

// Store wraps a SQLite database.
type Store struct {
	DB *sql.DB
}

// Open opens (or creates) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if err := s.init(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) init() error {
	_, err := s.DB.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS meta (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  seq     INTEGER PRIMARY KEY,
  id      TEXT NOT NULL UNIQUE,
  time    TEXT NOT NULL,
  type    TEXT NOT NULL,
  payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS layers (
  id          TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  version     INTEGER NOT NULL,
  parent_id   TEXT NOT NULL DEFAULT '',
  event_id    TEXT NOT NULL,
  partial     INTEGER NOT NULL DEFAULT 0,
  payload     TEXT NOT NULL,
  diagnostics TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS derived (
  id          TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  algo        TEXT NOT NULL,
  fingerprint TEXT NOT NULL,
  sources     TEXT NOT NULL,
  event_id    TEXT NOT NULL,
  payload     TEXT NOT NULL
);
`

// NextSeq returns the next event sequence number.
func (s *Store) NextSeq() (int64, error) {
	var max sql.NullInt64
	if err := s.DB.QueryRow(`SELECT MAX(seq) FROM events`).Scan(&max); err != nil {
		return 0, err
	}
	if !max.Valid {
		return 1, nil
	}
	return max.Int64 + 1, nil
}

// AppendEvent stores a new event. Seq and ID must already be assigned;
// preserving explicit seq/ID is what makes export/import identity stable.
func (s *Store) AppendEvent(e model.Event) error {
	_, err := s.DB.Exec(
		`INSERT INTO events(seq,id,time,type,payload) VALUES(?,?,?,?,?)`,
		e.Seq, e.ID, e.Time.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"), e.Type, string(e.Payload),
	)
	return err
}

// ListEvents returns all events ordered by seq.
func (s *Store) ListEvents() ([]model.Event, error) {
	rows, err := s.DB.Query(`SELECT seq,id,time,type,payload FROM events ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]model.Event, error) {
	var out []model.Event
	for rows.Next() {
		var e model.Event
		var ts, p string
		if err := rows.Scan(&e.Seq, &e.ID, &ts, &e.Type, &p); err != nil {
			return nil, err
		}
		var err error
		e.Time, err = parseTime(ts)
		if err != nil {
			return nil, err
		}
		e.Payload = []byte(p)
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetEvent returns one event by ID.
func (s *Store) GetEvent(id string) (*model.Event, error) {
	var e model.Event
	var ts, p string
	err := s.DB.QueryRow(`SELECT seq,id,time,type,payload FROM events WHERE id=?`, id).
		Scan(&e.Seq, &e.ID, &ts, &e.Type, &p)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	e.Time, err = parseTime(ts)
	e.Payload = []byte(p)
	return &e, err
}

// UpsertLayer stores an immutable layer version.
func (s *Store) UpsertLayer(l model.LayerVersion) error {
	diags, _ := json.Marshal(l.Diagnostics)
	_, err := s.DB.Exec(
		`INSERT INTO layers(id,kind,version,parent_id,event_id,partial,payload,diagnostics)
		 VALUES(?,?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO NOTHING`,
		l.ID, string(l.Kind), l.Version, l.ParentID, l.EventID,
		l.Partial, string(l.Payload), string(diags),
	)
	return err
}

// ListLayers returns all layer versions ordered by kind, version.
func (s *Store) ListLayers() ([]model.LayerVersion, error) {
	rows, err := s.DB.Query(
		`SELECT id,kind,version,parent_id,event_id,partial,payload,diagnostics
		 FROM layers ORDER BY kind, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.LayerVersion
	for rows.Next() {
		l, err := scanLayer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *l)
	}
	return out, rows.Err()
}

// GetLayer returns one layer version by ID.
func (s *Store) GetLayer(id string) (*model.LayerVersion, error) {
	row := s.DB.QueryRow(
		`SELECT id,kind,version,parent_id,event_id,partial,payload,diagnostics
		 FROM layers WHERE id=?`, id)
	l, err := scanLayer(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return l, err
}

type scanner interface {
	Scan(dest ...any) error
}

func scanLayer(r scanner) (*model.LayerVersion, error) {
	var l model.LayerVersion
	var kind, payload, diags string
	var partial int
	if err := r.Scan(&l.ID, &kind, &l.Version, &l.ParentID, &l.EventID,
		&partial, &payload, &diags); err != nil {
		return nil, err
	}
	l.Kind = model.LayerKind(kind)
	l.Partial = partial != 0
	l.Payload = []byte(payload)
	if err := json.Unmarshal([]byte(diags), &l.Diagnostics); err != nil {
		return nil, err
	}
	return &l, nil
}

// UpsertDerived stores a derived value; identical fingerprints are shared.
func (s *Store) UpsertDerived(d model.DerivedValue) error {
	srcs, _ := json.Marshal(d.SourceRefs)
	_, err := s.DB.Exec(
		`INSERT INTO derived(id,kind,algo,fingerprint,sources,event_id,payload)
		 VALUES(?,?,?,?,?,?,?)
		 ON CONFLICT(id) DO NOTHING`,
		d.ID, d.Kind, d.AlgoVersion, d.Fingerprint, string(srcs), d.EventID, string(d.Payload),
	)
	return err
}

// ListDerived returns all stored derived values.
func (s *Store) ListDerived() ([]model.DerivedValue, error) {
	rows, err := s.DB.Query(
		`SELECT id,kind,algo,fingerprint,sources,event_id,payload FROM derived ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DerivedValue
	for rows.Next() {
		var d model.DerivedValue
		var srcs, payload string
		if err := rows.Scan(&d.ID, &d.Kind, &d.AlgoVersion, &d.Fingerprint,
			&srcs, &d.EventID, &payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(srcs), &d.SourceRefs); err != nil {
			return nil, err
		}
		d.Payload = []byte(payload)
		out = append(out, d)
	}
	return out, rows.Err()
}

// MetaGet reads a meta value; missing returns ("", nil).
func (s *Store) MetaGet(key string) (string, error) {
	var v string
	err := s.DB.QueryRow(`SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

// MetaSet writes a meta value.
func (s *Store) MetaSet(key, value string) error {
	_, err := s.DB.Exec(
		`INSERT INTO meta(key,value) VALUES(?,?)
		 ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// ImportBundle atomically inserts events/layers/derived rows with preserved
// IDs and sequence order, used when re-importing an export file.
func (s *Store) ImportBundle(evs []model.Event, layers []model.LayerVersion, derived []model.DerivedValue) error {
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range evs {
		if _, err := tx.Exec(
			`INSERT INTO events(seq,id,time,type,payload) VALUES(?,?,?,?,?)`,
			e.Seq, e.ID, e.Time.UTC().Format("2006-01-02T15:04:05.000000000Z07:00"),
			e.Type, string(e.Payload)); err != nil {
			return fmt.Errorf("import event %s: %w", e.ID, err)
		}
	}
	for _, l := range layers {
		diags, _ := json.Marshal(l.Diagnostics)
		if _, err := tx.Exec(
			`INSERT INTO layers(id,kind,version,parent_id,event_id,partial,payload,diagnostics)
			 VALUES(?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
			l.ID, string(l.Kind), l.Version, l.ParentID, l.EventID,
			l.Partial, string(l.Payload), string(diags)); err != nil {
			return fmt.Errorf("import layer %s: %w", l.ID, err)
		}
	}
	for _, d := range derived {
		srcs, _ := json.Marshal(d.SourceRefs)
		if _, err := tx.Exec(
			`INSERT INTO derived(id,kind,algo,fingerprint,sources,event_id,payload)
			 VALUES(?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
			d.ID, d.Kind, d.AlgoVersion, d.Fingerprint, string(srcs), d.EventID,
			string(d.Payload)); err != nil {
			return fmt.Errorf("import derived %s: %w", d.ID, err)
		}
	}
	return tx.Commit()
}

// Reset removes all rows (used by tests and by "delete local database").
func (s *Store) Reset() error {
	_, err := s.DB.Exec(`DELETE FROM events; DELETE FROM layers; DELETE FROM derived; DELETE FROM meta;`)
	return err
}

func parseTime(ts string) (time.Time, error) {
	return time.Parse("2006-01-02T15:04:05.000000000Z07:00", ts)
}
