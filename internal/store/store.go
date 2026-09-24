// Package store is the SQLite-backed append-only journal plus a
// content-addressed artifact table. The journal is a hash chain; artifacts are
// immutable and keyed by content so re-running an identical analysis never
// changes any identity.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"
)

type Store struct {
	DB *sql.DB
}

func Open(ctx context.Context, dsn string) (*Store, error) {
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	s := &Store{DB: db}
	if err := s.migrate(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.DB.Close() }

func (s *Store) migrate(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS projects (
  project_id  TEXT PRIMARY KEY,
  name        TEXT NOT NULL,
  created_at  TEXT NOT NULL,
  deleted     INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS events (
  project_id  TEXT NOT NULL,
  seq         INTEGER NOT NULL,
  id          TEXT NOT NULL,
  kind        TEXT NOT NULL,
  at          TEXT NOT NULL,
  actor       TEXT NOT NULL,
  parent_id   TEXT NOT NULL,
  payload     BLOB NOT NULL,
  hash        TEXT NOT NULL,
  PRIMARY KEY (project_id, seq)
);
CREATE TABLE IF NOT EXISTS artifacts (
  project_id  TEXT NOT NULL,
  artifact_id TEXT NOT NULL,
  kind        TEXT NOT NULL,
  created_seq INTEGER NOT NULL,
  content     BLOB NOT NULL,
  PRIMARY KEY (project_id, artifact_id)
);
CREATE TABLE IF NOT EXISTS artifact_refs (
  project_id  TEXT NOT NULL,
  artifact_id TEXT NOT NULL,
  ref_kind    TEXT NOT NULL,
  ref_id      TEXT NOT NULL,
  PRIMARY KEY (project_id, artifact_id, ref_kind, ref_id)
);
CREATE TABLE IF NOT EXISTS kv_meta (
  project_id  TEXT NOT NULL,
  key         TEXT NOT NULL,
  value       TEXT NOT NULL,
  PRIMARY KEY (project_id, key)
);
`)
	return err
}

// AppendEvent writes one journal entry. parentID chains to the previous event;
// the hash commits over (project, seq, kind, payload, parent) so tampering with
// history invalidates the chain.
func (s *Store) AppendEvent(ctx context.Context, projectID, kind, actor string, payload any, at string) (map[string]any, string, int64, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, "", 0, err
	}
	defer tx.Rollback()
	var seq int64
	if err := tx.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq)+1,0) FROM events WHERE project_id=?`, projectID).Scan(&seq); err != nil {
		return nil, "", 0, err
	}
	var parentID string
	_ = tx.QueryRowContext(ctx,
		`SELECT id FROM events WHERE project_id=? ORDER BY seq DESC LIMIT 1`, projectID).Scan(&parentID)
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", 0, err
	}
	id := SHAID("evt", projectID, seq, kind, string(body))
	hash := Hash([]byte(projectID + "|" + fmt.Sprint(seq) + "|" + kind + "|" + parentID + "|" + string(body)))
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO events(project_id,seq,id,kind,at,actor,parent_id,payload,hash)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		projectID, seq, id, kind, at, actor, parentID, body, hash); err != nil {
		return nil, "", 0, err
	}
	if err := tx.Commit(); err != nil {
		return nil, "", 0, err
	}
	var m map[string]any
	_ = json.Unmarshal(body, &m)
	return m, id, seq, nil
}

func (s *Store) PutArtifact(ctx context.Context, projectID, artifactID, kind string, createdSeq int, content any, refs map[string]string) error {
	body, err := json.Marshal(content)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`INSERT OR IGNORE INTO artifacts(project_id,artifact_id,kind,created_seq,content) VALUES(?,?,?,?,?)`,
		projectID, artifactID, kind, createdSeq, body); err != nil {
		return err
	}
	for rk, rid := range refs {
		if _, err := tx.ExecContext(ctx,
			`INSERT OR IGNORE INTO artifact_refs(project_id,artifact_id,ref_kind,ref_id) VALUES(?,?,?,?)`,
			projectID, artifactID, rk, rid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Artifact(ctx context.Context, projectID, artifactID string, out any) (bool, error) {
	var body []byte
	err := s.DB.QueryRowContext(ctx,
		`SELECT content FROM artifacts WHERE project_id=? AND artifact_id=?`, projectID, artifactID).Scan(&body)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, json.Unmarshal(body, out)
}

func (s *Store) ArtifactRefs(ctx context.Context, projectID, artifactID string) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT ref_kind,ref_id FROM artifact_refs WHERE project_id=? AND artifact_id=?`, projectID, artifactID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

func (s *Store) ArtifactsByKind(ctx context.Context, projectID, kind string) ([][]byte, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT content FROM artifacts WHERE project_id=? AND kind=? ORDER BY created_seq`, projectID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][]byte
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, nil
}

func (s *Store) CreateProject(ctx context.Context, id, name, at string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO projects(project_id,name,created_at) VALUES(?,?,?)`, id, name, at)
	return err
}

func (s *Store) ProjectExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects WHERE project_id=? AND deleted=0`, id).Scan(&n)
	return n > 0, err
}

func (s *Store) DeleteProject(ctx context.Context, id string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE projects SET deleted=1 WHERE project_id=?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

type ProjectRow struct {
	ID        string `json:"project_id"`
	Name      string `json:"name"`
	CreatedAt string `json:"created_at"`
}

func (s *Store) ListProjects(ctx context.Context) ([]ProjectRow, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT project_id,name,created_at FROM projects WHERE deleted=0 ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		var p ProjectRow
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

type EventRow struct {
	Seq      int64           `json:"seq"`
	ID       string          `json:"id"`
	Kind     string          `json:"kind"`
	At       string          `json:"at"`
	Actor    string          `json:"actor"`
	ParentID string          `json:"parent_id"`
	Payload  json.RawMessage `json:"payload"`
	Hash     string          `json:"hash"`
}

func (s *Store) Events(ctx context.Context, projectID string, upToSeq int64) ([]EventRow, error) {
	q := `SELECT seq,id,kind,at,actor,parent_id,payload,hash FROM events WHERE project_id=?`
	args := []any{projectID}
	if upToSeq > 0 {
		q += ` AND seq<=?`
		args = append(args, upToSeq)
	}
	q += ` ORDER BY seq`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRow
	for rows.Next() {
		var e EventRow
		if err := rows.Scan(&e.Seq, &e.ID, &e.Kind, &e.At, &e.Actor, &e.ParentID, &e.Payload, &e.Hash); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, nil
}

// VerifyChain replays the hash chain and returns the first broken link.
func (s *Store) VerifyChain(ctx context.Context, projectID string) error {
	evs, err := s.Events(ctx, projectID, 0)
	if err != nil {
		return err
	}
	parent := ""
	for _, e := range evs {
		want := Hash([]byte(projectID + "|" + fmt.Sprint(e.Seq) + "|" + e.Kind + "|" + parent + "|" + string(e.Payload)))
		if want != e.Hash {
			return fmt.Errorf("event %d hash mismatch", e.Seq)
		}
		if e.ParentID != parent {
			return fmt.Errorf("event %d parent mismatch", e.Seq)
		}
		parent = e.ID
	}
	return nil
}

func (s *Store) SetMeta(ctx context.Context, projectID, key, value string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO kv_meta(project_id,key,value) VALUES(?,?,?)
		 ON CONFLICT(project_id,key) DO UPDATE SET value=excluded.value`,
		projectID, key, value)
	return err
}

func (s *Store) GetMeta(ctx context.Context, projectID, key string) (string, bool, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM kv_meta WHERE project_id=? AND key=?`, projectID, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// RawTables is used by export.
func (s *Store) RawTables() *sql.DB { return s.DB }
