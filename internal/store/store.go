// Package store is the SQLite-backed append-only event store. Event ids and
// sequence numbers are part of the payload so identity and ordering survive
// export, database deletion and re-import unchanged.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

// DB wraps the event database.
type DB struct{ db *sql.DB }

// ProjectRow is a project record.
type ProjectRow struct {
	ID        string
	Key       string
	Name      string
	CreatedAt string
	HeadSeq   int64
	Status    string
	CasePack  string
}

// Open opens (creating if needed) a SQLite database at path.
func Open(ctx context.Context, path string) (*DB, error) {
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	d := &DB{db: db}
	if err := d.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return d, nil
}

// Close closes the database.
func (d *DB) Close() error { return d.db.Close() }

func (d *DB) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS projects(
			id TEXT PRIMARY KEY,
			project_key TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			created_at TEXT NOT NULL,
			head_seq INTEGER NOT NULL DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'open',
			casepack TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE TABLE IF NOT EXISTS events(
			id TEXT NOT NULL,
			project_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			kind TEXT NOT NULL,
			at TEXT NOT NULL,
			payload TEXT NOT NULL,
			PRIMARY KEY(project_id, seq)
		) WITHOUT ROWID`,
		`CREATE UNIQUE INDEX IF NOT EXISTS events_id_idx ON events(id)`,
		`CREATE TABLE IF NOT EXISTS archive_meta(
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL
		)`,
	}
	for _, s := range stmts {
		if _, err := d.db.ExecContext(ctx, s); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// CreateProject inserts a project. Fails if the id or key already exists.
func (d *DB) CreateProject(ctx context.Context, p ProjectRow) error {
	_, err := d.db.ExecContext(ctx,
		`INSERT INTO projects(id, project_key, name, created_at, head_seq, status, casepack)
		 VALUES(?,?,?,?,?,?,?)`,
		p.ID, p.Key, p.Name, p.CreatedAt, p.HeadSeq, p.Status, p.CasePack)
	return err
}

// SetProjectStatus updates lifecycle status.
func (d *DB) SetProjectStatus(ctx context.Context, id, status string) error {
	_, err := d.db.ExecContext(ctx, `UPDATE projects SET status=? WHERE id=?`, status, id)
	return err
}

// StoredEvent is an event row with decoded payload kept opaque.
type StoredEvent struct {
	ID        string
	ProjectID string
	Seq       int64
	Kind      string
	At        string
	Payload   string
}

// AppendEvent appends at expectedSeq (must equal current head+1).
func (d *DB) AppendEvent(ctx context.Context, e StoredEvent) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var head int64
	if err := tx.QueryRowContext(ctx,
		`SELECT head_seq FROM projects WHERE id=?`, e.ProjectID).Scan(&head); err != nil {
		return err
	}
	if e.Seq != head+1 {
		return fmt.Errorf("sequence gap: expected %d, got %d", head+1, e.Seq)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events(id, project_id, seq, kind, at, payload) VALUES(?,?,?,?,?,?)`,
		e.ID, e.ProjectID, e.Seq, e.Kind, e.At, e.Payload); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`UPDATE projects SET head_seq=? WHERE id=?`, e.Seq, e.ProjectID); err != nil {
		return err
	}
	return tx.Commit()
}

// Events returns events in sequence order, optionally up to seqTo (0 = all).
func (d *DB) Events(ctx context.Context, projectID string, seqTo int64) ([]StoredEvent, error) {
	q := `SELECT id, project_id, seq, kind, at, payload FROM events
	      WHERE project_id=?`
	args := []any{projectID}
	if seqTo > 0 {
		q += ` AND seq<=?`
		args = append(args, seqTo)
	}
	q += ` ORDER BY seq`
	rows, err := d.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StoredEvent
	for rows.Next() {
		var e StoredEvent
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Seq, &e.Kind, &e.At, &e.Payload); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Project fetches a project by id.
func (d *DB) Project(ctx context.Context, id string) (ProjectRow, error) {
	var p ProjectRow
	err := d.db.QueryRowContext(ctx,
		`SELECT id, project_key, name, created_at, head_seq, status, casepack
		 FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Key, &p.Name, &p.CreatedAt, &p.HeadSeq, &p.Status, &p.CasePack)
	return p, err
}

// ProjectByKey fetches a project by its stable key.
func (d *DB) ProjectByKey(ctx context.Context, key string) (ProjectRow, error) {
	var p ProjectRow
	err := d.db.QueryRowContext(ctx,
		`SELECT id, project_key, name, created_at, head_seq, status, casepack
		 FROM projects WHERE project_key=?`, key).
		Scan(&p.ID, &p.Key, &p.Name, &p.CreatedAt, &p.HeadSeq, &p.Status, &p.CasePack)
	return p, err
}

// ListProjects returns all projects.
func (d *DB) ListProjects(ctx context.Context) ([]ProjectRow, error) {
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, project_key, name, created_at, head_seq, status, casepack
		 FROM projects ORDER BY created_at, project_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		var p ProjectRow
		if err := rows.Scan(&p.ID, &p.Key, &p.Name, &p.CreatedAt,
			&p.HeadSeq, &p.Status, &p.CasePack); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Archive is the portable export format.
type Archive struct {
	Format   string        `json:"format"`
	Version  int           `json:"version"`
	Projects []ProjectRow  `json:"projects"`
	Events   []StoredEvent `json:"events"`
}

// ExportArchive writes every project and event deterministically.
func (d *DB) ExportArchive(ctx context.Context) (Archive, error) {
	projects, err := d.ListProjects(ctx)
	if err != nil {
		return Archive{}, err
	}
	rows, err := d.db.QueryContext(ctx,
		`SELECT id, project_id, seq, kind, at, payload FROM events
		 ORDER BY project_id, seq`)
	if err != nil {
		return Archive{}, err
	}
	defer rows.Close()
	var events []StoredEvent
	for rows.Next() {
		var e StoredEvent
		if err := rows.Scan(&e.ID, &e.ProjectID, &e.Seq, &e.Kind, &e.At, &e.Payload); err != nil {
			return Archive{}, err
		}
		events = append(events, e)
	}
	if err := rows.Err(); err != nil {
		return Archive{}, err
	}
	return Archive{Format: "tga-review-archive", Version: 1, Projects: projects, Events: events}, nil
}

// ImportArchive re-inserts projects and events idempotently, preserving ids and
// sequence numbers. Existing identical events are skipped; conflicts error.
func (d *DB) ImportArchive(ctx context.Context, a Archive) (added int, err error) {
	if a.Format != "tga-review-archive" {
		return 0, fmt.Errorf("unsupported archive format %q", a.Format)
	}
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	for _, p := range a.Projects {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO projects(id, project_key, name, created_at, head_seq, status, casepack)
			 VALUES(?,?,?,?,?,?,?)
			 ON CONFLICT(id) DO NOTHING`,
			p.ID, p.Key, p.Name, p.CreatedAt, p.HeadSeq, p.Status, p.CasePack); err != nil {
			return 0, err
		}
	}
	maxSeq := map[string]int64{}
	for _, e := range a.Events {
		var existingPayload, existingKind string
		err := tx.QueryRowContext(ctx,
			`SELECT kind, payload FROM events WHERE project_id=? AND seq=?`,
			e.ProjectID, e.Seq).Scan(&existingKind, &existingPayload)
		if err == nil {
			if existingKind != e.Kind || existingPayload != e.Payload {
				return 0, fmt.Errorf("conflicting event at %s seq %d", e.ProjectID, e.Seq)
			}
			continue
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO events(id, project_id, seq, kind, at, payload) VALUES(?,?,?,?,?,?)`,
			e.ID, e.ProjectID, e.Seq, e.Kind, e.At, e.Payload); err != nil {
			return 0, err
		}
		added++
		if e.Seq > maxSeq[e.ProjectID] {
			maxSeq[e.ProjectID] = e.Seq
		}
	}
	for pid, seq := range maxSeq {
		if _, err := tx.ExecContext(ctx,
			`UPDATE projects SET head_seq=(SELECT MAX(head_seq,?) FROM projects WHERE id=?)
			 WHERE id=?`, seq, pid, pid); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return added, nil
}

// MarshalArchive is a small helper for handlers.
func MarshalArchive(a Archive) ([]byte, error) { return json.MarshalIndent(a, "", "  ") }
