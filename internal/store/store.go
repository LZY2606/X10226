// Package store persists projects, sources, nodes and the append-only event
// log in SQLite. JSON blobs keep the full domain objects intact.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"tga-review/internal/domain"
)

// Store is the SQLite-backed repository.
type Store struct {
	db *sql.DB
}

// Open creates/opens a database at dsn (":memory:" supported) and migrates it.
func Open(ctx context.Context, dsn string) (*Store, error) {
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

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS projects(
			id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at TEXT NOT NULL, seed INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS sources(
			id TEXT PRIMARY KEY, project_id TEXT NOT NULL, kind TEXT NOT NULL,
			version INTEGER NOT NULL, parent_id TEXT, body TEXT NOT NULL,
			batch_id TEXT, accepted INTEGER NOT NULL DEFAULT 1)`,
		`CREATE TABLE IF NOT EXISTS nodes(
			id TEXT PRIMARY KEY, project_id TEXT NOT NULL, kind TEXT NOT NULL,
			current INTEGER NOT NULL, superseded_by TEXT, body TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS stale(
			node_id TEXT PRIMARY KEY, reason TEXT NOT NULL, since_seq INTEGER NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS events(
			seq INTEGER PRIMARY KEY AUTOINCREMENT, project_id TEXT NOT NULL,
			kind TEXT NOT NULL, at TEXT NOT NULL, actor TEXT NOT NULL,
			payload TEXT NOT NULL, snap TEXT NOT NULL)`,
		`CREATE TABLE IF NOT EXISTS meta(k TEXT PRIMARY KEY, v TEXT NOT NULL)`,
	}
	for _, q := range stmts {
		if _, err := s.db.ExecContext(ctx, q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// CreateProject inserts a project (idempotent by id).
func (s *Store) CreateProject(ctx context.Context, p domain.Project) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO projects(id,name,created_at,seed) VALUES(?,?,?,?)`,
		p.ID, p.Name, p.CreatedAt, p.Seed)
	return err
}

func (s *Store) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,name,created_at,seed FROM projects WHERE id=?`, id)
	var p domain.Project
	if err := row.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.Seed); err != nil {
		return nil, err
	}
	return &p, nil
}

// PutSource stores an immutable source version. accepted=false preserves a
// rejected half of an import batch so half-failures are auditable.
func (s *Store) PutSource(ctx context.Context, src domain.Source, accepted bool) error {
	body, err := json.Marshal(src)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO sources(id,project_id,kind,version,parent_id,body,batch_id,accepted)
		 VALUES(?,?,?,?,?,?,?,?)`,
		src.ID, src.ProjectID, src.Kind, src.Version, nullIf(src.ParentID),
		string(body), nullIf(src.BatchID), b2i(accepted))
	return err
}

// ListSources returns all stored source versions for a project.
func (s *Store) ListSources(ctx context.Context, projectID string) ([]domain.Source, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT body FROM sources WHERE project_id=? ORDER BY kind, version`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Source
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var src domain.Source
		if err := json.Unmarshal([]byte(body), &src); err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// MarkSourceAccepted flips acceptance for a half-failure repair.
func (s *Store) MarkSourceAccepted(ctx context.Context, id string, accepted bool) error {
	_, err := s.db.ExecContext(ctx, `UPDATE sources SET accepted=? WHERE id=?`, b2i(accepted), id)
	return err
}

// PutNode upserts a derived node.
func (s *Store) PutNode(ctx context.Context, n domain.Node, current bool) error {
	body, err := json.Marshal(n)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO nodes(id,project_id,kind,current,body) VALUES(?,?,?,?,1)
		 ON CONFLICT(id) DO UPDATE SET current=excluded.current, body=excluded.body`,
		n.ID, n.ProjectID, n.Kind, b2i(current), string(body))
	return err
}

// Supersede marks an old node current=0 in favor of a new one and records why.
func (s *Store) Supersede(ctx context.Context, oldID, newID, reason string, seq int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`UPDATE nodes SET current=0, superseded_by=? WHERE id=?`, newID, oldID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT OR REPLACE INTO stale(node_id,reason,since_seq) VALUES(?,?,?)`,
		oldID, reason, seq); err != nil {
		return err
	}
	return tx.Commit()
}

// ListNodes returns nodes; currentOnly filters out superseded history.
func (s *Store) ListNodes(ctx context.Context, projectID string, currentOnly bool) ([]domain.Node, error) {
	q := `SELECT body FROM nodes WHERE project_id=?`
	if currentOnly {
		q += ` AND current=1`
	}
	rows, err := s.db.QueryContext(ctx, q, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Node
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var n domain.Node
		if err := json.Unmarshal([]byte(body), &n); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// AppendEvent writes an event with its snapshot and returns the assigned seq.
func (s *Store) AppendEvent(ctx context.Context, e *domain.Event) (int64, error) {
	payload, err := json.Marshal(e.Payload)
	if err != nil {
		return 0, err
	}
	snap, err := json.Marshal(e.Snap)
	if err != nil {
		return 0, err
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO events(project_id,kind,at,actor,payload,snap) VALUES(?,?,?,?,?,?)`,
		e.ProjectID, e.Kind, e.At, e.Actor, string(payload), string(snap))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ListEvents returns the full append-only history ordered by seq.
func (s *Store) ListEvents(ctx context.Context, projectID string) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT seq,kind,at,actor,payload,snap FROM events WHERE project_id=? ORDER BY seq`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var payload, snap string
		if err := rows.Scan(&e.Seq, &e.Kind, &e.At, &e.Actor, &payload, &snap); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(payload), &e.Payload); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(snap), &e.Snap); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// SetMeta/GetMeta persist bundle identity/hashes for export round trips.
func (s *Store) SetMeta(ctx context.Context, k, v string) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO meta(k,v) VALUES(?,?) ON CONFLICT(k) DO UPDATE SET v=excluded.v`, k, v)
	return err
}

func (s *Store) GetMeta(ctx context.Context, k string) (string, error) {
	row := s.db.QueryRowContext(ctx, `SELECT v FROM meta WHERE k=?`, k)
	var v string
	err := row.Scan(&v)
	return v, err
}

// AllProjects lists project ids, used by the UI switcher.
func (s *Store) AllProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,name,created_at,seed FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.Seed); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

func nullIf(s string) any {
	if s == "" {
		return nil
	}
	return s
}
