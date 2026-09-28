package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"

	"tga-evidence/internal/domain"
)

type Clock func() time.Time

type Store struct {
	db    *sql.DB
	now   Clock
	actor string
}

type Bundle struct {
	Schema     string         `json:"schema"`
	ExportedAt string         `json:"exported_at"`
	Project    domain.Project `json:"project"`
	Events     []domain.Event `json:"events"`
}

func Open(path string, now Clock) (*Store, error) {
	if now == nil {
		now = time.Now
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	st := &Store{db: db, now: now, actor: "reviewer"}
	if err := st.migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) SetActor(actor string) {
	if actor != "" {
		s.actor = actor
	}
}

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`PRAGMA foreign_keys = ON`,
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			case_pack_id TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL REFERENCES projects(id),
			seq INTEGER NOT NULL,
			type TEXT NOT NULL,
			occurred_at TEXT NOT NULL,
			actor TEXT NOT NULL,
			parent_event_id TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL,
			UNIQUE(project_id, seq)
		)`,
		`CREATE TABLE IF NOT EXISTS event_sources (
			project_id TEXT NOT NULL,
			event_id TEXT NOT NULL,
			source_event_id TEXT NOT NULL,
			ord INTEGER NOT NULL,
			PRIMARY KEY(event_id, ord),
			FOREIGN KEY(event_id) REFERENCES events(id),
			FOREIGN KEY(source_event_id) REFERENCES events(id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_sources_source ON event_sources(project_id, source_event_id)`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

type dbtx interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func (s *Store) CreateProject(ctx context.Context, p domain.Project) (domain.Project, error) {
	if p.ID == "" {
		p.ID = "prj_" + uuid.NewString()
	}
	if p.CreatedAt == "" {
		p.CreatedAt = s.now().UTC().Format(time.RFC3339Nano)
	}
	payload, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return p, err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO projects(id,name,case_pack_id,created_at,description) VALUES(?,?,?,?,?)`,
		p.ID, p.Name, p.CasePackID, p.CreatedAt, p.Description); err != nil {
		return p, err
	}
	if _, err := s.appendTx(ctx, tx, p.ID, domain.EventProjectCreated, "", nil, payload, p.ID, p.CreatedAt, "system"); err != nil {
		return p, err
	}
	return p, tx.Commit()
}

func (s *Store) Append(ctx context.Context, projectID, typ, parent string, sources []string, payload any, idOverride ...string) (domain.Event, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return domain.Event{}, err
	}
	return s.appendTx(ctx, s.db, projectID, typ, parent, sources, raw, firstOverride(idOverride), "", s.actor)
}

func firstOverride(values []string) string {
	if len(values) > 0 {
		return values[0]
	}
	return ""
}

func (s *Store) appendTx(ctx context.Context, tx dbtx, projectID, typ, parent string, sources []string, payload []byte, idOverride, occurredAt, actor string) (domain.Event, error) {
	if projectID == "" || typ == "" {
		return domain.Event{}, errors.New("project id and event id are required")
	}
	if idOverride == "" {
		idOverride = "evt_" + uuid.NewString()
	}
	if occurredAt == "" {
		occurredAt = s.now().UTC().Format(time.RFC3339Nano)
	}
	if actor == "" {
		actor = s.actor
	}
	var nextSeq int64
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE project_id=?`, projectID).Scan(&nextSeq); err != nil {
		return domain.Event{}, err
	}
	if parent != "" {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM events WHERE id=?`, parent).Scan(&count); err != nil {
			return domain.Event{}, err
		}
		if count == 0 {
			return domain.Event{}, fmt.Errorf("parent event %s not found", parent)
		}
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events(id,project_id,seq,type,occurred_at,actor,parent_event_id,payload) VALUES(?,?,?,?,?,?,?,?)`,
		idOverride, projectID, nextSeq, typ, occurredAt, actor, parent, string(payload)); err != nil {
		return domain.Event{}, err
	}
	for i, source := range sources {
		if source == "" {
			continue
		}
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM events WHERE id=?`, source).Scan(&count); err != nil {
			return domain.Event{}, err
		}
		if count == 0 {
			return domain.Event{}, fmt.Errorf("source event %s not found", source)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO event_sources(project_id,event_id,source_event_id,ord) VALUES(?,?,?,?)`,
			projectID, idOverride, source, i); err != nil {
			return domain.Event{}, err
		}
	}
	return domain.Event{ID: idOverride, ProjectID: projectID, Seq: nextSeq, Type: typ, OccurredAt: occurredAt, Actor: actor, ParentEventID: parent, DirectSources: sources, Payload: payload}, nil
}

func (s *Store) Projects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,case_pack_id,created_at,description FROM projects ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CasePackID, &p.CreatedAt, &p.Description); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) Project(ctx context.Context, id string) (domain.Project, error) {
	var p domain.Project
	err := s.db.QueryRowContext(ctx, `SELECT id,name,case_pack_id,created_at,description FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.CasePackID, &p.CreatedAt, &p.Description)
	return p, err
}

func (s *Store) Events(ctx context.Context, projectID string) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT e.id,e.project_id,e.seq,e.type,e.occurred_at,e.actor,e.parent_event_id,COALESCE(group_concat(es.source_event_id,'\x1f'),''),e.payload
		 FROM events e LEFT JOIN event_sources es ON es.event_id=e.id
		 WHERE e.project_id=? GROUP BY e.id ORDER BY e.seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

type rowsScanner interface {
	Scan(...any) error
	Next() bool
	Err() error
	Close() error
}

func scanEvents(rows *sql.Rows) ([]domain.Event, error) {
	var out []domain.Event
	for rows.Next() {
		var ev domain.Event
		var sources, payload string
		if err := rows.Scan(&ev.ID, &ev.ProjectID, &ev.Seq, &ev.Type, &ev.OccurredAt, &ev.Actor, &ev.ParentEventID, &sources, &payload); err != nil {
			return nil, err
		}
		ev.Payload = json.RawMessage(payload)
		if sources != "" {
			start := 0
			for i := 0; i <= len(sources); i++ {
				if i == len(sources) || sources[i] == 0x1f {
					if i > start {
						ev.DirectSources = append(ev.DirectSources, sources[start:i])
					}
					start = i + 1
				}
			}
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) Export(ctx context.Context, projectID string) (Bundle, error) {
	p, err := s.Project(ctx, projectID)
	if err != nil {
		return Bundle{}, err
	}
	events, err := s.Events(ctx, projectID)
	if err != nil {
		return Bundle{}, err
	}
	return Bundle{Schema: "tga-evidence/v1", ExportedAt: s.now().UTC().Format(time.RFC3339Nano), Project: p, Events: events}, nil
}

func (s *Store) Import(ctx context.Context, b Bundle, preserveID bool) (domain.Project, error) {
	if b.Schema != "tga-evidence/v1" {
		return domain.Project{}, fmt.Errorf("unsupported export schema %q", b.Schema)
	}
	if len(b.Events) == 0 || b.Events[0].Type != domain.EventProjectCreated {
		return domain.Project{}, errors.New("import bundle must begin with project.created")
	}
	p := b.Project
	if !preserveID {
		p.ID = "prj_" + uuid.NewString()
	}
	if p.CreatedAt == "" {
		p.CreatedAt = b.Events[0].OccurredAt
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.Project{}, err
	}
	defer tx.Rollback()
	if err := s.insertProject(ctx, p); err != nil {
		return domain.Project{}, err
	}
	idMap := map[string]string{}
	for _, ev := range b.Events {
		newID := ev.ID
		if !preserveID {
			newID = "evt_" + uuid.NewString()
		}
		idMap[ev.ID] = newID
	}
	for _, ev := range b.Events {
		newID := idMap[ev.ID]
		parent := idMap[ev.ParentEventID]
		sources := make([]string, 0, len(ev.DirectSources))
		for _, source := range ev.DirectSources {
			if mapped, ok := idMap[source]; ok {
				sources = append(sources, mapped)
			} else {
				sources = append(sources, source)
			}
		}
		payload := ev.Payload
		if ev.Type == domain.EventProjectCreated && !preserveID {
			payload, _ = json.Marshal(p)
		}
		written, err := s.appendTx(ctx, tx, p.ID, ev.Type, parent, sources, payload, newID, ev.OccurredAt, ev.Actor)
		if err != nil {
			return domain.Project{}, err
		}
		_ = written
	}
	if err := tx.Commit(); err != nil {
		return domain.Project{}, err
	}
	return p, nil
}
