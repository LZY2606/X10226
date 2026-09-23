package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"tga-archive/internal/domain"
)

// AppendProject creates a project and its first event in one transaction.
func (s *Store) AppendProject(ctx context.Context, id, name string, ev domain.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects(id,name,created_at) VALUES(?,?,?)`,
		id, name, ev.At); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, id, ev); err != nil {
		return err
	}
	return tx.Commit()
}

// AppendEvents stores events for a project as one atomic unit.
func (s *Store) AppendEvents(ctx context.Context, projectID string, evs []domain.Event) error {
	if len(evs) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, ev := range evs {
		if err := insertEvent(ctx, tx, projectID, ev); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func insertEvent(ctx context.Context, tx *sql.Tx, projectID string, ev domain.Event) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO events(project_id,seq,id,at,type,parent_seq,payload)
		 VALUES(?,?,?,?,?,?,?)`,
		projectID, ev.Seq, ev.ID, ev.At, ev.Type, ev.ParentSeq, string(ev.Payload))
	if err != nil {
		return fmt.Errorf("insert event seq=%d: %w", ev.Seq, err)
	}
	return nil
}

// Events loads the full ordered log for a project.
func (s *Store) Events(ctx context.Context, projectID string) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq,id,at,type,parent_seq,payload FROM events
		 WHERE project_id=? ORDER BY seq ASC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var ev domain.Event
		var payload string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.At, &ev.Type, &ev.ParentSeq, &payload); err != nil {
			return nil, err
		}
		ev.Payload = json.RawMessage(payload)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// ProjectRow is a project listing.
type ProjectRow struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

// ListProjects returns all projects.
func (s *Store) ListProjects(ctx context.Context) ([]ProjectRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,created_at FROM projects ORDER BY created_at,id`)
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

// ProjectExists reports whether a project id is present.
func (s *Store) ProjectExists(ctx context.Context, id string) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id=?`, id).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

// ReplaceArtifactProjection rebuilds the artifacts table after replay.
func (s *Store) ReplaceArtifactProjection(ctx context.Context, projectID string, arts map[string]projArt) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts WHERE project_id=?`, projectID); err != nil {
		return err
	}
	for id, a := range arts {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO artifacts(project_id,value_id,kind,first_seq,latest_seq)
			 VALUES(?,?,?,?,?)`, projectID, id, a.Kind, a.FirstSeq, a.LatestSeq); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type projArt struct {
	Kind      string
	FirstSeq  int
	LatestSeq int
}

// MetaGet/MetaSet hold small key/value bookkeeping (e.g. archive count).
func (s *Store) MetaGet(ctx context.Context, k string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx, `SELECT v FROM meta WHERE k=?`, k).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) MetaSet(ctx context.Context, k, v string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meta(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

// Exec runs a write without a result (admin/import helpers).
func (s *Store) Exec(ctx context.Context, q string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, q, args...)
}
