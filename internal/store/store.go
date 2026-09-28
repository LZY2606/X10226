// Package store persists the event-sourced TGA aggregate in SQLite. Events are
// the single source of truth: all sources, derivations, decisions and
// judgements live inside event payloads, so export/delete/import preserves
// identities, order and provenance relations byte-for-byte.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"tga/internal/domain"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_vclock INTEGER NOT NULL,
  head_vclock INTEGER NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  project_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  type TEXT NOT NULL,
  clock TEXT NOT NULL,
  vclock INTEGER NOT NULL,
  actor TEXT NOT NULL,
  payload TEXT NOT NULL,
  refs TEXT NOT NULL,
  PRIMARY KEY (project_id, seq)
);
CREATE TABLE IF NOT EXISTS bundles_log (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id TEXT NOT NULL,
  at TEXT NOT NULL,
  kind TEXT NOT NULL,
  detail TEXT NOT NULL
);`)
	return err
}

func (s *Store) UpsertProject(ctx context.Context, p *domain.Project) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO projects(id,name,created_vclock,head_vclock)
VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, head_vclock=excluded.head_vclock`,
		p.ID, p.Name, p.CreatedVClock, p.HeadVClock)
	return err
}

type ProjectRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Head int64  `json:"head_vclock"`
}

func (s *Store) ListProjects(ctx context.Context) ([]ProjectRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,head_vclock FROM projects ORDER BY created_vclock, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		var r ProjectRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Head); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AppendEvents inserts new events in one transaction. Seq values are already
// assigned deterministically by the engine.
func (s *Store) AppendEvents(ctx context.Context, project *domain.Project, events []*domain.Event) error {
	if len(events) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if project != nil {
		if _, err := tx.ExecContext(ctx, `INSERT INTO projects(id,name,created_vclock,head_vclock)
VALUES(?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name, head_vclock=excluded.head_vclock`,
			project.ID, project.Name, project.CreatedVClock, project.HeadVClock); err != nil {
			return err
		}
	}
	for _, ev := range events {
		payload, err := json.Marshal(ev.Payload)
		if err != nil {
			return err
		}
		refs, err := json.Marshal(ev.Refs)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO events(project_id,seq,type,clock,vclock,actor,payload,refs)
VALUES(?,?,?,?,?,?,?,?)`, ev.ProjectID, ev.Seq, string(ev.Type), ev.Clock, ev.VClock, ev.Actor,
			string(payload), string(refs)); err != nil {
			return fmt.Errorf("append event %s/%d: %w", ev.ProjectID, ev.Seq, err)
		}
	}
	return tx.Commit()
}

func (s *Store) LoadEvents(ctx context.Context, projectID string) ([]*domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT seq,type,clock,vclock,actor,payload,refs
FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Event
	for rows.Next() {
		ev := &domain.Event{ProjectID: projectID}
		var payload, refs string
		if err := rows.Scan(&ev.Seq, &ev.Type, &ev.Clock, &ev.VClock, &ev.Actor, &payload, &refs); err != nil {
			return nil, err
		}
		if payload != "" {
			if err := json.Unmarshal([]byte(payload), &ev.Payload); err != nil {
				return nil, fmt.Errorf("event %d payload: %w", ev.Seq, err)
			}
		}
		if refs != "" && refs != "null" {
			_ = json.Unmarshal([]byte(refs), &ev.Refs)
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE project_id=?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}
