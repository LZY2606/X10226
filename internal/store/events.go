package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"tgareview/internal/domain"
)

// ErrNotFound is returned when a project/event is absent.
var ErrNotFound = errors.New("not found")

// CreateProject records a new project.
func (s *Store) CreateProject(ctx context.Context, id, name, origin string, seed int64, at time.Time) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects(project_id,name,origin,seed,created_at) VALUES(?,?,?,?,?)`,
		id, name, origin, seed, at.UTC().Format(time.RFC3339Nano))
	return err
}

// ProjectMeta is project header data.
type ProjectMeta struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Origin string `json:"origin"`
	Seed   int64  `json:"seed"`
	Head   int64  `json:"head"`
}

// ListProjects returns all projects.
func (s *Store) ListProjects(ctx context.Context) ([]ProjectMeta, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT p.project_id,p.name,p.origin,p.seed,COALESCE(h.seq,0)
		 FROM projects p LEFT JOIN head h ON h.project_id=p.project_id
		 ORDER BY p.created_at, p.project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectMeta
	for rows.Next() {
		var m ProjectMeta
		if err := rows.Scan(&m.ID, &m.Name, &m.Origin, &m.Seed, &m.Head); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AppendEvent atomically appends one event with the next sequence number and
// stores the materialized snapshot. It returns the committed event.
func (s *Store) AppendEvent(ctx context.Context, ev domain.Event, stateJSON, viewJSON []byte) (domain.Event, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ev, err
	}
	defer func() { _ = tx.Rollback() }()

	var next int64
	row := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE project_id=?`, ev.ProjectID)
	if err := row.Scan(&next); err != nil {
		return ev, err
	}
	ev.Seq = next
	_, err = tx.ExecContext(ctx,
		`INSERT INTO events(project_id,seq,event_id,at,actor,type,parent_id,payload)
		 VALUES(?,?,?,?,?,?,?,?)`,
		ev.ProjectID, ev.Seq, ev.EventID, ev.At.UTC().Format(time.RFC3339Nano),
		ev.Actor, string(ev.Type), ev.ParentID, []byte(ev.Payload))
	if err != nil {
		return ev, fmt.Errorf("insert event: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO snapshots(project_id,seq,view,state) VALUES(?,?,?,?)`,
		ev.ProjectID, ev.Seq, viewJSON, stateJSON); err != nil {
		return ev, err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO head(project_id,seq) VALUES(?,?)
		 ON CONFLICT(project_id) DO UPDATE SET seq=excluded.seq`,
		ev.ProjectID, ev.Seq); err != nil {
		return ev, err
	}
	if err := tx.Commit(); err != nil {
		return ev, err
	}
	return ev, nil
}

// Events returns the full ordered event log of a project.
func (s *Store) Events(ctx context.Context, projectID string) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT event_id,seq,at,actor,type,parent_id,payload
		 FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// HeadSeq returns the current head sequence (0 if no events).
func (s *Store) HeadSeq(ctx context.Context, projectID string) (int64, error) {
	var seq sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT seq FROM head WHERE project_id=?`, projectID).Scan(&seq)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return seq.Int64, err
}

// ViewAt returns the materialized view snapshot at an exact sequence.
func (s *Store) ViewAt(ctx context.Context, projectID string, seq int64) ([]byte, error) {
	var view []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT view FROM snapshots WHERE project_id=? AND seq=?`, projectID, seq).Scan(&view)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return view, err
}

// StateAt returns the full materialized state snapshot at a sequence.
func (s *Store) StateAt(ctx context.Context, projectID string, seq int64) ([]byte, error) {
	var state []byte
	err := s.db.QueryRowContext(ctx,
		`SELECT state FROM snapshots WHERE project_id=? AND seq=?`, projectID, seq).Scan(&state)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return state, err
}

type rowScanner interface {
	Next() bool
	Scan(...any) error
	Err() error
}

func scanEvents(rows *sql.Rows) ([]domain.Event, error) {
	var out []domain.Event
	for rows.Next() {
		var ev domain.Event
		var at, typ string
		if err := rows.Scan(&ev.EventID, &ev.Seq, &at, &ev.Actor, &typ, &ev.ParentID, &ev.Payload); err != nil {
			return nil, err
		}
		ev.Type = domain.EventType(typ)
		ev.At, _ = time.Parse(time.RFC3339Nano, at)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// Export reads the full archive including snapshots.
func (s *Store) Export(ctx context.Context, projectID string) (*ExportedProject, error) {
	var name, origin, createdAt string
	var seed int64
	err := s.db.QueryRowContext(ctx,
		`SELECT name,origin,seed,created_at FROM projects WHERE project_id=?`, projectID).
		Scan(&name, &origin, &seed, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	events, err := s.Events(ctx, projectID)
	if err != nil {
		return nil, err
	}
	for i := range events {
		events[i].ProjectID = projectID
	}
	snaps := map[int64][]byte{}
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq,view FROM snapshots WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var seq int64
		var view []byte
		if err := rows.Scan(&seq, &view); err != nil {
			return nil, err
		}
		snaps[seq] = view
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &ExportedProject{
		Archive: Archive{
			Format: "tga-review-archive-1", ProjectID: projectID,
			Name: name, Origin: origin, Seed: seed, Events: events,
		},
		Snapshots: snaps,
	}, nil
}

// Import inserts an archive verbatim, preserving ids, seq order and parent
// links. Fails if a project with the id already exists.
func (s *Store) Import(ctx context.Context, a *ExportedProject, at time.Time) error {
	if err := s.CreateProject(ctx, a.ProjectID, a.Name, a.Origin, a.Seed, at); err != nil {
		return fmt.Errorf("import project: %w", err)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, ev := range a.Events {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events(project_id,seq,event_id,at,actor,type,parent_id,payload)
			 VALUES(?,?,?,?,?,?,?,?)`,
			a.ProjectID, ev.Seq, ev.EventID, ev.At.UTC().Format(time.RFC3339Nano),
			ev.Actor, string(ev.Type), ev.ParentID, []byte(ev.Payload)); err != nil {
			return err
		}
		// Snapshots are rebuilt by the service after import (replay), so only
		// the event log is authoritative here.

	}
	var head int64
	if len(a.Events) > 0 {
		head = a.Events[len(a.Events)-1].Seq
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO head(project_id,seq) VALUES(?,?)`, a.ProjectID, head); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteProject removes a project entirely (used by export->delete->import).
func (s *Store) DeleteProject(ctx context.Context, projectID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	for _, q := range []string{
		`DELETE FROM events WHERE project_id=?`,
		`DELETE FROM snapshots WHERE project_id=?`,
		`DELETE FROM head WHERE project_id=?`,
		`DELETE FROM projects WHERE project_id=?`,
	} {
		if _, err := tx.ExecContext(ctx, q, projectID); err != nil {
			return err
		}
	}
	return tx.Commit()
}
