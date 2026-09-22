package store

import (
	"context"

	"tga-review/internal/model"
)

// CreateProject inserts a new project.
func (s *Store) CreateProject(ctx context.Context, p model.Project) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO projects(id,name,created_at) VALUES(?,?,?)`,
		p.ID, p.Name, p.CreatedAt.UTC().Format(timeFmt))
	return err
}

// GetProject loads one project.
func (s *Store) GetProject(ctx context.Context, id string) (model.Project, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id,name,created_at FROM projects WHERE id=?`, id)
	var p model.Project
	var ts string
	if err := row.Scan(&p.ID, &p.Name, &ts); err != nil {
		return p, err
	}
	p.CreatedAt = parseTime(ts)
	return p, nil
}

// ListProjects returns all projects newest first.
func (s *Store) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,name,created_at FROM projects ORDER BY created_at DESC, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		var p model.Project
		var ts string
		if err := rows.Scan(&p.ID, &p.Name, &ts); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTime(ts)
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateBatch inserts an import batch.
func (s *Store) CreateBatch(ctx context.Context, b model.Batch) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO batches(id,project_id,created_at,note,status) VALUES(?,?,?,?,?)`,
		b.ID, b.ProjectID, b.CreatedAt.UTC().Format(timeFmt), b.Note, b.Status)
	return err
}

// GetBatch loads a batch.
func (s *Store) GetBatch(ctx context.Context, id string) (model.Batch, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id,project_id,created_at,note,status FROM batches WHERE id=?`, id)
	var b model.Batch
	var ts string
	err := row.Scan(&b.ID, &b.ProjectID, &ts, &b.Note, &b.Status)
	if err == nil {
		b.CreatedAt = parseTime(ts)
	}
	return b, err
}
