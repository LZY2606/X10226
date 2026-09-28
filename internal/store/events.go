package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"tgareview/internal/domain"
)

// CreateProject inserts a project and its project_created genesis event.
func (s *Store) CreateProject(ctx context.Context, p domain.Project) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO projects(id,name,created_at) VALUES(?,?,?)`,
		p.ID, p.Name, p.CreatedAt.Format(timeFmt)); err != nil {
		return err
	}
	_, err = s.appendEventTx(ctx, tx, p.ID, domain.EvProjectCreated, "system",
		"项目创建："+p.Name, map[string]any{"project_id": p.ID})
	if err != nil {
		return err
	}
	return tx.Commit()
}

// ProjectExists reports whether id is present.
func (s *Store) ProjectExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

// GetProject loads a project.
func (s *Store) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,created_at FROM projects WHERE id=?`, id)
	var p domain.Project
	var created string
	if err := row.Scan(&p.ID, &p.Name, &created); err != nil {
		return nil, err
	}
	p.CreatedAt = parseTime(created)
	return &p, nil
}

// ListProjects returns all projects newest-first.
func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,created_at FROM projects ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		var c string
		if err := rows.Scan(&p.ID, &p.Name, &c); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTime(c)
		out = append(out, p)
	}
	return out, rows.Err()
}

// AppendEvent appends to the journal inside a fresh transaction. Event IDs chain
// from the previous event, so identity/order survive export/import.
func (s *Store) AppendEvent(ctx context.Context, projectID, typ, actor, summary string, payload map[string]any) (domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback()
	ev, err := s.appendEventTx(ctx, tx, projectID, typ, actor, summary, payload)
	if err != nil {
		return domain.Event{}, err
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

func (s *Store) appendEventTx(ctx context.Context, tx *sql.Tx, projectID, typ, actor, summary string, payload map[string]any) (domain.Event, error) {
	if payload == nil {
		payload = map[string]any{}
	}
	var lastSeq sql.NullInt64
	var parentID string
	err := tx.QueryRowContext(ctx,
		`SELECT seq,id FROM events WHERE project_id=? ORDER BY seq DESC LIMIT 1`, projectID).
		Scan(&lastSeq, &parentID)
	if err == sql.ErrNoRows {
		parentID = "GENESIS"
	} else if err != nil {
		return domain.Event{}, err
	}
	seq := lastSeq.Int64 + 1
	at := s.clock.Now().UTC()
	payloadBytes := jsonRaw(payload)
	id := domain.ContentID("evt", append([]byte(fmt.Sprintf("%s|%d|%s|%s|", projectID, seq, typ, parentID)), payloadBytes...))
	ev := domain.Event{
		Seq: seq, ID: id, ParentID: parentID, ProjectID: projectID,
		Type: typ, At: at, Actor: actor, Summary: summary, Payload: payload,
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events(project_id,seq,id,parent_id,type,at,actor,summary,payload)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		projectID, seq, id, parentID, typ, at.Format(timeFmt), actor, summary, string(payloadBytes)); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

// ListEvents returns the full ordered journal.
func (s *Store) ListEvents(ctx context.Context, projectID string) ([]domain.Event, error) {
	return s.listEvents(ctx, s.db, projectID)
}

func (s *Store) listEvents(ctx context.Context, q queryer, projectID string) ([]domain.Event, error) {
	rows, err := q.QueryContext(ctx,
		`SELECT seq,id,parent_id,type,at,actor,summary,payload FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var ev domain.Event
		var at, payload string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.ParentID, &ev.Type, &at, &ev.Actor, &ev.Summary, &payload); err != nil {
			return nil, err
		}
		ev.ProjectID = projectID
		ev.At = parseTime(at)
		_ = json.Unmarshal([]byte(payload), &ev.Payload)
		out = append(out, ev)
	}
	return out, rows.Err()
}

func jsonRaw(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}
