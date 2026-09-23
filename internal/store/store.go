package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"tga-review/internal/replay"
)

// Store is the SQLite-backed event store.
type Store struct {
	db    *sql.DB
	clock Clock
}

// Open opens/creates a database at dsn and applies the schema.
func Open(ctx context.Context, dsn string, clock Clock) (*Store, error) {
	if clock == nil {
		clock = NewRealClock()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db, clock: clock}
	if err := s.seedMeta(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

// SetClock swaps the clock (used by tests for virtual time).
func (s *Store) SetClock(c Clock) { s.clock = c }

const schemaSQL = `
CREATE TABLE IF NOT EXISTS meta(k TEXT PRIMARY KEY, v TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS projects(
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  created_seq INTEGER NOT NULL,
  created_ts TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events(
  project_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  id TEXT NOT NULL,
  type TEXT NOT NULL,
  virtual_ts TEXT NOT NULL,
  actor TEXT NOT NULL DEFAULT '',
  payload TEXT NOT NULL,
  PRIMARY KEY(project_id, seq)
);
CREATE INDEX IF NOT EXISTS events_id ON events(project_id, id);
`

func (s *Store) seedMeta(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO meta(k,v) VALUES('schema','tga-review/v1')`)
	return err
}

// CreateProject appends a project_created event at seq 1.
func (s *Store) CreateProject(ctx context.Context, id, name string) error {
	pl, _ := json.Marshal(replay.ProjectCreatedPayload{Name: name})
	ev := replay.Event{ProjectID: id, Type: replay.EvProjectCreated,
		VirtualTS: s.clock.NowISO(), Payload: pl}
	return s.appendEvents(ctx, id, []replay.Event{ev}, func(_ int64) error {
		return nil
	}, true)
}

// ListProjects returns project summaries.
func (s *Store) ListProjects(ctx context.Context) ([]ProjectInfo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,created_ts FROM projects ORDER BY created_seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectInfo
	for rows.Next() {
		var p ProjectInfo
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedTS); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ProjectInfo is a project list row.
type ProjectInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CreatedTS string `json:"created_ts"`
}

func (s *Store) projectExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

// AppendEvents stores events, assigning monotonically increasing seqs inside one
// transaction. The project row is created on its project_created event.
func (s *Store) AppendEvents(ctx context.Context, projectID string, evs []replay.Event) error {
	return s.appendEvents(ctx, projectID, evs, nil, false)
}

func (s *Store) appendEvents(ctx context.Context, projectID string, evs []replay.Event, after func(seq int64) error, create bool) (err error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	var seq int64
	if create {
		if ok, e := s.existsTx(ctx, tx, projectID); e != nil {
			return e
		} else if ok {
			return fmt.Errorf("project %s already exists", projectID)
		}
		seq = 1
	} else {
		row := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(seq),0) FROM events WHERE project_id=?`, projectID)
		if err = row.Scan(&seq); err != nil {
			return err
		}
		if seq == 0 {
			return fmt.Errorf("unknown project %s", projectID)
		}
		seq++
	}
	for i := range evs {
		evs[i].Seq = seq
		if evs[i].ID == "" {
			evs[i].ID = replay.NewEventID()
		}
		if evs[i].VirtualTS == "" {
			evs[i].VirtualTS = s.clock.NowISO()
		}
		pl, _ := json.Marshal(evs[i].Payload)
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO events(project_id,seq,id,type,virtual_ts,actor,payload)
			 VALUES(?,?,?,?,?,?,?)`,
			projectID, seq, evs[i].ID, evs[i].Type, evs[i].VirtualTS, evs[i].Actor, string(pl)); err != nil {
			return err
		}
		if evs[i].Type == replay.EvProjectCreated {
			var p replay.ProjectCreatedPayload
			_ = json.Unmarshal(pl, &p)
			if _, err = tx.ExecContext(ctx,
				`INSERT INTO projects(id,name,created_seq,created_ts) VALUES(?,?,?,?)`,
				projectID, p.Name, seq, evs[i].VirtualTS); err != nil {
				return err
			}
		}
		if after != nil {
			if err = after(seq); err != nil {
				return err
			}
		}
		seq++
	}
	return tx.Commit()
}

func (s *Store) existsTx(ctx context.Context, tx *sql.Tx, id string) (bool, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

// Events returns the full ordered event stream of a project.
func (s *Store) Events(ctx context.Context, projectID string) ([]replay.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,type,virtual_ts,COALESCE(actor,''),payload,seq
		 FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []replay.Event
	for rows.Next() {
		var ev replay.Event
		var pl string
		if err := rows.Scan(&ev.ID, &ev.Type, &ev.VirtualTS, &ev.Actor, &pl, &ev.Seq); err != nil {
			return nil, err
		}
		ev.ProjectID = projectID
		ev.Payload = json.RawMessage(pl)
		out = append(out, ev)
	}
	return out, rows.Err()
}

// MarkEventUndone stamps Undone on an event (used when projecting undo state).
func (s *Store) MarkEventUndone(ctx context.Context, projectID, eventID string) error {
	// Undo is represented as an undo event, never as an in-place mutation.
	return nil
}
