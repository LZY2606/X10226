// Package store persists the append-only event log in SQLite.
//
// Only projects and events are stored: every derived state is reconstructed by
// deterministic replay, so deleting the database and importing an archive
// restores identities, ordering and provenance exactly.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"

	"tga/internal/model"
)

// Store is an event-log store backed by a local SQLite file.
type Store struct {
	db *sql.DB
}

// Open opens (creating the schema if needed) the database at dsn.
func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  project_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  id TEXT NOT NULL,
  type TEXT NOT NULL,
  prev_id TEXT NOT NULL,
  payload BLOB NOT NULL,
  created_at TEXT NOT NULL,
  PRIMARY KEY (project_id, seq)
);
CREATE INDEX IF NOT EXISTS events_project ON events(project_id, seq);
`)
	return err
}

// ErrNoProject is returned when a project id is unknown.
var ErrNoProject = errors.New("project not found")

// CreateProject inserts a project row.
func (s *Store) CreateProject(ctx context.Context, p model.Project) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects(id, name, created_at) VALUES(?,?,?)`,
		p.ID, p.Name, p.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"))
	if err != nil {
		return fmt.Errorf("create project: %w", err)
	}
	return nil
}

// ListProjects lists all projects ordered by creation time.
func (s *Store) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, created_at FROM projects ORDER BY created_at, id`)
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
		p.CreatedAt = parseTS(ts)
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProject loads one project.
func (s *Store) GetProject(ctx context.Context, id string) (*model.Project, error) {
	var p model.Project
	var ts string
	err := s.db.QueryRowContext(ctx, `SELECT id,name,created_at FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNoProject
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt = parseTS(ts)
	return &p, nil
}

// AppendEvent inserts one event at its fixed seq; the log is append-only.
func (s *Store) AppendEvent(ctx context.Context, ev model.Event) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO events(project_id, seq, id, type, prev_id, payload, created_at)
         VALUES(?,?,?,?,?,?,?)`,
		ev.ProjectID, ev.Seq, ev.ID, ev.Type, ev.PrevID, ev.Payload,
		ev.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z"))
	if err != nil {
		return fmt.Errorf("append event %d: %w", ev.Seq, err)
	}
	return nil
}

// Events returns a project's events in seq order.
func (s *Store) Events(ctx context.Context, projectID string) ([]*model.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq, id, type, prev_id, payload, created_at FROM events
         WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*model.Event
	for rows.Next() {
		ev := &model.Event{ProjectID: projectID}
		var ts string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.Type, &ev.PrevID, &ev.Payload, &ts); err != nil {
			return nil, err
		}
		ev.CreatedAt = parseTS(ts)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// LastEvent returns the newest event of a project or nil.
func (s *Store) LastEvent(ctx context.Context, projectID string) (*model.Event, error) {
	ev := &model.Event{ProjectID: projectID}
	var ts string
	err := s.db.QueryRowContext(ctx,
		`SELECT seq, id, type, prev_id, payload, created_at FROM events
         WHERE project_id=? ORDER BY seq DESC LIMIT 1`, projectID).
		Scan(&ev.Seq, &ev.ID, &ev.Type, &ev.PrevID, &ev.Payload, &ts)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ev.CreatedAt = parseTS(ts)
	return ev, nil
}

// InsertEvents imports a full event log in seq order (used by archive import).
func (s *Store) InsertEvents(ctx context.Context, p model.Project, events []*model.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO projects(id,name,created_at) VALUES(?,?,?)`,
		p.ID, p.Name, p.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z")); err != nil {
		return err
	}
	for _, ev := range events {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events(project_id, seq, id, type, prev_id, payload, created_at)
             VALUES(?,?,?,?,?,?,?)`,
			p.ID, ev.Seq, ev.ID, ev.Type, ev.PrevID, ev.Payload,
			ev.CreatedAt.UTC().Format("2006-01-02T15:04:05.000000000Z")); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func parseTS(ts string) (t timeT) { return timeParse(ts) }
