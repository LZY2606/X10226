// Package store persists the append-only event log, immutable layers and
// content-addressed derived nodes in SQLite.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"tgaline/internal/domain"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")
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

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS projects(
			id TEXT PRIMARY KEY, name TEXT NOT NULL, created_at TEXT NOT NULL, meta TEXT NOT NULL DEFAULT '{}'
		)`,
		`CREATE TABLE IF NOT EXISTS events(
			seq INTEGER NOT NULL, project_id TEXT NOT NULL, id TEXT NOT NULL,
			type TEXT NOT NULL, at TEXT NOT NULL, actor TEXT NOT NULL,
			parent_id TEXT NOT NULL, payload TEXT NOT NULL,
			PRIMARY KEY(project_id, seq)
		)`,
		`CREATE TABLE IF NOT EXISTS layers(
			id TEXT PRIMARY KEY, project_id TEXT NOT NULL, kind TEXT NOT NULL,
			version INTEGER NOT NULL, parent_id TEXT NOT NULL, origin TEXT NOT NULL,
			sha TEXT NOT NULL, body TEXT NOT NULL, created_at TEXT NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS nodes(
			id TEXT PRIMARY KEY, kind TEXT NOT NULL, algo_version TEXT NOT NULL,
			params_fp TEXT NOT NULL, sources TEXT NOT NULL, body TEXT NOT NULL,
			status TEXT NOT NULL, notes TEXT NOT NULL, project_hint TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, seq)`,
		`CREATE INDEX IF NOT EXISTS idx_layers_project ON layers(project_id, version)`,
		`CREATE INDEX IF NOT EXISTS idx_nodes_hint ON nodes(project_hint)`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w\n%s", err, q)
		}
	}
	return nil
}

func (s *Store) CreateProject(id, name, createdAt, metaJSON string) error {
	_, err := s.db.Exec(
		`INSERT OR IGNORE INTO projects(id,name,created_at,meta) VALUES(?,?,?,?)`,
		id, name, createdAt, metaJSON)
	return err
}

func (s *Store) ListProjects() ([]ProjectRow, error) {
	rows, err := s.db.Query(`SELECT id,name,created_at FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		var r ProjectRow
		if err := rows.Scan(&r.ID, &r.Name, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type ProjectRow struct {
	ID, Name, CreatedAt string
}

var _ domain.Dimension
