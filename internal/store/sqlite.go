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
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)")
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	st := &Store{db: db}
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	return st, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) migrate() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS projects (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT NOT NULL DEFAULT '',
			labels TEXT NOT NULL DEFAULT '{}',
			created_at TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS events (
			project_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			at TEXT NOT NULL,
			actor TEXT NOT NULL,
			parent_id TEXT NOT NULL DEFAULT '',
			payload TEXT NOT NULL,
			undone_by TEXT NOT NULL DEFAULT '',
			UNIQUE(project_id, seq)
		);`,
		`CREATE TABLE IF NOT EXISTS params (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			parent_id TEXT NOT NULL DEFAULT '',
			body TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS versions (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			layer TEXT NOT NULL,
			version INTEGER NOT NULL,
			parent_version TEXT NOT NULL DEFAULT '',
			media_type TEXT NOT NULL,
			raw_text TEXT NOT NULL,
			sha256 TEXT NOT NULL,
			committed_at TEXT NOT NULL,
			commit_event_id TEXT NOT NULL,
			body TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS diagnostics (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			session_id TEXT NOT NULL DEFAULT '',
			layer TEXT NOT NULL,
			row INTEGER NOT NULL,
			field TEXT NOT NULL,
			token TEXT NOT NULL,
			severity TEXT NOT NULL,
			message TEXT NOT NULL
		);`,
		`CREATE TABLE IF NOT EXISTS derived (
			id TEXT PRIMARY KEY,
			project_id TEXT NOT NULL,
			batch_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			fingerprint TEXT NOT NULL,
			body TEXT NOT NULL,
			source_version_ids TEXT NOT NULL DEFAULT '[]',
			param_ids TEXT NOT NULL DEFAULT '[]',
			input_node_ids TEXT NOT NULL DEFAULT '[]',
			produced_event_id TEXT NOT NULL
		);`,
	}
	for _, q := range stmts {
		if _, err := s.db.Exec(q); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	for _, idx := range []string{
		`CREATE INDEX IF NOT EXISTS idx_events_project ON events(project_id, seq);`,
		`CREATE INDEX IF NOT EXISTS idx_versions_project ON versions(project_id, layer, version);`,
		`CREATE INDEX IF NOT EXISTS idx_derived_project ON derived(project_id, batch_id);`,
		`CREATE INDEX IF NOT EXISTS idx_diag_project ON diagnostics(project_id);`,
	} {
		if _, err := s.db.Exec(idx); err != nil {
			return err
		}
	}
	return nil
}

// Tx runs fn in a serialized transaction.
func (s *Store) Tx(ctx context.Context, fn func(execTx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) CreateProject(ctx context.Context, p *domain.Project) error {
	return s.createProject(ctx, s.db, p)
}

func (s *Store) createProject(ctx context.Context, ex execTx, p *domain.Project) error {
	labels, _ := json.Marshal(p.Labels)
	_, err := ex.ExecContext(ctx,
		`INSERT INTO projects(id,name,description,labels,created_at) VALUES(?,?,?,?,?)`,
		p.ID, p.Name, p.Description, string(labels), p.CreatedAt.Format("2006-01-02T15:04:05.000000000Z07:00"))
	return err
}

func (s *Store) CreateProjectTx(ctx context.Context, tx execTx, p *domain.Project) error {
	return s.createProject(ctx, tx, p)
}

func (s *Store) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id,name,description,labels,created_at FROM projects WHERE id=?`, id)
	var p domain.Project
	var labels string
	if err := row.Scan(&p.ID, &p.Name, &p.Description, &labels, &p.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	_ = json.Unmarshal([]byte(labels), &p.Labels)
	return &p, nil
}

func (s *Store) ListProjects(ctx context.Context) ([]*domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,labels,created_at FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Project
	for rows.Next() {
		var p domain.Project
		var labels string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &labels, &p.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(labels), &p.Labels)
		out = append(out, &p)
	}
	return out, rows.Err()
}

// AppendEvent writes one event with a gapless per-project sequence number.
func (s *Store) AppendEvent(ctx context.Context, tx execTx, e *domain.Event) error {
	if e.ID == "" {
		e.ID = randID(domain.PrefixEvent)
	}
	var last sql.NullInt64
	if err := tx.QueryRowContext(ctx, `SELECT MAX(seq) FROM events WHERE project_id=?`, e.ProjectID).Scan(&last); err != nil {
		return err
	}
	e.Seq = last.Int64 + 1
	body, _ := json.Marshal(e.Payload)
	_, err := tx.ExecContext(ctx,
		`INSERT INTO events(project_id,seq,id,type,at,actor,parent_id,payload,undone_by) VALUES(?,?,?,?,?,?,?,?,?)`,
		e.ProjectID, e.Seq, e.ID, e.Type, e.At, e.Actor, e.ParentID, string(body), e.UndoneBy)
	return err
}

func (s *Store) LastEventID(ctx context.Context, projectID string) (string, int64, error) {
	var id string
	var seq int64
	err := s.db.QueryRowContext(ctx, `SELECT id,seq FROM events WHERE project_id=? ORDER BY seq DESC LIMIT 1`, projectID).Scan(&id, &seq)
	if errors.Is(err, sql.ErrNoRows) {
		return "", 0, nil
	}
	return id, seq, err
}

func (s *Store) LoadEvents(ctx context.Context, projectID string) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,type,at,actor,parent_id,payload,undone_by,seq FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var body string
		if err := rows.Scan(&e.ID, &e.Type, &e.At, &e.Actor, &e.ParentID, &body, &e.UndoneBy, &e.Seq); err != nil {
			return nil, err
		}
		e.ProjectID = projectID
		e.Payload = json.RawMessage(body)
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) SaveParams(ctx context.Context, tx execTx, p domain.Params) error {
	body, _ := json.Marshal(p)
	_, err := tx.ExecContext(ctx,
		`INSERT INTO params(id,project_id,version,parent_id,body) VALUES(?,?,?,?,?)
		 ON CONFLICT(id) DO NOTHING`,
		p.ID, p.ProjectID, p.Version, p.ParentID, string(body))
	return err
}

func (s *Store) LoadParams(ctx context.Context, id string) (*domain.Params, error) {
	var body string
	err := s.db.QueryRowContext(ctx, `SELECT body FROM params WHERE id=?`, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var p domain.Params
	if err := json.Unmarshal([]byte(body), &p); err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) SaveVersion(ctx context.Context, tx execTx, v *domain.SourceVersion) error {
	body, _ := json.Marshal(v)
	_, err := tx.ExecContext(ctx,
		`INSERT INTO versions(id,project_id,layer,version,parent_version,media_type,raw_text,sha256,committed_at,commit_event_id,body)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
		v.ID, v.ProjectID, v.Layer, v.Version, v.ParentVersion, v.MediaType, v.RawText, v.SHA256, v.CommittedAt, v.CommitEventID, string(body))
	return err
}

func (s *Store) LoadVersion(ctx context.Context, id string) (*domain.SourceVersion, error) {
	var body string
	err := s.db.QueryRowContext(ctx, `SELECT body FROM versions WHERE id=?`, id).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var v domain.SourceVersion
	if err := json.Unmarshal([]byte(body), &v); err != nil {
		return nil, err
	}
	return &v, nil
}

func (s *Store) ListVersions(ctx context.Context, projectID, layer string) ([]*domain.SourceVersion, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT body FROM versions WHERE project_id=? AND layer=? ORDER BY version`, projectID, layer)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.SourceVersion
	for rows.Next() {
		var body string
		if err := rows.Scan(&body); err != nil {
			return nil, err
		}
		var v domain.SourceVersion
		if err := json.Unmarshal([]byte(body), &v); err != nil {
			return nil, err
		}
		out = append(out, &v)
	}
	return out, rows.Err()
}

func (s *Store) AddDiagnostics(ctx context.Context, tx execTx, projectID string, ds []domain.Diagnostic) error {
	for _, d := range ds {
		_, err := tx.ExecContext(ctx,
			`INSERT INTO diagnostics(id,project_id,session_id,layer,row,field,token,severity,message)
			 VALUES(?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
			d.ID, projectID, d.SessionID, d.Layer, d.Row, d.Field, d.Token, d.Severity, d.Message)
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListDiagnostics(ctx context.Context, projectID string) ([]domain.Diagnostic, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,session_id,layer,row,field,token,severity,message FROM diagnostics WHERE project_id=? ORDER BY layer,row`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Diagnostic
	for rows.Next() {
		var d domain.Diagnostic
		if err := rows.Scan(&d.ID, &d.SessionID, &d.Layer, &d.Row, &d.Field, &d.Token, &d.Severity, &d.Message); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

func (s *Store) SaveDerived(ctx context.Context, tx execTx, n *domain.DerivedNode, eventID string) error {
	body, _ := json.Marshal(n)
	fp, _ := json.Marshal(n.Fingerprint)
	src, _ := json.Marshal(n.SourceVersionIDs)
	pids, _ := json.Marshal(n.ParamIDs)
	in, _ := json.Marshal(n.InputNodeIDs)
	_, err := tx.ExecContext(ctx,
		`INSERT INTO derived(id,project_id,batch_id,kind,fingerprint,body,source_version_ids,param_ids,input_node_ids,produced_event_id)
		 VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(id) DO NOTHING`,
		n.ID, n.ProjectID, n.BatchID, string(n.Kind), string(fp), string(body), string(src), string(pids), string(in), eventID)
	return err
}

func (s *Store) LoadDerivedByBatch(ctx context.Context, projectID, batchID string) ([]*domain.DerivedNode, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,batch_id,kind,fingerprint,body,source_version_ids,param_ids,input_node_ids,produced_event_id FROM derived WHERE project_id=? AND batch_id=?`,
		projectID, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.DerivedNode
	for rows.Next() {
		n, err := scanDerived(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (s *Store) LoadDerived(ctx context.Context, id string) (*domain.DerivedNode, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT id,batch_id,kind,fingerprint,body,source_version_ids,param_ids,input_node_ids,produced_event_id FROM derived WHERE id=?`, id)
	n, err := scanDerived(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return n, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanDerived(r rowScanner) (*domain.DerivedNode, error) {
	var n domain.DerivedNode
	var kind, fpStr, body, src, pids, in string
	if err := r.Scan(&n.ID, &n.BatchID, &kind, &fpStr, &body, &src, &pids, &in, &n.ProducedEventID); err != nil {
		return nil, err
	}
	n.Kind = domain.DerivedKind(kind)
	var full domain.DerivedNode
	if err := json.Unmarshal([]byte(body), &full); err != nil {
		return nil, err
	}
	full.ID, full.BatchID, full.Kind = n.ID, n.BatchID, n.Kind
	return &full, nil
}

// execTx is the transaction surface used by services.
type execTx interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}
