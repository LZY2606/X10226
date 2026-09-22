// Package store is the SQLite persistence layer. Only the append-only event
// log is source of truth; layer raw content is kept immutable on revisions.
// Derived artifacts are cached with their basis so exports stay self-contained.
package store

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, err
	}
	s := &Store{DB: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id          TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  description TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS events (
  project_id TEXT NOT NULL REFERENCES projects(id),
  seq        INTEGER NOT NULL,
  type       TEXT NOT NULL,
  at         TEXT NOT NULL,
  payload    TEXT NOT NULL,
  PRIMARY KEY (project_id, seq)
);

CREATE TABLE IF NOT EXISTS layer_revisions (
  project_id   TEXT NOT NULL REFERENCES projects(id),
  rev_id       TEXT NOT NULL,
  kind         TEXT NOT NULL,
  parent_rev   TEXT NOT NULL DEFAULT '',
  seq          INTEGER NOT NULL,
  source_file  TEXT NOT NULL DEFAULT '',
  raw_canonical TEXT NOT NULL,
  status       TEXT NOT NULL DEFAULT 'accepted',
  PRIMARY KEY (project_id, rev_id)
);

CREATE TABLE IF NOT EXISTS fragments (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id  TEXT NOT NULL,
  seq         INTEGER NOT NULL,
  kind        TEXT NOT NULL,
  row_idx     INTEGER,
  field       TEXT NOT NULL DEFAULT '',
  message     TEXT NOT NULL,
  raw_value   TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS artifacts (
  project_id  TEXT NOT NULL,
  artifact_id TEXT NOT NULL,
  basis_seq   INTEGER NOT NULL,
  alg_version TEXT NOT NULL,
  params_fp   TEXT NOT NULL,
  content     TEXT NOT NULL,
  current     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (project_id, artifact_id)
);

CREATE TABLE IF NOT EXISTS decisions (
  project_id TEXT NOT NULL,
  id         TEXT NOT NULL,
  seq        INTEGER NOT NULL,
  artifact_id TEXT NOT NULL,
  target_id  TEXT NOT NULL,
  kind       TEXT NOT NULL,
  value      TEXT NOT NULL DEFAULT '',
  rationale  TEXT NOT NULL DEFAULT '',
  supersedes TEXT NOT NULL DEFAULT '',
  undone     INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (project_id, id)
);
`

func (s *Store) migrate() error {
	_, err := s.DB.Exec(schema)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}
