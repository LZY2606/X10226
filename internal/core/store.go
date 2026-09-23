package core

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE IF NOT EXISTS objects(
  id          TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  version     INTEGER NOT NULL,
  parent_id   TEXT NOT NULL DEFAULT '',
  content     TEXT NOT NULL,
  created_seq INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS events(
  seq     INTEGER PRIMARY KEY,
  id      TEXT UNIQUE NOT NULL,
  type    TEXT NOT NULL,
  payload TEXT NOT NULL,
  at      TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS results(
  id          TEXT PRIMARY KEY,
  kind        TEXT NOT NULL,
  algo        TEXT NOT NULL,
  params_fp   TEXT NOT NULL,
  sources     TEXT NOT NULL,
  value       TEXT NOT NULL,
  created_seq INTEGER NOT NULL
);
`

type Store struct {
	db    *sql.DB
	clock Clock
}

func Open(path string, clock Clock) (*Store, error) {
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db, clock: clock}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) Now() time.Time   { return s.clock() }
func (s *Store) NowSeq() int64 {
	var n int64
	s.db.QueryRow(`SELECT COALESCE(MAX(seq),0) FROM events`).Scan(&n)
	return n
}

// ObjectID is deterministic over kind, ancestry and raw content.
func ObjectID(kind, parentID, content string) string {
	return hashID("object", kind, parentID, content)
}

// AddObject inserts a raw input object. version is derived from the parent.
func (s *Store) AddObject(kind, parentID, content string, createdSeq int64) (Object, bool, error) {
	version := 1
	if parentID != "" {
		var pv int
		err := s.db.QueryRow(`SELECT version FROM objects WHERE id=?`, parentID).Scan(&pv)
		if err != nil {
			return Object{}, false, fmt.Errorf("parent %s not found", parentID)
		}
		version = pv + 1
	}
	id := ObjectID(kind, parentID, content)
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO objects(id,kind,version,parent_id,content,created_seq) VALUES(?,?,?,?,?,?)`,
		id, kind, version, parentID, content, createdSeq)
	if err != nil {
		return Object{}, false, err
	}
	n, _ := res.RowsAffected()
	return Object{ID: id, Kind: kind, Version: version, ParentID: parentID,
		Content: content, CreatedSeq: createdSeq}, n == 1, nil
}

// AddEvent appends an event. Events with the same (type,payload) are idempotent,
// so replaying/recomputing never duplicates history.
func (s *Store) AddEvent(typ string, payload any) (Event, bool, error) {
	pl := canonical(payload)
	var existing Event
	err := s.db.QueryRow(`SELECT seq,id,type,payload,at FROM events WHERE type=? AND payload=?`,
		typ, pl).Scan(&existing.Seq, &existing.ID, &existing.Type, &existing.Payload, &existing.At)
	if err == nil {
		return existing, false, nil
	}
	at := s.clock().UTC().Format(time.RFC3339Nano)
	id := hashID("event", typ, pl, at)
	res, err := s.db.Exec(`INSERT INTO events(id,type,payload,at) VALUES(?,?,?,?)`, id, typ, pl, at)
	if err != nil {
		return Event{}, false, err
	}
	rid, _ := res.LastInsertId()
	return Event{Seq: rid, ID: id, Type: typ, Payload: pl, At: at}, true, nil
}

func (s *Store) InsertEvent(e Event) error {
	_, err := s.db.Exec(`INSERT OR IGNORE INTO events(seq,id,type,payload,at) VALUES(?,?,?,?,?)`,
		e.Seq, e.ID, e.Type, e.Payload, e.At)
	return err
}

// PutResult writes a derived result with deterministic identity. The identity
// covers algorithm version, parameter fingerprint, sources and value, so the
// same computation always maps to the same row (in any database).
func (s *Store) PutResult(kind, algo, paramsFP string, sources []string, value any) (Result, bool, error) {
	sort.Strings(sources)
	val := canonical(value)
	id := hashID("result", kind, algo, paramsFP, strings.Join(sources, ","), val)
	seq := s.NowSeq()
	res, err := s.db.Exec(
		`INSERT OR IGNORE INTO results(id,kind,algo,params_fp,sources,value,created_seq) VALUES(?,?,?,?,?,?,?)`,
		id, kind, algo, paramsFP, canonical(sources), val, seq)
	if err != nil {
		return Result{}, false, err
	}
	n, _ := res.RowsAffected()
	return Result{ID: id, Kind: kind, Algo: algo, ParamsFP: paramsFP,
		Sources: sources, Value: val, CreatedSeq: seq}, n == 1, nil
}

func (s *Store) GetObject(id string) (Object, error) {
	var o Object
	err := s.db.QueryRow(
		`SELECT id,kind,version,parent_id,content,created_seq FROM objects WHERE id=?`, id).
		Scan(&o.ID, &o.Kind, &o.Version, &o.ParentID, &o.Content, &o.CreatedSeq)
	return o, err
}

// Objects returns all objects of a kind created at or before asof (<=0 means now).
func (s *Store) Objects(kind string, asof int64) ([]Object, error) {
	q := `SELECT id,kind,version,parent_id,content,created_seq FROM objects WHERE kind=?`
	args := []any{kind}
	if asof > 0 {
		q += ` AND created_seq<=?`
		args = append(args, asof)
	}
	q += ` ORDER BY version ASC, id ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanObjects(rows)
}

func (s *Store) AllObjects(asof int64) ([]Object, error) {
	q := `SELECT id,kind,version,parent_id,content,created_seq FROM objects`
	var args []any
	if asof > 0 {
		q += ` WHERE created_seq<=?`
		args = append(args, asof)
	}
	q += ` ORDER BY created_seq ASC, id ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanObjects(rows)
}

func scanObjects(rows *sql.Rows) ([]Object, error) {
	var out []Object
	for rows.Next() {
		var o Object
		if err := rows.Scan(&o.ID, &o.Kind, &o.Version, &o.ParentID, &o.Content, &o.CreatedSeq); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// LatestObject returns the newest version of a kind visible at asof.
func (s *Store) LatestObject(kind string, asof int64) (Object, bool, error) {
	objs, err := s.Objects(kind, asof)
	if err != nil || len(objs) == 0 {
		return Object{}, false, err
	}
	return objs[len(objs)-1], true, nil
}

func (s *Store) Events(asof int64) ([]Event, error) {
	q := `SELECT seq,id,type,payload,at FROM events`
	var args []any
	if asof > 0 {
		q += ` WHERE seq<=?`
		args = append(args, asof)
	}
	q += ` ORDER BY seq ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.Seq, &e.ID, &e.Type, &e.Payload, &e.At); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) GetEvent(id string) (Event, error) {
	var e Event
	err := s.db.QueryRow(`SELECT seq,id,type,payload,at FROM events WHERE id=?`, id).
		Scan(&e.Seq, &e.ID, &e.Type, &e.Payload, &e.At)
	return e, err
}

func (s *Store) Results(kind string, asof int64) ([]Result, error) {
	q := `SELECT id,kind,algo,params_fp,sources,value,created_seq FROM results`
	var args []any
	where := []string{}
	if kind != "" {
		where = append(where, "kind=?")
		args = append(args, kind)
	}
	if asof > 0 {
		where = append(where, "created_seq<=?")
		args = append(args, asof)
	}
	if len(where) > 0 {
		q += " WHERE " + strings.Join(where, " AND ")
	}
	q += ` ORDER BY created_seq ASC, id ASC`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Result
	for rows.Next() {
		var r Result
		var src string
		if err := rows.Scan(&r.ID, &r.Kind, &r.Algo, &r.ParamsFP, &src, &r.Value, &r.CreatedSeq); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(src), &r.Sources)
		out = append(out, r)
	}
	return out, rows.Err()
}

// SupersededIDs returns ids that have a descendant revision visible at asof.
// Parent chains make the ancestry explicit.
func (s *Store) SupersededIDs(asof int64) (map[string]bool, error) {
	objs, err := s.AllObjects(asof)
	if err != nil {
		return nil, err
	}
	byID := map[string]Object{}
	for _, o := range objs {
		byID[o.ID] = o
	}
	out := map[string]bool{}
	for _, o := range objs {
		pid := o.ParentID
		for pid != "" {
			out[pid] = true
			po, ok := byID[pid]
			if !ok {
				break
			}
			pid = po.ParentID
		}
	}
	return out, nil
}

// Views annotates results with staleness. A result is stale when any raw
// source it depends on has been superseded by a newer version (at asof).
func (s *Store) Views(kind string, asof int64) ([]ResultView, error) {
	rs, err := s.Results(kind, asof)
	if err != nil {
		return nil, err
	}
	sup, err := s.SupersededIDs(asof)
	if err != nil {
		return nil, err
	}
	out := make([]ResultView, 0, len(rs))
	for _, r := range rs {
		stale := false
		for _, src := range r.Sources {
			if sup[src] {
				stale = true
				break
			}
		}
		out = append(out, ResultView{Result: r, Stale: stale})
	}
	return out, nil
}
