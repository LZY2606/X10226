// Package store persists sources, derived values and the append-only event
// log in SQLite. Raw inputs are never overwritten: revisions insert new rows.
package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"tga/internal/model"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS sources (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_id TEXT NOT NULL DEFAULT '',
  raw TEXT NOT NULL,
  format TEXT NOT NULL,
  diagnostics TEXT NOT NULL DEFAULT '[]',
  created_seq INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS derived (
  id TEXT PRIMARY KEY,
  kind TEXT NOT NULL,
  algo_version TEXT NOT NULL,
  param_fp TEXT NOT NULL,
  deps TEXT NOT NULL DEFAULT '[]',
  value TEXT NOT NULL,
  status TEXT NOT NULL,
  reason TEXT NOT NULL DEFAULT '',
  created_seq INTEGER NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  seq INTEGER PRIMARY KEY,
  id TEXT NOT NULL UNIQUE,
  type TEXT NOT NULL,
  actor TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL DEFAULT '{}',
  at TEXT NOT NULL,
  undone_by INTEGER NOT NULL DEFAULT 0
);
`)
	return err
}

// NextSeq reserves the next event sequence number (monotonic, gap-free
// enough for ordering; uses meta table so it survives restarts).
func (s *Store) NextSeq() (int64, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var cur int64
	err = tx.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM events`).Scan(&cur)
	if err != nil {
		return 0, err
	}
	next := cur + 1
	if _, err := tx.Exec(`INSERT INTO meta(key,value) VALUES('seq',?) 
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, fmt.Sprint(next)); err != nil {
		return 0, err
	}
	return next, tx.Commit()
}

func (s *Store) InsertSource(src *model.Source) error {
	diag, _ := json.Marshal(src.Diagnostics)
	if src.Diagnostics == nil {
		diag = []byte("[]")
	}
	_, err := s.db.Exec(`INSERT INTO sources(id,kind,version,parent_id,raw,format,diagnostics,created_seq,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		src.ID, string(src.Kind), src.Version, src.ParentID, src.Raw, src.Format,
		string(diag), src.CreatedSeq, src.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) Sources() ([]model.Source, error) {
	rows, err := s.db.Query(`SELECT id,kind,version,parent_id,raw,format,diagnostics,created_seq,created_at FROM sources ORDER BY created_seq, kind, version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Source
	for rows.Next() {
		var src model.Source
		var diag, at string
		if err := rows.Scan(&src.ID, &src.Kind, &src.Version, &src.ParentID, &src.Raw, &src.Format, &diag, &src.CreatedSeq, &at); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(diag), &src.Diagnostics)
		src.CreatedAt, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, src)
	}
	return out, rows.Err()
}

func (s *Store) InsertDerived(d *model.Derived) error {
	deps, _ := json.Marshal(d.Deps)
	_, err := s.db.Exec(`INSERT INTO derived(id,kind,algo_version,param_fp,deps,value,status,reason,created_seq,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.Kind, d.AlgoVersion, d.ParamFP, string(deps), string(d.Value),
		string(d.Status), d.Reason, d.CreatedSeq, d.CreatedAt.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) UpdateDerivedStatus(id string, status model.DerivedStatus, reason string) error {
	_, err := s.db.Exec(`UPDATE derived SET status=?, reason=? WHERE id=?`, string(status), reason, id)
	return err
}

func (s *Store) Derived() ([]model.Derived, error) {
	rows, err := s.db.Query(`SELECT id,kind,algo_version,param_fp,deps,value,status,reason,created_seq,created_at FROM derived ORDER BY created_seq, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Derived
	for rows.Next() {
		var d model.Derived
		var deps, at string
		if err := rows.Scan(&d.ID, &d.Kind, &d.AlgoVersion, &d.ParamFP, &deps, &d.Value, &d.Status, &d.Reason, &d.CreatedSeq, &at); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(deps), &d.Deps)
		d.Value = json.RawMessage([]byte(d.Value))
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) AppendEvent(ev *model.Event) error {
	_, err := s.db.Exec(`INSERT INTO events(seq,id,type,actor,payload,at,undone_by) VALUES(?,?,?,?,?,?,0)`,
		ev.Seq, ev.ID, ev.Type, ev.Actor, string(ev.Payload), ev.At.UTC().Format(time.RFC3339Nano))
	return err
}

func (s *Store) MarkUndone(targetSeq, undoSeq int64) error {
	_, err := s.db.Exec(`UPDATE events SET undone_by=? WHERE seq=?`, undoSeq, targetSeq)
	return err
}

func (s *Store) Events() ([]model.Event, error) {
	rows, err := s.db.Query(`SELECT seq,id,type,actor,payload,at,undone_by FROM events ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Event
	for rows.Next() {
		var ev model.Event
		var payload, at string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.Type, &ev.Actor, &payload, &at, &ev.UndoneBy); err != nil {
			return nil, err
		}
		ev.Payload = json.RawMessage(payload)
		ev.At, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// CountSources reports how many source rows exist (used to detect empty DB).
func (s *Store) CountSources() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM sources`).Scan(&n)
	return n, err
}
