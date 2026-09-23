package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"tga-review/internal/domain"
)

var ErrNotFound = errors.New("not found")

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// NextEventSeq returns the next ledger sequence for a project.
func (s *Store) NextEventSeq(ctx context.Context, tx *sql.Tx, projectID string) (int64, error) {
	var max sql.NullInt64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq),0) FROM events WHERE project_id=?`, projectID).
		Scan(&max); err != nil {
		return 0, err
	}
	return max.Int64 + 1, nil
}

// AppendEvent writes one ledger entry and returns the stored event.
func (s *Store) AppendEvent(ctx context.Context, tx *sql.Tx, projectID, typ string, atStep int64, parentID string, payload any) (domain.Event, error) {
	seq, err := s.NextEventSeq(ctx, tx, projectID)
	if err != nil {
		return domain.Event{}, err
	}
	ev := domain.Event{
		ID: fmt.Sprintf("%s/E%d", projectID, seq), ProjectID: projectID, Seq: seq,
		Type: typ, AtStep: atStep, ParentID: parentID, Payload: mustJSON(payload),
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events(id,project_id,seq,type,at_step,parent_id,payload) VALUES(?,?,?,?,?,?,?)`,
		ev.ID, ev.ProjectID, ev.Seq, ev.Type, ev.AtStep, ev.ParentID, string(ev.Payload)); err != nil {
		return domain.Event{}, err
	}
	return ev, nil
}

func scanEvent(row interface {
	Scan(...any) error
}) (domain.Event, error) {
	var ev domain.Event
	var payload, parent sql.NullString
	if err := row.Scan(&ev.ID, &ev.ProjectID, &ev.Seq, &ev.Type, &ev.AtStep, &parent, &payload); err != nil {
		return ev, err
	}
	if parent.Valid {
		ev.ParentID = parent.String
	}
	ev.Payload = json.RawMessage(payload.String)
	return ev, nil
}

const eventCols = `id,project_id,seq,type,at_step,parent_id,payload`

// EventsAfter returns events with seq > afterSeq, in order.
func (s *Store) EventsAfter(ctx context.Context, projectID string, afterSeq int64) ([]domain.Event, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+eventCols+` FROM events WHERE project_id=? AND seq>? ORDER BY seq`, projectID, afterSeq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// EventsUntil returns events up to and including seq (as-of reconstruction).
func (s *Store) EventsUntil(ctx context.Context, projectID string, seq int64) ([]domain.Event, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+eventCols+` FROM events WHERE project_id=? AND seq<=? ORDER BY seq`, projectID, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// AllEvents returns the full ledger for a project.
func (s *Store) AllEvents(ctx context.Context, projectID string) ([]domain.Event, error) {
	return s.EventsAfter(ctx, projectID, 0)
}

func scanEvents(rows *sql.Rows) ([]domain.Event, error) {
	var out []domain.Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) Event(ctx context.Context, id string) (domain.Event, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+eventCols+` FROM events WHERE id=?`, id)
	ev, err := scanEvent(row)
	if errors.Is(err, sql.ErrNoRows) {
		return ev, ErrNotFound
	}
	return ev, err
}

// ProjectExists creates the project row (idempotent within a tx).
func (s *Store) CreateProject(ctx context.Context, tx *sql.Tx, p domain.Project) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO projects(id,name,created_seq,created_at) VALUES(?,?,?,?)
		 ON CONFLICT(id) DO NOTHING`, p.ID, p.Name, p.CreatedSeq, p.CreatedAt)
	return err
}

func (s *Store) Project(ctx context.Context, id string) (domain.Project, error) {
	var p domain.Project
	row := s.DB.QueryRowContext(ctx,
		`SELECT id,name,created_seq,created_at FROM projects WHERE id=?`, id)
	err := row.Scan(&p.ID, &p.Name, &p.CreatedSeq, &p.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id,name,created_seq,created_at FROM projects ORDER BY created_seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedSeq, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
