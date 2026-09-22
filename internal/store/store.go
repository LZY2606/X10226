// Package store is the SQLite-backed, append-only persistence for the TGA
// review system. All identity is deterministic (project/seq based) so that
// export -> delete database -> import preserves identity, order and source
// relations. Time travel reconstructs any prior event state.
package store

import (
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"

	"tga-review/internal/domain"
)

// Clock allows tests to drive virtual time.
type Clock func() time.Time

// Store wraps the SQLite database.
type Store struct {
	db *sql.DB
	cl Clock
}

func Open(path string, cl Clock) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	if cl == nil {
		cl = time.Now
	}
	s := &Store{db: db, cl: cl}
	if err := s.migrate(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	_, err := s.db.Exec(schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY, seq INTEGER NOT NULL, name TEXT NOT NULL,
  material TEXT NOT NULL, note TEXT NOT NULL DEFAULT '',
  created_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS project_seq (n INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS events (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, seq INTEGER NOT NULL,
  type TEXT NOT NULL, at TEXT NOT NULL, payload TEXT NOT NULL DEFAULT '{}'
);
CREATE TABLE IF NOT EXISTS submissions (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, seq INTEGER NOT NULL,
  layer TEXT NOT NULL, kind TEXT NOT NULL, parent_id TEXT NOT NULL DEFAULT '',
  source_name TEXT NOT NULL, payload_hash TEXT NOT NULL,
  partial INTEGER NOT NULL DEFAULT 0, mutation TEXT NOT NULL DEFAULT '',
  note TEXT NOT NULL DEFAULT '', created_at TEXT NOT NULL, data TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS derivations (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, seq INTEGER NOT NULL,
  event_id TEXT NOT NULL, data TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY, project_id TEXT NOT NULL, seq INTEGER NOT NULL,
  event_id TEXT NOT NULL, target_kind TEXT NOT NULL, target_key TEXT NOT NULL,
  derivation_id TEXT NOT NULL, action TEXT NOT NULL,
  adjustments TEXT NOT NULL DEFAULT '{}', reason TEXT NOT NULL DEFAULT '',
  supersedes_id TEXT NOT NULL DEFAULT '', active INTEGER NOT NULL DEFAULT 1,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, seq);
CREATE INDEX IF NOT EXISTS idx_sub_project ON submissions(project_id, seq);
CREATE INDEX IF NOT EXISTS idx_der_project ON derivations(project_id, seq);
CREATE INDEX IF NOT EXISTS idx_dec_project ON decisions(project_id, seq);
`

func (s *Store) now() time.Time { return s.cl() }

// CreateProject appends a project-created event and allocates a stable id.
func (s *Store) CreateProject(name, material, note string) (*domain.Project, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	row := tx.QueryRow(`SELECT n FROM project_seq`)
	if err := row.Scan(&n); err != nil {
		if _, err2 := tx.Exec(`INSERT INTO project_seq(n) VALUES(1)`); err2 != nil {
			return nil, err2
		}
		n = 1
	} else {
		n++
		if _, err := tx.Exec(`UPDATE project_seq SET n = ?`, n); err != nil {
			return nil, err
		}
	}
	pid := domain.ProjectStableID(n)
	p := &domain.Project{ID: pid, Name: name, Material: material, Note: note, CreatedAt: s.now()}
	if _, err := tx.Exec(
		`INSERT INTO projects(id, seq, name, material, note, created_at) VALUES(?,?,?,?,?,?)`,
		p.ID, n, p.Name, p.Material, p.Note, p.CreatedAt.Format(time.RFC3339Nano)); err != nil {
		return nil, err
	}
	if _, err := appendEvent(tx, p.ID, 1, "project_created", s.now(), map[string]any{
		"name": name, "material": material, "note": note,
	}); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return p, nil
}

func (s *Store) NextSeq(table, pid string) (int, error) {
	var n int
	row := s.db.QueryRow(fmt.Sprintf(`SELECT COALESCE(MAX(seq),0)+1 FROM %s WHERE project_id=?`, table), pid)
	if err := row.Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Store) nextEventSeq(tx *sql.Tx, pid string) (int, error) {
	var n int
	if err := tx.QueryRow(`SELECT COALESCE(MAX(seq),0)+1 FROM events WHERE project_id=?`, pid).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ListProjects returns all projects.
func (s *Store) ListProjects() ([]domain.Project, error) {
	rows, err := s.db.Query(`SELECT id, name, material, note, created_at FROM projects ORDER BY seq`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		var ct string
		if err := rows.Scan(&p.ID, &p.Name, &p.Material, &p.Note, &ct); err != nil {
			return nil, err
		}
		p.CreatedAt, _ = time.Parse(time.RFC3339Nano, ct)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProject(pid string) (*domain.Project, error) {
	var p domain.Project
	var ct string
	err := s.db.QueryRow(`SELECT id, name, material, note, created_at FROM projects WHERE id=?`, pid).
		Scan(&p.ID, &p.Name, &p.Material, &p.Note, &ct)
	if err != nil {
		return nil, err
	}
	p.CreatedAt, _ = time.Parse(time.RFC3339Nano, ct)
	return &p, nil
}
