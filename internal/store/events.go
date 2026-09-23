package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"tga-lineage/internal/domain"
)

// ErrProjectExists is returned when creating a duplicate project id.
var ErrProjectExists = errors.New("project id already exists")

type ProjectRow struct {
	ID        string
	Name      string
	CreatedAt string
}

func (s *Store) CreateProject(ctx context.Context, p ProjectRow) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects(id,name,created_at) VALUES(?,?,?)`, p.ID, p.Name, p.CreatedAt)
	if err != nil {
		if isDup(err) {
			return ErrProjectExists
		}
		return err
	}
	return nil
}

func isDup(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "UNIQUE constraint") || strings.Contains(msg, "duplicate")
}

func (s *Store) ListProjects(ctx context.Context) ([]ProjectRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,created_at FROM projects ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		var p ProjectRow
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProject(ctx context.Context, id string) (*ProjectRow, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,created_at FROM projects WHERE id=?`, id)
	var p ProjectRow
	if err := row.Scan(&p.ID, &p.Name, &p.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// AppendEvent writes event with its given sequence id (caller computed it via
// MaxEventID + 1 in the same transactional caller).
func (s *Store) AppendEvent(ctx context.Context, e domain.Event) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events(id,project_id,kind,at,payload) VALUES(?,?,?,?,?)`,
		e.ID, e.ProjectID, string(e.Kind), e.At, []byte(e.Payload))
	if err != nil {
		return fmt.Errorf("append event: %w", err)
	}
	return nil
}

func (s *Store) MaxEventID(ctx context.Context, projectID string) (int, error) {
	var max sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(id) FROM events WHERE project_id=?`, projectID).Scan(&max)
	if err != nil {
		return 0, err
	}
	if !max.Valid {
		return 0, nil
	}
	return int(max.Int64), nil
}

func (s *Store) Events(ctx context.Context, projectID string, upto int) ([]domain.Event, error) {
	q := `SELECT id,project_id,kind,at,payload FROM events WHERE project_id=?`
	args := []any{projectID}
	if upto > 0 {
		q += ` AND id<=?`
		args = append(args, upto)
	}
	q += ` ORDER BY id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var kind string
		if err := rows.Scan(&e.ID, &e.ProjectID, &kind, &e.At, &e.Payload); err != nil {
			return nil, err
		}
		e.Kind = domain.EventKind(kind)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SetKV(ctx context.Context, projectID, key, value string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO kv(project_id,key,value) VALUES(?,?,?)
		 ON CONFLICT(project_id,key) DO UPDATE SET value=excluded.value`,
		projectID, key, value)
	return err
}

func (s *Store) GetKV(ctx context.Context, projectID, key string) (string, bool, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM kv WHERE project_id=? AND key=?`, projectID, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// Snapshot returns every row needed to recreate the database elsewhere.
func (s *Store) Snapshot(ctx context.Context) (projects []ProjectRow, events []domain.Event, err error) {
	projects, err = s.ListProjects(ctx)
	if err != nil {
		return nil, nil, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id,project_id,kind,at,payload FROM events ORDER BY project_id,id`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e domain.Event
		var kind string
		if err := rows.Scan(&e.ID, &e.ProjectID, &kind, &e.At, &e.Payload); err != nil {
			return nil, nil, err
		}
		e.Kind = domain.EventKind(kind)
		events = append(events, e)
	}
	return projects, events, rows.Err()
}

func (s *Store) RestoreProject(ctx context.Context, p ProjectRow, evs []domain.Event) error {
	if err := s.CreateProject(ctx, p); err != nil {
		return err
	}
	for _, e := range evs {
		if err := s.AppendEvent(ctx, e); err != nil {
			return err
		}
	}
	return nil
}
