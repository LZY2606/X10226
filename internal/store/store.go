package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"tga-review/internal/domain"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
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
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  title TEXT NOT NULL,
  case_id TEXT NOT NULL DEFAULT '',
  created_at INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  project_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  id TEXT NOT NULL,
  type TEXT NOT NULL,
  actor TEXT NOT NULL,
  at INTEGER NOT NULL,
  payload_json TEXT NOT NULL,
  payload_hash TEXT NOT NULL,
  PRIMARY KEY(project_id, seq),
  UNIQUE(project_id, id)
);
CREATE INDEX IF NOT EXISTS events_project_seq ON events(project_id, seq);
`)
	return err
}

func (s *Store) InTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateProject(ctx context.Context, id, title, caseID string, at int64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO projects(id,title,case_id,created_at) VALUES(?,?,?,?)`, id, title, caseID, at)
	return err
}

func (s *Store) ProjectExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

func (s *Store) ProjectMeta(ctx context.Context, id string) (title, caseID string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT title, case_id FROM projects WHERE id=?`, id).Scan(&title, &caseID)
	return
}

func (s *Store) ListProjects(ctx context.Context) ([]map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,title,case_id FROM projects ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]string
	for rows.Next() {
		var id, title, caseID string
		if err := rows.Scan(&id, &title, &caseID); err != nil {
			return nil, err
		}
		out = append(out, map[string]string{"id": id, "title": title, "caseId": caseID})
	}
	return out, rows.Err()
}

func (s *Store) AppendEventTx(tx *sql.Tx, env domain.EventEnvelope) error {
	_, err := tx.Exec(`INSERT INTO events(project_id,seq,id,type,actor,at,payload_json,payload_hash) VALUES(?,?,?,?,?,?,?,?)`,
		env.ProjectID, env.Seq, env.ID, env.Type, env.Actor, env.At, env.PayloadJSON, env.PayloadHash)
	return err
}

func (s *Store) AppendEvent(ctx context.Context, env domain.EventEnvelope) error {
	return s.InTx(ctx, func(tx *sql.Tx) error { return s.AppendEventTx(tx, env) })
}

func (s *Store) NextSeqTx(ctx context.Context, tx *sql.Tx, projectID string) (int64, error) {
	var max sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(seq) FROM events WHERE project_id=?`, projectID).Scan(&max); err != nil {
		return 0, err
	}
	return max.Int64 + 1, nil
}

func (s *Store) Events(ctx context.Context, projectID string) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq,id,type,actor,at,payload_json FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var ev domain.Event
		var payload string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.Type, &ev.Actor, &ev.At, &payload); err != nil {
			return nil, err
		}
		decoded, err := domain.DecodePayload(ev.Type, payload)
		if err != nil {
			return nil, err
		}
		ev.Payload = decoded
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) State(ctx context.Context, projectID string) (*domain.ProjectState, error) {
	events, err := s.Events(ctx, projectID)
	if err != nil {
		return nil, err
	}
	return domain.Replay(events)
}

type ExportProject struct {
	Schema  string                 `json:"schema"`
	Project ExportMeta             `json:"project"`
	Events  []domain.EventEnvelope `json:"events"`
}

type ExportMeta struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	CaseID string `json:"caseId"`
}

func (s *Store) Export(ctx context.Context, projectID string) (ExportProject, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq,id,type,actor,at,payload_json,payload_hash FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return ExportProject{}, err
	}
	defer rows.Close()
	title, caseID, err := s.ProjectMeta(ctx, projectID)
	if err != nil {
		return ExportProject{}, err
	}
	out := ExportProject{Schema: "tga-review-export/v1", Project: ExportMeta{ID: projectID, Title: title, CaseID: caseID}}
	for rows.Next() {
		var env domain.EventEnvelope
		env.ProjectID = projectID
		if err := rows.Scan(&env.Seq, &env.ID, &env.Type, &env.Actor, &env.At, &env.PayloadJSON, &env.PayloadHash); err != nil {
			return ExportProject{}, err
		}
		out.Events = append(out.Events, env)
	}
	return out, rows.Err()
}

func (s *Store) Import(ctx context.Context, p ExportProject, replace bool) (int, error) {
	if len(p.Events) == 0 {
		return 0, fmt.Errorf("import contains no events")
	}
	exists, err := s.ProjectExists(ctx, p.Project.ID)
	if err != nil {
		return 0, err
	}
	if exists && !replace {
		return 0, fmt.Errorf("project %s already exists", p.Project.ID)
	}
	err = s.InTx(ctx, func(tx *sql.Tx) error {
		if exists {
			if _, err := tx.Exec(`DELETE FROM events WHERE project_id=?`, p.Project.ID); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM projects WHERE id=?`, p.Project.ID); err != nil {
				return err
			}
		}
		var createdAt int64
		if len(p.Events) > 0 {
			createdAt = p.Events[0].At
		}
		if _, err := tx.Exec(`INSERT INTO projects(id,title,case_id,created_at) VALUES(?,?,?,?)`, p.Project.ID, p.Project.Title, p.Project.CaseID, createdAt); err != nil {
			return err
		}
		for _, env := range p.Events {
			if env.ProjectID != p.Project.ID {
				env.ProjectID = p.Project.ID
			}
			var payload any
			decoded, err := domain.DecodePayload(env.Type, env.PayloadJSON)
			if err != nil {
				return err
			}
			payload = decoded
			b, _ := json.Marshal(payload)
			env.PayloadJSON = string(b)
			if err := s.AppendEventTx(tx, env); err != nil {
				return err
			}
		}
		return nil
	})
	return len(p.Events), err
}
