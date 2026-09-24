// Package store is the SQLite-backed persistence layer. Events and raw
// records are append-only. Export/import copies content-addressed rows
// verbatim, so identity, ordering and provenance survive a local-database
// deletion and re-import.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"tga-review/internal/domain"
	"tga-review/internal/events"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS evlog (
  seq INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT UNIQUE NOT NULL,
  project_id TEXT NOT NULL,
  parent_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  actor TEXT NOT NULL,
  summary TEXT NOT NULL,
  undone_of TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS raw_records (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  batch_id TEXT NOT NULL,
  stratum TEXT NOT NULL,
  status TEXT NOT NULL,
  line_no INTEGER NOT NULL,
  original TEXT NOT NULL,
  diagnostic TEXT NOT NULL DEFAULT '',
  supersedes TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL,
  received_at TEXT NOT NULL,
  seq_in_batch INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS batches (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  started_at TEXT NOT NULL,
  finished_at TEXT NOT NULL,
  total INTEGER NOT NULL, accepted INTEGER NOT NULL,
  rejected INTEGER NOT NULL, staged INTEGER NOT NULL,
  half_failed INTEGER NOT NULL, diagnostic TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS derived_cache (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  body TEXT NOT NULL,
  algo_version TEXT NOT NULL,
  param_fp TEXT NOT NULL,
  direct_sources TEXT NOT NULL,
  created_event_seq INTEGER NOT NULL
);
`

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

// Project operations --------------------------------------------------------

func (s *Store) CreateProject(ctx context.Context, id, name, createdAt string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects(id,name,created_at) VALUES(?,?,?)`, id, name, createdAt)
	return err
}

func (s *Store) ListProjects(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, nil
}

// Events --------------------------------------------------------------------

// AppendEvent writes a new event; seq and id are assigned deterministically.
func (s *Store) AppendEvent(ctx context.Context, projectID string, kind events.Kind,
	actor, summary, undoneOf string, payload any, clock domain.Clock) (events.Event, error) {
	pb, err := json.Marshal(payload)
	if err != nil {
		return events.Event{}, err
	}
	parent, err := s.HeadID(ctx, projectID)
	if err != nil {
		return events.Event{}, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO evlog(project_id,parent_id,kind,occurred_at,actor,summary,undone_of,payload,seq)
		 VALUES(?,?,?,?,?,?,?,?,NULL)`,
		projectID, parent, string(kind), clock.Now().Format("2006-01-02T15:04:05.000000Z"),
		actor, summary, undoneOf, string(pb))
	if err != nil {
		return events.Event{}, err
	}
	seq64, _ := res.LastInsertId()
	id := fmt.Sprintf("e_%d_%s", seq64, domain.MustID("h", map[string]any{
		"project": projectID, "kind": kind, "payload": json.RawMessage(pb), "parent": parent,
	})[:13])
	if _, err := s.db.ExecContext(ctx, `UPDATE evlog SET id=? WHERE seq=?`, id, seq64); err != nil {
		return events.Event{}, err
	}
	return events.Event{Seq: seq64, ID: id, ProjectID: projectID, ParentID: parent,
		Kind: kind, Actor: actor, Summary: summary, UndoneOf: undoneOf, Payload: pb}, nil
}

func (s *Store) HeadID(ctx context.Context, projectID string) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx,
		`SELECT id FROM evlog WHERE project_id=? ORDER BY seq DESC LIMIT 1`, projectID).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return id, err
}

func (s *Store) Events(ctx context.Context, projectID string) ([]events.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq,id,parent_id,kind,occurred_at,actor,summary,undone_of,payload
		 FROM evlog WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []events.Event
	for rows.Next() {
		var e events.Event
		var kind string
		if err := rows.Scan(&e.Seq, &e.ID, &e.ParentID, &kind, &e.OccurredAt,
			&e.Actor, &e.Summary, &e.UndoneOf, &e.Payload); err != nil {
			return nil, err
		}
		e.Kind = events.Kind(kind)
		out = append(out, e)
	}
	return out, nil
}

// Raw records and batches ---------------------------------------------------

func (s *Store) InsertBatch(ctx context.Context, b domain.Batch) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO batches(id,project_id,kind,started_at,finished_at,
		   total,accepted,rejected,staged,half_failed,diagnostic)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.ProjectID, b.Kind, b.StartedAt, b.FinishedAt, b.Total, b.Accepted,
		b.Rejected, b.Staged, b.HalfFailed, b.Diagnostic)
	return err
}

func (s *Store) InsertRecord(ctx context.Context, r domain.RawRecord, seqInBatch int) error {
	pb, err := json.Marshal(r.Payload)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO raw_records(id,project_id,batch_id,stratum,status,line_no,
		   original,diagnostic,supersedes,payload,received_at,seq_in_batch)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ProjectID, r.BatchID, string(r.Stratum), string(r.Status), r.LineNo,
		r.Original, r.Diagnostic, r.Supersedes, string(pb), r.ReceivedAt, seqInBatch)
	return err
}

// RecordRow is a stored record with decoded payload.
type RecordRow struct {
	domain.RawRecord
}

func (s *Store) Records(ctx context.Context, projectID string) ([]RecordRow, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,batch_id,stratum,status,line_no,original,diagnostic,supersedes,payload,received_at
		 FROM raw_records WHERE project_id=? ORDER BY rowid`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RecordRow
	for rows.Next() {
		var r RecordRow
		var st, stat string
		var pb string
		if err := rows.Scan(&r.ID, &r.BatchID, &st, &stat, &r.LineNo,
			&r.Original, &r.Diagnostic, &r.Supersedes, &pb, &r.ReceivedAt); err != nil {
			return nil, err
		}
		r.Stratum = domain.Stratum(st)
		r.Status = domain.ImportStatus(stat)
		r.ProjectID = projectID
		if err := json.Unmarshal([]byte(pb), &r.Payload); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// DecodeRecord rehydrates the typed payload (Payload is decoded as
// map[string]any by Records; typed accessors below are used by the app).
func DecodePayload[T any](r RecordRow) (T, error) {
	var v T
	b, err := json.Marshal(r.Payload)
	if err != nil {
		return v, err
	}
	err = json.Unmarshal(b, &v)
	return v, err
}

func (s *Store) Batches(ctx context.Context, projectID string) ([]domain.Batch, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,kind,started_at,finished_at,total,accepted,rejected,staged,half_failed,diagnostic
		 FROM batches WHERE project_id=? ORDER BY started_at`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Batch
	for rows.Next() {
		var b domain.Batch
		var hf int
		if err := rows.Scan(&b.ID, &b.Kind, &b.StartedAt, &b.FinishedAt, &b.Total,
			&b.Accepted, &b.Rejected, &b.Staged, &hf, &b.Diagnostic); err != nil {
			return nil, err
		}
		b.ProjectID = projectID
		b.HalfFailed = hf != 0
		out = append(out, b)
	}
	return out, nil
}

// Derived cache -------------------------------------------------------------

func (s *Store) SaveDerived(ctx context.Context, id, projectID, kind string, body any,
	algo, fp string, sources []string, eventSeq int64) error {
	bb, err := json.Marshal(body)
	if err != nil {
		return err
	}
	sb, _ := json.Marshal(sources)
	_, err = s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO derived_cache(id,project_id,kind,body,algo_version,param_fp,direct_sources,created_event_seq)
		 VALUES(?,?,?,?,?,?,?,?)`,
		id, projectID, kind, string(bb), algo, fp, string(sb), eventSeq)
	return err
}

// Export / import -----------------------------------------------------------

// Bundle is the portable project archive.
type Bundle struct {
	Version  int               `json:"version"`
	Project  ExportProject     `json:"project"`
	Events   []events.Event    `json:"events"`
	Records  []domain.RawRecord `json:"records"`
	Batches  []domain.Batch    `json:"batches"`
}

type ExportProject struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) Export(ctx context.Context, projectID string) (Bundle, error) {
	var b Bundle
	b.Version = 1
	var name, created string
	if err := s.db.QueryRowContext(ctx,
		`SELECT id,name,created_at FROM projects WHERE id=?`, projectID).Scan(
		&b.Project.ID, &name, &created); err != nil {
		return b, err
	}
	b.Project.Name = name
	b.Project.CreatedAt = created
	evs, err := s.Events(ctx, projectID)
	if err != nil {
		return b, err
	}
	b.Events = evs
	recs, err := s.Records(ctx, projectID)
	if err != nil {
		return b, err
	}
	for _, r := range recs {
		b.Records = append(b.Records, r.RawRecord)
	}
	b.Batches, err = s.Batches(ctx, projectID)
	if err != nil {
		return b, err
	}
	return b, nil
}

func (s *Store) Import(ctx context.Context, b Bundle) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects(id,name,created_at) VALUES(?,?,?)`,
		b.Project.ID, b.Project.Name, b.Project.CreatedAt); err != nil {
		return err
	}
	for _, e := range b.Events {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO evlog(seq,id,project_id,parent_id,kind,occurred_at,actor,summary,undone_of,payload)
			 VALUES(?,?,?,?,?,?,?,?,?,?)`,
			e.Seq, e.ID, e.ProjectID, e.ParentID, string(e.Kind), e.OccurredAt,
			e.Actor, e.Summary, e.UndoneOf, string(e.Payload)); err != nil {
			return err
		}
	}
	for _, ba := range b.Batches {
		hf := 0
		if ba.HalfFailed {
			hf = 1
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO batches(id,project_id,kind,started_at,finished_at,
			   total,accepted,rejected,staged,half_failed,diagnostic)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
			ba.ID, ba.ProjectID, ba.Kind, ba.StartedAt, ba.FinishedAt, ba.Total,
			ba.Accepted, ba.Rejected, ba.Staged, hf, ba.Diagnostic); err != nil {
			return err
		}
	}
	for i, r := range b.Records {
		pb, err := json.Marshal(r.Payload)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO raw_records(id,project_id,batch_id,stratum,status,line_no,
			   original,diagnostic,supersedes,payload,received_at,seq_in_batch)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			r.ID, r.ProjectID, r.BatchID, string(r.Stratum), string(r.Status), r.LineNo,
			r.Original, r.Diagnostic, r.Supersedes, string(pb), r.ReceivedAt, i); err != nil {
			return err
		}
	}
	return tx.Commit()
}
