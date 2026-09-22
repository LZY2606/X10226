package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tga/internal/domain"
)

// ErrNotFound is returned for missing entities.
var ErrNotFound = errors.New("not found")

// Clock supplies virtual timestamps; tests pass a fixed controllable clock.
type Clock func() string

func (s *Store) withTxT(ctx context.Context, fn func(*sql.Tx) error) error {
	return s.withTx(ctx, fn)
}

// Create inserts a project row (no event; the caller appends project_created).
func (s *Store) Create(ctx context.Context, p domain.Project) error {
	return s.withTx(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO projects(id,name,slug,created_ts,archived,head_seq) VALUES(?,?,?,?,0,0)`,
			p.ID, p.Name, p.Slug, p.CreatedTS)
		return err
	})
}

// Event appends an arbitrary audit event inside a fresh transaction.
func (s *Store) Event(ctx context.Context, projectID, typ string, payload any, refs []string, now Clock) (domain.HistoryEvent, error) {
	var hev domain.HistoryEvent
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		e, err := appendEvent(tx, projectID, typ, payload, refs, now())
		if err != nil {
			return err
		}
		hev = e
		return nil
	})
	return hev, err
}

// GetProject loads a project.
func (s *Store) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	var p domain.Project
	var archived int
	err := s.db.QueryRowContext(ctx, `SELECT id,name,slug,created_ts,archived,head_seq FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Slug, &p.CreatedTS, &archived, &p.HeadSeq)
	if err != nil {
		return nil, ErrNotFound
	}
	p.Archived = archived == 1
	return &p, nil
}

// ListProjects returns all projects in insertion order.
func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,slug,created_ts,archived,head_seq FROM projects ORDER BY rowid`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		var a int
		if err := rows.Scan(&p.ID, &p.Name, &p.Slug, &p.CreatedTS, &a, &p.HeadSeq); err != nil {
			return nil, err
		}
		p.Archived = a == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetArchived flips archived state and records an event.
func (s *Store) SetArchived(ctx context.Context, id string, archived bool, now Clock) (domain.HistoryEvent, error) {
	var hev domain.HistoryEvent
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE projects SET archived=? WHERE id=?`, btoi(archived), id); err != nil {
			return err
		}
		typ := "project_archived"
		if !archived {
			typ = "project_reopened"
		}
		e, err := appendEvent(tx, id, typ, map[string]any{"archived": archived}, nil, now())
		if err != nil {
			return err
		}
		hev = e
		return nil
	})
	return hev, err
}

// History returns the full ordered hash-chained event log.
func (s *Store) History(ctx context.Context, projectID string) ([]domain.HistoryEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq,project_id,type,payload,ref_ids,virtual_ts,prev_hash,hash FROM history WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.HistoryEvent
	for rows.Next() {
		var h domain.HistoryEvent
		var payload, refs string
		if err := rows.Scan(&h.Seq, &h.ProjectID, &h.Type, &payload, &refs, &h.VirtualTS, &h.PrevHash, &h.Hash); err != nil {
			return nil, err
		}
		h.Payload = json.RawMessage(payload)
		_ = json.Unmarshal([]byte(refs), &h.RefIDs)
		out = append(out, h)
	}
	return out, rows.Err()
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}
