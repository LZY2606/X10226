package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	_ "github.com/mattn/go-sqlite3"

	"tga-review/internal/tga"
)

type Store struct {
	db *sql.DB
}

type SourceInput struct {
	Kind    string                 `json:"kind"`
	Label   string                 `json:"label"`
	Raw     string                 `json:"raw"`
	Payload map[string]interface{} `json:"payload"`
	Status  string                 `json:"status"`
	Error   string                 `json:"error"`
}

type Project struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	Params    tga.Params `json:"params"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type State struct {
	Project   Project          `json:"project"`
	Sources   []tga.Source     `json:"sources"`
	Nodes     []tga.Node       `json:"nodes"`
	Decisions []tga.Decision   `json:"decisions"`
	Events    []Event          `json:"events"`
	Judgments []tga.Judgment   `json:"judgments"`
	InputFailures []SourceInput `json:"input_failures,omitempty"`
}

type Event struct {
	ID         int64           `json:"id"`
	ProjectID  string          `json:"project_id"`
	Type       string          `json:"type"`
	Summary    string          `json:"summary"`
	SourceID   string          `json:"source_id,omitempty"`
	Payload    json.RawMessage `json:"payload,omitempty"`
	Snapshot   json.RawMessage `json:"snapshot,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite3", path+"?_foreign_keys=on&_busy_timeout=5000")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, schema)
	return err
}

const schema = `
CREATE TABLE IF NOT EXISTS projects (
  id TEXT PRIMARY KEY,
  name TEXT NOT NULL,
  params_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS sources (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  kind TEXT NOT NULL,
  version INTEGER NOT NULL,
  parent_id TEXT,
  label TEXT NOT NULL,
  raw_text TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  status TEXT NOT NULL,
  error_text TEXT NOT NULL,
  seq INTEGER NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(project_id, kind, version)
);
CREATE TABLE IF NOT EXISTS nodes (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  node_key TEXT NOT NULL,
  version INTEGER NOT NULL,
  display_id TEXT NOT NULL,
  status TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  created_at TEXT NOT NULL,
  UNIQUE(project_id, node_key, version)
);
CREATE TABLE IF NOT EXISTS decisions (
  id TEXT PRIMARY KEY,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  event_id INTEGER NOT NULL,
  candidate_key TEXT NOT NULL,
  stage INTEGER NOT NULL,
  alternative INTEGER NOT NULL,
  action TEXT NOT NULL,
  reason TEXT NOT NULL,
  parameters_json TEXT NOT NULL,
  conflict_json TEXT NOT NULL,
  superseded_by TEXT NOT NULL,
  status TEXT NOT NULL DEFAULT 'current',
  created_at TEXT NOT NULL,
  UNIQUE(project_id, candidate_key)
);
CREATE TABLE IF NOT EXISTS events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  project_id TEXT NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
  type TEXT NOT NULL,
  summary TEXT NOT NULL,
  source_id TEXT NOT NULL,
  payload_json TEXT NOT NULL,
  snapshot_json TEXT NOT NULL,
  created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_sources_project ON sources(project_id, seq);
CREATE INDEX IF NOT EXISTS idx_nodes_project ON nodes(project_id, node_key, version);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, id);
`
