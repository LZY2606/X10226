package store

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

// Clock is the time source. Tests inject a fake clock so every timestamp is
// deterministic; the server uses wall time.
type Clock interface{ Now() int64 }

type WallClock struct{}

func (WallClock) Now() int64 { return nowUnix() }

// Store is the SQLite-backed event log plus read projections.
type Store struct {
	db    *sql.DB
	clock Clock
}

func Open(path string, clock Clock) (*Store, error) {
	if clock == nil {
		clock = WallClock{}
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db, clock: clock}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

var ErrExists = errors.New("already exists")
var ErrNotFound = errors.New("not found")

// CreateProject inserts the project row and its genesis event atomically.
func (s *Store) CreateProject(id, name, sourceFP string) (*StoredEvent, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM projects WHERE id=? OR source_fp=?`, id, sourceFP).Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, ErrExists
	}
	if _, err := tx.Exec(`INSERT INTO projects(id,name,created_at,last_seq,last_hash,source_fp)
		VALUES(?,?,?,0,'',?)`, id, name, s.clock.Now(), sourceFP); err != nil {
		return nil, err
	}
	ev, err := appendTx(tx, s.clock.Now(), id, "project_created",
		map[string]interface{}{"name": name, "source_fp": sourceFP}, "")
	if err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE projects SET created_seq=? WHERE id=?`, ev.Seq, id); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ev, nil
}

type ProjectRow struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LastSeq  int64  `json:"last_seq"`
	SourceFP string `json:"source_fp"`
}

func (s *Store) ListProjects() ([]ProjectRow, error) {
	rows, err := s.db.Query(`SELECT id,name,last_seq,source_fp FROM projects ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		var p ProjectRow
		if err := rows.Scan(&p.ID, &p.Name, &p.LastSeq, &p.SourceFP); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) ProjectExists(id string) (bool, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM projects WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

// StoredEvent is an event as persisted.
type StoredEvent struct {
	Seq        int64                  `json:"seq"`
	Project    string                 `json:"project"`
	Kind       string                 `json:"kind"`
	OccurredAt int64                  `json:"occurred_at"`
	ClientID   string                 `json:"client_id,omitempty"`
	Payload    map[string]interface{} `json:"payload"`
	PrevHash   string                 `json:"prev_hash"`
	Hash       string                 `json:"hash"`
}

// AppendEvent appends one event with hash chaining and updates projections.
// A non-empty clientID makes the append idempotent (replays no-op).
func (s *Store) AppendEvent(project, kind string, payload map[string]interface{}, clientID string) (*StoredEvent, error) {
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	if clientID != "" {
		row := tx.QueryRow(`SELECT seq,kind,occurred_at,client_id,payload,prev_hash,hash FROM events
			WHERE project=? AND client_id=?`, project, clientID)
		ev, err := scanEvent(row, project)
		if err == nil {
			return ev, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	ev, err := appendTx(tx, s.clock.Now(), project, kind, payload, clientID)
	if err != nil {
		return nil, err
	}
	if err := projectEvent(tx, ev); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return ev, nil
}

func appendTx(tx *sql.Tx, now int64, project, kind string, payload map[string]interface{}, clientID string) (*StoredEvent, error) {
	var lastSeq int64
	var lastHash string
	row := tx.QueryRow(`SELECT last_seq,last_hash FROM projects WHERE id=?`, project)
	if err := row.Scan(&lastSeq, &lastHash); err != nil {
		return nil, fmt.Errorf("project %s: %w", project, err)
	}
	pj, err := canonicalJSON(payload)
	if err != nil {
		return nil, err
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d\n%s\n%s\n%d\n%s\n%s\n%s",
		lastSeq+1, project, kind, now, clientID, string(pj), lastHash)))
	hash := hex.EncodeToString(sum[:])
	if _, err := tx.Exec(`INSERT INTO events(seq,project,kind,occurred_at,client_id,payload,prev_hash,hash)
		VALUES(?,?,?,?,?,?,?,?)`, lastSeq+1, project, kind, now, clientID, string(pj), lastHash, hash); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`UPDATE projects SET last_seq=?,last_hash=? WHERE id=?`, lastSeq+1, hash, project); err != nil {
		return nil, err
	}
	return &StoredEvent{
		Seq: lastSeq + 1, Project: project, Kind: kind, OccurredAt: now,
		ClientID: clientID, Payload: payload, PrevHash: lastHash, Hash: hash,
	}, nil
}

type rowScanner interface {
	Scan(dest ...interface{}) error
}

func scanEvent(row rowScanner, project string) (*StoredEvent, error) {
	var seq, occurred int64
	var kind, clientID, payloadJSON, prevHash, hash string
	if err := row.Scan(&seq, &kind, &occurred, &clientID, &payloadJSON, &prevHash, &hash); err != nil {
		return nil, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		return nil, err
	}
	return &StoredEvent{Seq: seq, Project: project, Kind: kind, OccurredAt: occurred,
		ClientID: clientID, Payload: payload, PrevHash: prevHash, Hash: hash}, nil
}

// Events returns the full ordered event log of a project.
func (s *Store) Events(project string) ([]*StoredEvent, error) {
	rows, err := s.db.Query(`SELECT seq,kind,occurred_at,client_id,payload,prev_hash,hash
		FROM events WHERE project=? ORDER BY seq`, project)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*StoredEvent
	for rows.Next() {
		ev, err := scanEvent(rows, project)
		if err != nil {
			return nil, err
		}
		out = append(out, ev)
	}
	return out, rows.Err()
}

func canonicalJSON(v map[string]interface{}) ([]byte, error) {
	return json.Marshal(canonical(v))
}

func canonical(v interface{}) interface{} {
	switch t := v.(type) {
	case map[string]interface{}:
		m := make(map[string]interface{}, len(t))
		for k, val := range t {
			m[k] = canonical(val)
		}
		return m
	case []interface{}:
		out := make([]interface{}, len(t))
		for i := range t {
			out[i] = canonical(t[i])
		}
		return out
	default:
		return v
	}
}
