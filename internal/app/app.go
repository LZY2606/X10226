package app

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

const AlgorithmVersion = "tga-stage-v1"

var (
	ErrConflict = errors.New("conflict")
	ErrInvalid  = errors.New("invalid input")
	ErrNotFound = errors.New("not found")
)

type App struct {
	db    *sql.DB
	clock Clock
}

func New(db *sql.DB, clock Clock) *App {
	if clock == nil {
		clock = func() string { return time.Now().UTC().Format(time.RFC3339Nano) }
	}
	return &App{db: db, clock: clock}
}

func FixedClock(value string) Clock {
	return func() string { return value }
}

func (a *App) CreateProject(ctx context.Context, projectID, name, materialID, clockFamily string) (Project, error) {
	if projectID == "" || name == "" || materialID == "" {
		return Project{}, fmt.Errorf("%w: project id, name and material id are required", ErrInvalid)
	}
	if clockFamily == "" {
		clockFamily = "wall-utc"
	}
	now := a.clock()
	project := Project{ProjectID: projectID, Name: name, MaterialID: materialID, CreatedAt: now, ClockFamily: clockFamily}
	payload, _ := json.Marshal(project)
	if err := a.appendEvent(ctx, projectID, "project_created", "system", now, payload, func(tx *sql.Tx, _ Event) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO projects(project_id,name,material_id,created_at,clock_family) VALUES(?,?,?,?,?)`,
			projectID, name, materialID, now, clockFamily)
		return err
	}); err != nil {
		return Project{}, err
	}
	return project, nil
}

func (a *App) appendEvent(ctx context.Context, projectID, eventType, actor, occurredAt string, payload json.RawMessage, apply func(*sql.Tx, Event) error) error {
	tx, err := a.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var seq int64
	if eventType != "project_created" {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM events WHERE project_id=?`, projectID).Scan(&seq); err != nil {
			return err
		}
	} else {
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE project_id=?`, projectID).Scan(&seq); err != nil {
			return err
		}
		if seq != 0 {
			return fmt.Errorf("%w: project already exists", ErrConflict)
		}
	}

	var previous sql.NullString
	if err := tx.QueryRowContext(ctx, `SELECT event_hash FROM events WHERE project_id=? ORDER BY seq DESC LIMIT 1`, projectID).Scan(&previous); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	eventID := stableID("event", projectID, seq, eventType, string(payload))
	prev := previous.String
	hash := eventHash(projectID, seq, eventID, eventType, actor, occurredAt, payload, prev)
	event := Event{ProjectID: projectID, Seq: seq, EventID: eventID, EventType: eventType, Actor: actor, OccurredAt: occurredAt, Payload: payload, PreviousHash: prev, EventHash: hash}
	if _, err := tx.ExecContext(ctx, `INSERT INTO events(project_id,seq,event_id,event_type,actor,occurred_at,payload_json,previous_hash,event_hash)
		VALUES(?,?,?,?,?,?,?,?,?)`, projectID, seq, eventID, eventType, actor, occurredAt, string(payload), prev, hash); err != nil {
		return err
	}
	if apply != nil {
		if err := apply(tx, event); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func stableID(prefix string, parts ...interface{}) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\x00", prefix)
	for _, part := range parts {
		fmt.Fprintf(h, "%v\x00", part)
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}

func eventHash(projectID string, seq int64, eventID, eventType, actor, occurredAt string, payload json.RawMessage, previous string) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s\n%d\n%s\n%s\n%s\n%s\n%s\n%s", projectID, seq, eventID, eventType, actor, occurredAt, string(payload), previous)
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func (a *App) Events(ctx context.Context, projectID string) ([]Event, error) {
	rows, err := a.db.QueryContext(ctx, `SELECT project_id,seq,event_id,event_type,actor,occurred_at,payload_json,previous_hash,event_hash FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var events []Event
	for rows.Next() {
		var event Event
		var payload string
		if err := rows.Scan(&event.ProjectID, &event.Seq, &event.EventID, &event.EventType, &event.Actor, &event.OccurredAt, &payload, &event.PreviousHash, &event.EventHash); err != nil {
			return nil, err
		}
		event.Payload = json.RawMessage(payload)
		events = append(events, event)
	}
	return events, rows.Err()
}

func (a *App) Export(ctx context.Context, projectID string) (ExportBundle, error) {
	project, err := a.project(ctx, projectID)
	if err != nil {
		return ExportBundle{}, err
	}
	events, err := a.Events(ctx, projectID)
	if err != nil {
		return ExportBundle{}, err
	}
	return ExportBundle{Format: "tga-review-bundle", Version: "1", Project: project, Events: events}, nil
}

func (a *App) project(ctx context.Context, projectID string) (Project, error) {
	var project Project
	err := a.db.QueryRowContext(ctx, `SELECT project_id,name,material_id,created_at,clock_family FROM projects WHERE project_id=?`, projectID).
		Scan(&project.ProjectID, &project.Name, &project.MaterialID, &project.CreatedAt, &project.ClockFamily)
	if errors.Is(err, sql.ErrNoRows) {
		return Project{}, fmt.Errorf("%w: project %s", ErrNotFound, projectID)
	}
	return project, err
}
