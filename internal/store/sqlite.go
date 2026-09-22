package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/example/tga-evidence/internal/domain"
)

type SQLiteStore struct{ db *sql.DB }

func Open(path string) (*SQLiteStore, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)", path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	store := &SQLiteStore{db: db}
	if err := store.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (store *SQLiteStore) Close() error { return store.db.Close() }

func (store *SQLiteStore) migrate(ctx context.Context) error {
	_, err := store.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS projects (
  project_id TEXT PRIMARY KEY,
  created_event_id TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  project_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  event_id TEXT NOT NULL,
  parent_event_id TEXT NOT NULL,
  type TEXT NOT NULL,
  occurred_at TEXT NOT NULL,
  actor TEXT NOT NULL,
  payload TEXT NOT NULL,
  PRIMARY KEY (project_id, sequence),
  UNIQUE (project_id, event_id)
);
CREATE INDEX IF NOT EXISTS events_project_sequence ON events(project_id, sequence);
`)
	return err
}

func (store *SQLiteStore) CreateProject(ctx context.Context, event domain.Event) error {
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM projects WHERE project_id = ?`, event.ProjectID).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return fmt.Errorf("project %s already exists", event.ProjectID)
	}
	event.Sequence = 1
	event.ParentEventID = ""
	if _, err := tx.ExecContext(ctx, `INSERT INTO projects(project_id, created_event_id, created_at) VALUES (?, ?, ?)`, event.ProjectID, event.ID, event.OccurredAt); err != nil {
		return err
	}
	if err := insertEvent(ctx, tx, event); err != nil {
		return err
	}
	return tx.Commit()
}

func (store *SQLiteStore) AppendEvents(ctx context.Context, projectID string, events []domain.Event) (int64, error) {
	if len(events) == 0 {
		return 0, nil
	}
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var sequence int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) FROM events WHERE project_id = ?`, projectID).Scan(&sequence); err != nil {
		return 0, err
	}
	if sequence == 0 {
		return 0, fmt.Errorf("project %s not found", projectID)
	}
	parentID, err := lastEventID(ctx, tx, projectID)
	if err != nil {
		return 0, err
	}
	for _, event := range events {
		sequence++
		event.ProjectID = projectID
		event.Sequence = sequence
		event.ParentEventID = parentID
		if err := insertEvent(ctx, tx, event); err != nil {
			return 0, err
		}
		parentID = event.ID
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return sequence, nil
}

func (store *SQLiteStore) ImportProject(ctx context.Context, create domain.Event, events []domain.Event) error {
	all := append([]domain.Event{create}, events...)
	tx, err := store.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var exists int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM projects WHERE project_id = ?`, create.ProjectID).Scan(&exists); err != nil {
		return err
	}
	if exists != 0 {
		return fmt.Errorf("project %s already exists", create.ProjectID)
	}
	parentID := ""
	for sequence, event := range all {
		event.ProjectID = create.ProjectID
		event.Sequence = int64(sequence + 1)
		event.ParentEventID = parentID
		if sequence == 0 {
			if event.Type != domain.EventProjectCreated {
				return fmt.Errorf("import must start with project_created")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO projects(project_id, created_event_id, created_at) VALUES (?, ?, ?)`, event.ProjectID, event.ID, event.OccurredAt); err != nil {
				return err
			}
		}
		if err := insertEvent(ctx, tx, event); err != nil {
			return err
		}
		parentID = event.ID
	}
	return tx.Commit()
}

func insertEvent(ctx context.Context, tx *sql.Tx, event domain.Event) error {
	if len(event.Payload) == 0 {
		event.Payload = json.RawMessage(`{}`)
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO events(project_id, sequence, event_id, parent_event_id, type, occurred_at, actor, payload) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		event.ProjectID, event.Sequence, event.ID, event.ParentEventID, event.Type, event.OccurredAt, event.Actor, string(event.Payload))
	return err
}

func lastEventID(ctx context.Context, tx *sql.Tx, projectID string) (string, error) {
	var id string
	err := tx.QueryRowContext(ctx, `SELECT event_id FROM events WHERE project_id = ? ORDER BY sequence DESC LIMIT 1`, projectID).Scan(&id)
	return id, err
}

func (store *SQLiteStore) ListEvents(ctx context.Context, projectID string) ([]domain.Event, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT event_id, parent_event_id, type, occurred_at, actor, payload FROM events WHERE project_id = ? ORDER BY sequence`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []domain.Event
	var sequence int64 = 0
	for rows.Next() {
		sequence++
		var event domain.Event
		var payload string
		if err := rows.Scan(&event.ID, &event.ParentEventID, &event.Type, &event.OccurredAt, &event.Actor, &payload); err != nil {
			return nil, err
		}
		event.ProjectID = projectID
		event.Sequence = sequence
		event.Payload = json.RawMessage(payload)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (store *SQLiteStore) ListProjectIDs(ctx context.Context) ([]string, error) {
	rows, err := store.db.QueryContext(ctx, `SELECT project_id FROM projects ORDER BY created_at, project_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (store *SQLiteStore) ProjectExists(ctx context.Context, projectID string) (bool, error) {
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM projects WHERE project_id = ?`, projectID).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}
