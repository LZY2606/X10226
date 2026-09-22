// Package store is the SQLite-backed, append-only event log. It is the only
// durable state of the system: deleting the database and importing an export
// reconstructs every project, identity, ordering and provenance edge.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"tga-lineage/internal/model"
)

type Store struct{ db *sql.DB }

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, err
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS projects(
  uid TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL,
  seq INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS events(
  project TEXT NOT NULL REFERENCES projects(uid),
  seq INTEGER NOT NULL,
  uid TEXT NOT NULL,
  kind TEXT NOT NULL,
  at TEXT NOT NULL,
  parent TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '',
  payload_type TEXT NOT NULL,
  payload TEXT NOT NULL,
  PRIMARY KEY(project, seq),
  UNIQUE(project, uid)
);
CREATE INDEX IF NOT EXISTS events_by_project ON events(project, seq);
CREATE TABLE IF NOT EXISTS kv(
  project TEXT NOT NULL, k TEXT NOT NULL, v TEXT NOT NULL,
  PRIMARY KEY(project, k)
);
`

// Append writes one event under a transaction, assigning the next dense seq.
func (s *Store) Append(ctx context.Context, project, kind, parent, note string, payload any) (model.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return model.Event{}, err
	}
	defer tx.Rollback()

	var seq int64
	if err := tx.QueryRowContext(ctx, `SELECT seq+1 FROM projects WHERE uid=?`, project).Scan(&seq); err != nil {
		return model.Event{}, fmt.Errorf("project %s: %w", project, err)
	}
	ptype, raw, err := encodePayload(payload)
	if err != nil {
		return model.Event{}, err
	}
	uid := eventUID(project, seq, kind, raw)
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events(project,seq,uid,kind,at,parent,note,payload_type,payload)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		project, seq, uid, kind, "", parent, note, ptype, raw); err != nil {
		return model.Event{}, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET seq=? WHERE uid=?`, seq, project); err != nil {
		return model.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return model.Event{}, err
	}
	ev := model.Event{Seq: seq, UID: uid, Project: project, Kind: kind,
		Parent: parent, Note: note, Payload: payload, PayloadType: ptype}
	return ev, nil
}

// AppendAt is Append with an explicit timestamp (virtual clock).
func (s *Store) AppendAt(ctx context.Context, project, kind, parent, at, note string, payload any) (model.Event, error) {
	ev, err := s.Append(ctx, project, kind, parent, note, payload)
	if err != nil {
		return ev, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE events SET at=? WHERE project=? AND seq=?`, at, project, ev.Seq); err != nil {
		return ev, err
	}
	ev.At = at
	return ev, nil
}

func (s *Store) CreateProject(ctx context.Context, info model.ProjectInfo) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects(uid,name,created_at,seq) VALUES(?,?,?,0)`,
		info.UID, info.Name, info.CreatedAt)
	return err
}

type ProjectRow struct {
	UID       string
	Name      string
	CreatedAt string
	Seq       int64
}

func (s *Store) ListProjects(ctx context.Context) ([]ProjectRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT uid,name,created_at,seq FROM projects ORDER BY created_at, uid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		var r ProjectRow
		if err := rows.Scan(&r.UID, &r.Name, &r.CreatedAt, &r.Seq); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) ProjectExists(ctx context.Context, uid string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT 1 FROM projects WHERE uid=?`, uid).Scan(&n)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}

// Stream replys events in order; stop returns true to end early.
func (s *Store) Stream(ctx context.Context, project string, until int64, fn func(model.Event) error) error {
	q := `SELECT seq,uid,kind,at,parent,note,payload_type,payload FROM events WHERE project=?`
	args := []any{project}
	if until > 0 {
		q += ` AND seq<=?`
		args = append(args, until)
	}
	q += ` ORDER BY seq`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var ev model.Event
		var raw string
		if err := rows.Scan(&ev.Seq, &ev.UID, &ev.Kind, &ev.At, &ev.Parent,
			&ev.Note, &ev.PayloadType, &raw); err != nil {
			return err
		}
		ev.Project = project
		ev.Payload, err = decodePayload(ev.PayloadType, raw)
		if err != nil {
			return err
		}
		if err := fn(ev); err != nil {
			return err
		}
	}
	return rows.Err()
}

func (s *Store) MaxSeq(ctx context.Context, project string) (int64, error) {
	var seq sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT seq FROM projects WHERE uid=?`, project).Scan(&seq)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return seq.Int64, nil
}

func encodePayload(p any) (string, string, error) {
	switch p.(type) {
	case model.ProjectInfo:
	case model.LayerPayload:
	case model.ParamsPayload:
	case model.DerivedPayload:
	case model.DecisionPayload:
	case model.JudgmentPayload:
	case model.CounterPayload:
	case model.NotePayload:
	default:
		return "", "", fmt.Errorf("unknown payload type %T", p)
	}
	b, err := json.Marshal(p)
	return payloadName(p), string(b), err
}

func payloadName(p any) string {
	switch p.(type) {
	case model.ProjectInfo:
		return "project"
	case model.LayerPayload:
		return "layer"
	case model.ParamsPayload:
		return "params"
	case model.DerivedPayload:
		return "derived"
	case model.DecisionPayload:
		return "decision"
	case model.JudgmentPayload:
		return "judgment"
	case model.CounterPayload:
		return "counter"
	case model.NotePayload:
		return "note"
	}
	return ""
}

func decodePayload(ptype, raw string) (any, error) {
	switch ptype {
	case "project":
		var v model.ProjectInfo
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		return v, nil
	case "layer":
		var v model.LayerPayload
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		return v, nil
	case "params":
		var v model.ParamsPayload
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		return v, nil
	case "derived":
		var v model.DerivedPayload
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		return v, nil
	case "decision":
		var v model.DecisionPayload
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		return v, nil
	case "judgment":
		var v model.JudgmentPayload
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		return v, nil
	case "counter":
		var v model.CounterPayload
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		return v, nil
	case "note":
		var v model.NotePayload
		if err := json.Unmarshal([]byte(raw), &v); err != nil {
			return nil, err
		}
		return v, nil
	default:
		return nil, fmt.Errorf("unknown payload type %q", ptype)
	}
}

// Export is the identity-preserving archive: raw event rows plus project
// header, in original order.
type Export struct {
	Format   string        `json:"format"`
	Projects []ProjectRow  `json:"projects"`
	Events   []StoredEvent `json:"events"`
}

type StoredEvent struct {
	Project     string `json:"project"`
	Seq         int64  `json:"seq"`
	UID         string `json:"uid"`
	Kind        string `json:"kind"`
	At          string `json:"at"`
	Parent      string `json:"parent"`
	Note        string `json:"note"`
	PayloadType string `json:"payload_type"`
	Payload     string `json:"payload"`
}

func (s *Store) ExportAll(ctx context.Context) (Export, error) {
	out := Export{Format: "tga-lineage-export-1"}
	projs, err := s.ListProjects(ctx)
	if err != nil {
		return out, err
	}
	out.Projects = projs
	for _, pr := range projs {
		err := s.Stream(ctx, pr.UID, 0, func(ev model.Event) error {
			raw, err := json.Marshal(ev.Payload)
			if err != nil {
				return err
			}
			out.Events = append(out.Events, StoredEvent{
				Project: pr.UID,
				Seq:     ev.Seq, UID: ev.UID, Kind: ev.Kind, At: ev.At,
				Parent: ev.Parent, Note: ev.Note,
				PayloadType: ev.PayloadType, Payload: string(raw),
			})
			return nil
		})
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

// ImportAll rebuilds projects and raw event rows. Existing uid conflicts are
// fatal: a re-import of a previously imported archive is reported instead of
// being merged, which keeps provenance unambiguous.
func (s *Store) ImportAll(ctx context.Context, ex Export) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range ex.Projects {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO projects(uid,name,created_at,seq) VALUES(?,?,?,?)`,
			p.UID, p.Name, p.CreatedAt, 0); err != nil {
			return fmt.Errorf("project %s: %w", p.UID, err)
		}
	}
	maxSeq := map[string]int64{}
	for _, e := range ex.Events {
		pid := e.Project
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events(project,seq,uid,kind,at,parent,note,payload_type,payload)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			pid, e.Seq, e.UID, e.Kind, e.At, e.Parent, e.Note,
			e.PayloadType, e.Payload); err != nil {
			return err
		}
		if e.Seq > maxSeq[pid] {
			maxSeq[pid] = e.Seq
		}
	}
	for p, m := range maxSeq {
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET seq=? WHERE uid=?`, m, p); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// CloneEvents copies the raw event rows of src into dst preserving seq, uid
// and order, so a counterexample branch starts as a bit-identical fork.
func (s *Store) CloneEvents(ctx context.Context, src, dst string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx,
		`SELECT seq,uid,kind,at,parent,note,payload_type,payload FROM events
		 WHERE project=? ORDER BY seq`, src)
	if err != nil {
		return err
	}
	type row struct {
		seq                                         int64
		uid, kind, at, parent, note, ptype, payload string
	}
	var rs []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.seq, &r.uid, &r.kind, &r.at, &r.parent, &r.note,
			&r.ptype, &r.payload); err != nil {
			rows.Close()
			return err
		}
		rs = append(rs, r)
	}
	rows.Close()
	var maxSeq int64
	for _, r := range rs {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events(project,seq,uid,kind,at,parent,note,payload_type,payload)
			 VALUES(?,?,?,?,?,?,?,?,?)`,
			dst, r.seq, r.uid, r.kind, r.at, r.parent, r.note, r.ptype, r.payload); err != nil {
			return err
		}
		if r.seq > maxSeq {
			maxSeq = r.seq
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET seq=? WHERE uid=?`, maxSeq, dst); err != nil {
		return err
	}
	return tx.Commit()
}
