// Package store persists the event log and its projections in SQLite.
// The events table is the source of truth; other tables are
// projections carrying the sequence numbers needed for time travel.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"tga-review/internal/model"
)

const schema = `
PRAGMA journal_mode=WAL;
CREATE TABLE IF NOT EXISTS projects(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_seq INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS events(
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT UNIQUE NOT NULL,
  project_id TEXT NOT NULL,
  vtime INTEGER NOT NULL,
  kind TEXT NOT NULL,
  actor TEXT NOT NULL,
  payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, seq);
CREATE TABLE IF NOT EXISTS batches(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  status TEXT NOT NULL,
  seq INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS imports(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  layer TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_id TEXT NOT NULL,
  batch_id TEXT NOT NULL,
  raw TEXT NOT NULL,
  format TEXT NOT NULL,
  status TEXT NOT NULL,
  diagnostics TEXT NOT NULL,
  seq INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_imports_project ON imports(project_id, layer, version);
CREATE TABLE IF NOT EXISTS derived(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  algo_version TEXT NOT NULL,
  param_fp TEXT NOT NULL,
  source_ids TEXT NOT NULL,
  status TEXT NOT NULL,
  payload TEXT NOT NULL,
  created_seq INTEGER NOT NULL,
  stale_seq INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_derived_project ON derived(project_id, kind);
CREATE TABLE IF NOT EXISTS decisions(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  target_id TEXT NOT NULL,
  verdict TEXT NOT NULL,
  note TEXT NOT NULL,
  actor TEXT NOT NULL,
  seq INTEGER NOT NULL,
  undone_seq INTEGER NOT NULL DEFAULT 0,
  inactive_seq INTEGER NOT NULL DEFAULT 0,
  conflict INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS params(
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  params TEXT NOT NULL,
  seq INTEGER NOT NULL
);
`

// Store wraps the SQLite handle.
type Store struct {
	db *sql.DB
}

// Open opens (creating if needed) the database at path.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// NextSeq returns the sequence number the next event will receive.
// Must be called inside the mutation's transaction scope by callers
// that allocate IDs from it; here it is a plain read.
func (s *Store) NextSeq() (int64, error) {
	var n int64
	err := s.db.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM events`).Scan(&n)
	return n, err
}

// AppendEvent inserts an event. If ev.Seq > 0 it is preserved
// (used by import-restore); otherwise the row is auto-assigned and
// the assigned sequence is written back to ev.Seq.
func (s *Store) AppendEvent(ev *model.Event) error {
	if ev.Seq > 0 {
		_, err := s.db.Exec(`INSERT INTO events(seq,id,project_id,vtime,kind,actor,payload) VALUES(?,?,?,?,?,?,?)`,
			ev.Seq, ev.ID, ev.ProjectID, ev.VTime, ev.Kind, ev.Actor, ev.Payload)
		return err
	}
	res, err := s.db.Exec(`INSERT INTO events(id,project_id,vtime,kind,actor,payload) VALUES(?,?,?,?,?,?)`,
		ev.ID, ev.ProjectID, ev.VTime, ev.Kind, ev.Actor, ev.Payload)
	if err != nil {
		return err
	}
	ev.Seq, err = res.LastInsertId()
	return err
}

// Events returns project events with seq <= upto (0 = all), ordered.
func (s *Store) Events(projectID string, upto int64) ([]model.Event, error) {
	q := `SELECT seq,id,project_id,vtime,kind,actor,payload FROM events WHERE project_id=?`
	args := []any{projectID}
	if upto > 0 {
		q += ` AND seq<=?`
		args = append(args, upto)
	}
	q += ` ORDER BY seq`
	rows, err := s.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Event
	for rows.Next() {
		var e model.Event
		if err := rows.Scan(&e.Seq, &e.ID, &e.ProjectID, &e.VTime, &e.Kind, &e.Actor, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) CreateProject(p model.Project) error {
	_, err := s.db.Exec(`INSERT INTO projects(id,name,created_seq) VALUES(?,?,?)`, p.ID, p.Name, p.CreatedSeq)
	return err
}

func (s *Store) Projects() ([]model.Project, error) {
	rows, err := s.db.Query(`SELECT id,name,created_seq FROM projects ORDER BY created_seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		var p model.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedSeq); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Project(id string) (model.Project, error) {
	var p model.Project
	err := s.db.QueryRow(`SELECT id,name,created_seq FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.CreatedSeq)
	return p, err
}

func (s *Store) InsertBatch(b model.Batch) error {
	_, err := s.db.Exec(`INSERT INTO batches(id,project_id,status,seq) VALUES(?,?,?,?)`,
		b.ID, b.ProjectID, b.Status, b.Seq)
	return err
}

func (s *Store) UpdateBatchStatus(id, status string) error {
	_, err := s.db.Exec(`UPDATE batches SET status=? WHERE id=?`, status, id)
	return err
}

func (s *Store) InsertImport(v model.ImportVersion) error {
	_, err := s.db.Exec(`INSERT INTO imports(id,project_id,layer,version,parent_id,batch_id,raw,format,status,diagnostics,seq)
	  VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		v.ID, v.ProjectID, v.Layer, v.Version, v.ParentID, v.BatchID, v.Raw, v.Format, v.Status, v.Diagnostics, v.Seq)
	return err
}

// Imports returns all import versions of a project, ordered.
func (s *Store) Imports(projectID string) ([]model.ImportVersion, error) {
	rows, err := s.db.Query(`SELECT id,project_id,layer,version,parent_id,batch_id,raw,format,status,diagnostics,seq
	  FROM imports WHERE project_id=? ORDER BY layer, version`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ImportVersion
	for rows.Next() {
		var v model.ImportVersion
		if err := rows.Scan(&v.ID, &v.ProjectID, &v.Layer, &v.Version, &v.ParentID, &v.BatchID,
			&v.Raw, &v.Format, &v.Status, &v.Diagnostics, &v.Seq); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// LayerVersions returns all versions of one layer ordered by version.
func (s *Store) LayerVersions(projectID, layer string) ([]model.ImportVersion, error) {
	all, err := s.Imports(projectID)
	if err != nil {
		return nil, err
	}
	var out []model.ImportVersion
	for _, v := range all {
		if v.Layer == layer {
			out = append(out, v)
		}
	}
	return out, nil
}

func (s *Store) InsertDerived(d model.Derived) error {
	_, err := s.db.Exec(`INSERT INTO derived(id,project_id,kind,algo_version,param_fp,source_ids,status,payload,created_seq,stale_seq)
	  VALUES(?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.Kind, d.AlgoVersion, d.ParamFP, d.SourceIDs, d.Status, d.Payload, d.CreatedSeq, d.StaleSeq)
	return err
}

func (s *Store) MarkDerivedStale(id string, staleSeq int64) error {
	_, err := s.db.Exec(`UPDATE derived SET status=?, stale_seq=? WHERE id=?`, model.StatusStale, staleSeq, id)
	return err
}

// DerivedAll returns all derived objects of a project.
func (s *Store) DerivedAll(projectID string) ([]model.Derived, error) {
	rows, err := s.db.Query(`SELECT id,project_id,kind,algo_version,param_fp,source_ids,status,payload,created_seq,stale_seq
	  FROM derived WHERE project_id=? ORDER BY created_seq, id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Derived
	for rows.Next() {
		var d model.Derived
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.Kind, &d.AlgoVersion, &d.ParamFP, &d.SourceIDs,
			&d.Status, &d.Payload, &d.CreatedSeq, &d.StaleSeq); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) InsertDecision(d model.Decision) error {
	_, err := s.db.Exec(`INSERT INTO decisions(id,project_id,target_id,verdict,note,actor,seq,undone_seq,inactive_seq,conflict)
	  VALUES(?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.TargetID, d.Verdict, d.Note, d.Actor, d.Seq, d.UndoneSeq, d.InactiveSeq, d.Conflict)
	return err
}

func (s *Store) UpdateDecision(d model.Decision) error {
	_, err := s.db.Exec(`UPDATE decisions SET undone_seq=?, inactive_seq=?, conflict=? WHERE id=?`,
		d.UndoneSeq, d.InactiveSeq, d.Conflict, d.ID)
	return err
}

func (s *Store) Decisions(projectID string) ([]model.Decision, error) {
	rows, err := s.db.Query(`SELECT id,project_id,target_id,verdict,note,actor,seq,undone_seq,inactive_seq,conflict
	  FROM decisions WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Decision
	for rows.Next() {
		var d model.Decision
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.TargetID, &d.Verdict, &d.Note, &d.Actor,
			&d.Seq, &d.UndoneSeq, &d.InactiveSeq, &d.Conflict); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) InsertParams(p model.ParamsVersion) error {
	_, err := s.db.Exec(`INSERT INTO params(id,project_id,params,seq) VALUES(?,?,?,?)`,
		p.ID, p.ProjectID, p.Params, p.Seq)
	return err
}

func (s *Store) ParamsAll(projectID string) ([]model.ParamsVersion, error) {
	rows, err := s.db.Query(`SELECT id,project_id,params,seq FROM params WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.ParamsVersion
	for rows.Next() {
		var p model.ParamsVersion
		if err := rows.Scan(&p.ID, &p.ProjectID, &p.Params, &p.Seq); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Batches returns all import batches of a project.
func (s *Store) Batches(projectID string) ([]model.Batch, error) {
	rows, err := s.db.Query(`SELECT id,project_id,status,seq FROM batches WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Batch
	for rows.Next() {
		var b model.Batch
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.Status, &b.Seq); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}
