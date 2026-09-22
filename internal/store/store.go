// Package store is the SQLite-backed, append-only persistence layer.
// Events are the source of truth; derived nodes are a re-creatable cache.
// Identity is content-based, so an exported -> delete -> imported archive
// keeps every id, ordering and provenance relation.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"

	"tga-archive/internal/domain"
)

type Clock func() string

type Store struct {
	db    *sql.DB
	clock Clock
}

func Open(path string, clock Clock) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{db: db, clock: clock}
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
	id TEXT PRIMARY KEY,
	name TEXT NOT NULL,
	case_id TEXT NOT NULL DEFAULT '',
	seed INTEGER NOT NULL DEFAULT 0,
	created_at TEXT NOT NULL,
	note TEXT NOT NULL DEFAULT ''
);
CREATE TABLE IF NOT EXISTS events (
	seq INTEGER PRIMARY KEY AUTOINCREMENT,
	id TEXT NOT NULL UNIQUE,
	project TEXT NOT NULL REFERENCES projects(id),
	type TEXT NOT NULL,
	at TEXT NOT NULL,
	parent TEXT NOT NULL DEFAULT '',
	payload TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_events_project ON events(project, seq);
CREATE TABLE IF NOT EXISTS derived_nodes (
	id TEXT NOT NULL,
	project TEXT NOT NULL REFERENCES projects(id),
	kind TEXT NOT NULL,
	first_seen_seq INTEGER NOT NULL,
	stale INTEGER NOT NULL DEFAULT 0,
	payload TEXT NOT NULL,
	PRIMARY KEY(id, project)
);
CREATE INDEX IF NOT EXISTS idx_nodes_project ON derived_nodes(project, stale);
CREATE TABLE IF NOT EXISTS kv (
	project TEXT NOT NULL,
	key TEXT NOT NULL,
	value TEXT NOT NULL,
	PRIMARY KEY(project, key)
);
`

// Project metadata.
type ProjectInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CaseID    string `json:"caseId"`
	Seed      int64  `json:"seed"`
	CreatedAt string `json:"createdAt"`
	Note      string `json:"note"`
}

func (s *Store) CreateProject(p domain.ProjectPayload, id, at string) (ProjectInfo, error) {
	info := ProjectInfo{ID: id, Name: p.Name, CaseID: p.CaseID, Seed: p.Seed, CreatedAt: at, Note: p.Note}
	_, err := s.db.Exec(`INSERT INTO projects(id,name,case_id,seed,created_at,note) VALUES(?,?,?,?,?,?)`,
		id, info.Name, info.CaseID, info.Seed, at, info.Note)
	if err != nil {
		return ProjectInfo{}, err
	}
	payload, _ := json.Marshal(p)
	ev := domain.Envelope{Project: id, Type: domain.EvProjectCreated, At: at, Payload: payload}
	ev.ID = domain.EventID(id, ev.Type, at, "", p)
	return info, s.insertEvent(ev, payload)
}

func (s *Store) insertEvent(ev domain.Envelope, payload []byte) error {
	_, err := s.db.Exec(`INSERT INTO events(id,project,type,at,parent,payload) VALUES(?,?,?,?,?,?)`,
		ev.ID, ev.Project, ev.Type, ev.At, ev.Parent, string(payload))
	return err
}

// AppendEvent stores an event. ID is derived from content (not seq), so the
// same sequence of decisions yields identical ids on a fresh database.
func (s *Store) AppendEvent(projectID, typ, parent string, payload any) (domain.Envelope, error) {
	at := s.clock()
	id := domain.EventID(projectID, typ, at, parent, payload)
	raw, _ := json.Marshal(payload)
	ev := domain.Envelope{ID: id, Project: projectID, Type: typ, At: at, Parent: parent, Payload: raw}
	err := s.insertEvent(ev, raw)
	return ev, err
}

var ErrNotFound = errors.New("not found")

func (s *Store) Project(id string) (ProjectInfo, error) {
	row := s.db.QueryRow(`SELECT id,name,case_id,seed,created_at,note FROM projects WHERE id=?`, id)
	var p ProjectInfo
	if err := row.Scan(&p.ID, &p.Name, &p.CaseID, &p.Seed, &p.CreatedAt, &p.Note); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ProjectInfo{}, ErrNotFound
		}
		return ProjectInfo{}, err
	}
	return p, nil
}

func (s *Store) Projects() ([]ProjectInfo, error) {
	rows, err := s.db.Query(`SELECT id,name,case_id,seed,created_at,note FROM projects ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectInfo
	for rows.Next() {
		var p ProjectInfo
		if err := rows.Scan(&p.ID, &p.Name, &p.CaseID, &p.Seed, &p.CreatedAt, &p.Note); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// Events returns all envelopes of a project in canonical order.
func (s *Store) Events(projectID string) ([]domain.Envelope, error) {
	rows, err := s.db.Query(`SELECT seq,id,project,type,at,parent,payload FROM events WHERE project=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

// EventsUntil replays the prefix up to and including seq.
func (s *Store) EventsUntil(projectID string, seq int64) ([]domain.Envelope, error) {
	rows, err := s.db.Query(`SELECT seq,id,project,type,at,parent,payload FROM events WHERE project=? AND seq<=? ORDER BY seq`, projectID, seq)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanEvents(rows)
}

func scanEvents(rows *sql.Rows) ([]domain.Envelope, error) {
	var out []domain.Envelope
	for rows.Next() {
		var ev domain.Envelope
		var raw string
		if err := rows.Scan(&ev.Seq, &ev.ID, &ev.Project, &ev.Type, &ev.At, &ev.Parent, &raw); err != nil {
			return nil, err
		}
		ev.Payload = json.RawMessage(raw)
		out = append(out, ev)
	}
	return out, rows.Err()
}

func (s *Store) LastSeq(projectID string) (int64, error) {
	var seq sql.NullInt64
	err := s.db.QueryRow(`SELECT MAX(seq) FROM events WHERE project=?`, projectID).Scan(&seq)
	if err != nil {
		return 0, err
	}
	return seq.Int64, nil
}

// UpsertNodes stores the current derived node set and marks missing ones stale.
func (s *Store) UpsertNodes(ctx context.Context, projectID string, seq int64, nodes map[string]json.RawMessage) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for id, payload := range nodes {
		kind := idKind(id)
		_, err := tx.ExecContext(ctx, `INSERT INTO derived_nodes(id,project,kind,first_seen_seq,stale,payload)
			VALUES(?,?,?,?,0,?)
			ON CONFLICT(id,project) DO UPDATE SET stale=0, payload=excluded.payload`,
			id, projectID, kind, seq, string(payload))
		if err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE derived_nodes SET stale=1 WHERE project=? AND id NOT IN (SELECT value FROM json_each(?))`,
		projectID, mustJSONSet(nodes)); err != nil && !isJSON1Missing(err) {
		return err
	}
	return tx.Commit()
}

func mustJSONSet(nodes map[string]json.RawMessage) string {
	ids := make([]string, 0, len(nodes))
	for id := range nodes {
		ids = append(ids, id)
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

// modernc sqlite includes the JSON1 extension; if unavailable the stale sweep
// simply becomes a no-op (correctness does not depend on it).
func isJSON1Missing(err error) bool {
	return err != nil && (contains(err.Error(), "no such function: json_each"))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}

func idKind(id string) string {
	for i := 0; i < len(id); i++ {
		if id[i] == ':' {
			return id[:i]
		}
	}
	return "node"
}

// Node fetches one cached derived node (even stale ones).
func (s *Store) Node(projectID, id string) (json.RawMessage, bool, error) {
	var payload string
	var stale int
	err := s.db.QueryRow(`SELECT payload,stale FROM derived_nodes WHERE project=? AND id=?`, projectID, id).Scan(&payload, &stale)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	_ = stale
	return json.RawMessage(payload), true, nil
}

// NodeCount is used by tests.
func (s *Store) NodeCount(projectID string) (int, int, error) {
	var total, stale int
	if err := s.db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(stale),0) FROM derived_nodes WHERE project=?`, projectID).Scan(&total, &stale); err != nil {
		return 0, 0, err
	}
	return total, stale, nil
}

func (s *Store) SetKV(projectID, key, value string) error {
	_, err := s.db.Exec(`INSERT INTO kv(project,key,value) VALUES(?,?,?)
		ON CONFLICT(project,key) DO UPDATE SET value=excluded.value`, projectID, key, value)
	return err
}

func (s *Store) GetKV(projectID, key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM kv WHERE project=? AND key=?`, projectID, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return v, err
}

func (s *Store) String() string { return fmt.Sprintf("store(%p)", s) }
