// Package store is the SQLite persistence for the append-only event log and
// the cached derived artifacts.
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

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(schemaSQL)
	return err
}

const schemaSQL = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  seed INTEGER NOT NULL,
  created_virtual TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS events (
  project_id TEXT NOT NULL,
  seq INTEGER NOT NULL,
  id TEXT NOT NULL,
  type TEXT NOT NULL,
  at_virtual TEXT NOT NULL,
  parent_seq INTEGER NOT NULL DEFAULT 0,
  payload TEXT NOT NULL,
  PRIMARY KEY (project_id, seq)
);
CREATE UNIQUE INDEX IF NOT EXISTS events_event_id ON events(project_id, id);
CREATE INDEX IF NOT EXISTS events_type ON events(project_id, type, seq);
CREATE TABLE IF NOT EXISTS artifacts (
  project_id TEXT NOT NULL,
  kind TEXT NOT NULL,
  stable_id TEXT NOT NULL,
  computed_at_seq INTEGER NOT NULL,
  body TEXT NOT NULL,
  PRIMARY KEY (project_id, kind, stable_id, computed_at_seq)
);
CREATE INDEX IF NOT EXISTS artifacts_lookup ON artifacts(project_id, kind, computed_at_seq);
`

func (s *Store) CreateProject(ctx context.Context, id, name string, seed int64, atVirtual string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects(id,name,seed,created_virtual) VALUES(?,?,?,?)`,
		id, name, seed, atVirtual)
	return err
}

func (s *Store) ListProjects(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM projects ORDER BY id`)
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

func (s *Store) ProjectExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

// NextSeq returns the next append position.
func (s *Store) NextSeq(ctx context.Context, projectID string) (int64, error) {
	var max sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(seq) FROM events WHERE project_id=?`, projectID).Scan(&max)
	if err != nil {
		return 0, err
	}
	return max.Int64 + 1, nil
}

// AppendEvent persists one event, assigning seq when it is zero.
func (s *Store) AppendEvent(ctx context.Context, e *domain.Event) error {
	if e.Seq == 0 {
		seq, err := s.NextSeq(ctx, e.ProjectID)
		if err != nil {
			return err
		}
		e.Seq = seq
	}
	body, err := json.Marshal(e.Payload)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO events(project_id,seq,id,type,at_virtual,parent_seq,payload)
		 VALUES(?,?,?,?,?,?,?)`,
		e.ProjectID, e.Seq, e.ID, e.Type, e.AtVirtual.UTC().Format("2006-01-02T15:04:05.000000000Z"), e.ParentSeq, string(body))
	if err != nil {
		return fmt.Errorf("append event %s: %w", e.ID, err)
	}
	return nil
}

// Events replays the log in order, optionally stopping at an inclusive seq.
func (s *Store) Events(ctx context.Context, projectID string, upToSeq int64) ([]domain.Event, error) {
	q := `SELECT seq,id,type,at_virtual,parent_seq,payload FROM events WHERE project_id=?`
	args := []any{projectID}
	if upToSeq > 0 {
		q += ` AND seq<=?`
		args = append(args, upToSeq)
	}
	q += ` ORDER BY seq`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var at, body string
		if err := rows.Scan(&e.Seq, &e.ID, &e.Type, &at, &e.ParentSeq, &body); err != nil {
			return nil, err
		}
		e.ProjectID = projectID
		if err := json.Unmarshal([]byte(body), &e.Payload); err != nil {
			return nil, fmt.Errorf("event %s payload: %w", e.ID, err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// MaxSeq returns the current tip.
func (s *Store) MaxSeq(ctx context.Context, projectID string) (int64, error) {
	var max sql.NullInt64
	err := s.db.QueryRowContext(ctx, `SELECT MAX(seq) FROM events WHERE project_id=?`, projectID).Scan(&max)
	if err != nil {
		return 0, err
	}
	return max.Int64, nil
}

// SaveArtifact caches one immutable derived artifact (superseded versions
// remain queryable).
func (s *Store) SaveArtifact(ctx context.Context, projectID, kind, stableID string, atSeq int64, body []byte) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR REPLACE INTO artifacts(project_id,kind,stable_id,computed_at_seq,body)
		 VALUES(?,?,?,?,?)`, projectID, kind, stableID, atSeq, string(body))
	return err
}

// ArtifactAt returns the newest cached version of stableID not newer than
// atSeq, so old reviews read the dependency version that was visible then.
func (s *Store) ArtifactAt(ctx context.Context, projectID, kind, stableID string, atSeq int64) ([]byte, error) {
	var body string
	err := s.db.QueryRowContext(ctx,
		`SELECT body FROM artifacts WHERE project_id=? AND kind=? AND stable_id=? AND computed_at_seq<=?
		 ORDER BY computed_at_seq DESC LIMIT 1`,
		projectID, kind, stableID, atSeq).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return []byte(body), err
}

// ImportEvents replaces a project log with the supplied events, preserving
// their ids, seq and ordering exactly (used by export -> delete -> import).
func (s *Store) ImportEvents(ctx context.Context, projectID, name string, seed int64, createdVirtual string, events []domain.Event) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts WHERE project_id=?`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE project_id=?`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO projects(id,name,seed,created_virtual) VALUES(?,?,?,?)`,
		projectID, name, seed, createdVirtual); err != nil {
		return err
	}
	for _, e := range events {
		body, err := json.Marshal(e.Payload)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events(project_id,seq,id,type,at_virtual,parent_seq,payload)
			 VALUES(?,?,?,?,?,?,?)`,
			projectID, e.Seq, e.ID, e.Type, e.AtVirtual.UTC().Format("2006-01-02T15:04:05.000000000Z"), e.ParentSeq, string(body)); err != nil {
			return fmt.Errorf("import event %s: %w", e.ID, err)
		}
	}
	return tx.Commit()
}

// DeleteProject removes all local rows for a project.
func (s *Store) DeleteProject(ctx context.Context, projectID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM artifacts WHERE project_id=?`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE project_id=?`, projectID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM projects WHERE id=?`, projectID); err != nil {
		return err
	}
	return tx.Commit()
}

type ProjectMeta struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Seed    int64  `json:"seed"`
	Created string `json:"created_virtual"`
}

func (s *Store) Project(ctx context.Context, id string) (*ProjectMeta, error) {
	var p ProjectMeta
	err := s.db.QueryRowContext(ctx,
		`SELECT id,name,seed,created_virtual FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Seed, &p.Created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}
