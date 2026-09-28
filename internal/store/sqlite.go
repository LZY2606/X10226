package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"

	"tga-review/internal/domain"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	st := &Store{db: db}
	if err := st.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
create table if not exists projects (
  project_id text primary key,
  name text not null,
  created_seq integer not null
);
create table if not exists events (
  seq integer primary key,
  event_id text not null unique,
  project_id text not null,
  type text not null,
  actor text not null,
  occurred_at text not null,
  payload text not null check (json_valid(payload))
);
create index if not exists events_project_seq on events(project_id, seq);
create table if not exists export_meta (
  key text primary key,
  value text not null
);`)
	return err
}

func (s *Store) Append(ctx context.Context, projectID, eventType, actor string, payload any, eventID string, occurredAt any) (domain.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Event{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Event{}, err
	}
	defer tx.Rollback()

	var maxSeq sql.NullInt64
	if err := tx.QueryRowContext(ctx, `select max(seq) from events`).Scan(&maxSeq); err != nil {
		return domain.Event{}, err
	}
	var exists int
	if err := tx.QueryRowContext(ctx, `select count(1) from events where event_id = ?`, eventID).Scan(&exists); err != nil {
		return domain.Event{}, err
	}
	if exists != 0 {
		return domain.Event{}, fmt.Errorf("event already exists: %s", eventID)
	}
	seq := int64(1)
	if maxSeq.Valid {
		seq = maxSeq.Int64 + 1
	}
	at := eventTime(occurredAt)
	_, err = tx.ExecContext(ctx, `insert into events(seq,event_id,project_id,type,actor,occurred_at,payload) values(?,?,?,?,?,?,?)`, seq, eventID, projectID, eventType, actor, at.Format("2006-01-02T15:04:05.999999999Z07:00"), string(raw))
	if err != nil {
		return domain.Event{}, err
	}
	if eventType == domain.EventProjectCreated {
		var p domain.ProjectCreatedPayload
		if err := json.Unmarshal(raw, &p); err != nil {
			return domain.Event{}, err
		}
		_, err = tx.ExecContext(ctx, `insert into projects(project_id,name,created_seq) values(?,?,?)`, projectID, p.Name, seq)
		if err != nil {
			return domain.Event{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return domain.Event{}, err
	}
	return domain.Event{Seq: seq, EventID: eventID, ProjectID: projectID, Type: eventType, Actor: actor, OccurredAt: at, Payload: raw}, nil
}

func (s *Store) Events(ctx context.Context, projectID string) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, `select seq,event_id,project_id,type,actor,occurred_at,payload from events where project_id = ? order by seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		var when string
		if err := rows.Scan(&e.Seq, &e.EventID, &e.ProjectID, &e.Type, &e.Actor, &when, &e.Payload); err != nil {
			return nil, err
		}
		e.OccurredAt = parseTime(when)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) EventByID(ctx context.Context, eventID string) (domain.Event, error) {
	var e domain.Event
	var when string
	err := s.db.QueryRowContext(ctx, `select seq,event_id,project_id,type,actor,occurred_at,payload from events where event_id=?`, eventID).Scan(&e.Seq, &e.EventID, &e.ProjectID, &e.Type, &e.Actor, &when, &e.Payload)
	if errors.Is(err, sql.ErrNoRows) {
		return domain.Event{}, ErrNotFound
	}
	if err != nil {
		return domain.Event{}, err
	}
	e.OccurredAt = parseTime(when)
	return e, nil
}

func (s *Store) ProjectID(ctx context.Context) (string, error) {
	var id string
	err := s.db.QueryRowContext(ctx, `select project_id from projects order by created_seq limit 1`).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

func (s *Store) ExportEvents(ctx context.Context) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, `select seq,event_id,project_id,type,actor,occurred_at,payload from events order by seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []domain.Event
	for rows.Next() {
		var e domain.Event
		var when string
		if err := rows.Scan(&e.Seq, &e.EventID, &e.ProjectID, &e.Type, &e.Actor, &when, &e.Payload); err != nil {
			return nil, err
		}
		e.OccurredAt = parseTime(when)
		events = append(events, e)
	}
	return events, rows.Err()
}

func (s *Store) ImportEvents(ctx context.Context, events []domain.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range events {
		if !json.Valid(e.Payload) {
			return fmt.Errorf("event %s payload is not json", e.EventID)
		}
		if _, err := tx.ExecContext(ctx, `insert into events(seq,event_id,project_id,type,actor,occurred_at,payload) values(?,?,?,?,?,?,?)`, e.Seq, e.EventID, e.ProjectID, e.Type, e.Actor, e.OccurredAt.Format("2006-01-02T15:04:05.999999999Z07:00"), string(e.Payload)); err != nil {
			return err
		}
		if e.Type == domain.EventProjectCreated {
			var p domain.ProjectCreatedPayload
			if err := json.Unmarshal(e.Payload, &p); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `insert into projects(project_id,name,created_seq) values(?,?,?)`, e.ProjectID, p.Name, e.Seq); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (s *Store) HasEvents(ctx context.Context) (bool, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, `select count(1) from events`).Scan(&n); err != nil {
		return false, err
	}
	return n > 0, nil
}

var ErrNotFound = errors.New("not found")
