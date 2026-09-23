package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	_ "modernc.org/sqlite"

	"tga-review/internal/domain"
)

// Store is the SQLite-backed, append-only project repository.
type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	if _, err := db.ExecContext(ctx, "PRAGMA busy_timeout=5000; PRAGMA foreign_keys=ON; PRAGMA journal_mode=WAL;"); err != nil {
		return nil, err
	}
	if _, err := db.ExecContext(ctx, schema); err != nil {
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

func jb(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ---- counters ----

func (s *Store) NextCounter(ctx context.Context, projectID, name string) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var v int
	row := tx.QueryRowContext(ctx, "SELECT value FROM counters WHERE project_id=? AND name=?", projectID, name)
	switch err := row.Scan(&v); err {
	case nil:
		v++
		if _, err := tx.ExecContext(ctx, "UPDATE counters SET value=? WHERE project_id=? AND name=?", v, projectID, name); err != nil {
			return 0, err
		}
	case sql.ErrNoRows:
		v = 1
		if _, err := tx.ExecContext(ctx, "INSERT INTO counters(project_id,name,value) VALUES(?,?,1)", projectID, name); err != nil {
			return 0, err
		}
	default:
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return v, nil
}

// ---- projects ----

func (s *Store) CreateProject(ctx context.Context, p domain.Project) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO projects(id,name,description,created_at) VALUES(?,?,?,?)",
		p.ID, p.Name, p.Description, p.CreatedAt)
	return err
}

type Project struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	CreatedAt   string `json:"created_at"`
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, "SELECT id,name,description,created_at FROM projects ORDER BY created_at, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProject(ctx context.Context, id string) (*Project, error) {
	var p Project
	err := s.db.QueryRowContext(ctx, "SELECT id,name,description,created_at FROM projects WHERE id=?", id).
		Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ---- events ----

func (s *Store) AppendEvent(ctx context.Context, e domain.Event) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO events(project_id,seq,id,type,payload,created_at) VALUES(?,?,?,?,?,?)",
		e.ProjectID, e.Seq, e.ID, e.Type, string(jb(e.Payload)), e.CreatedAt)
	return err
}

func (s *Store) Events(ctx context.Context, projectID string) ([]domain.Event, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT seq,id,type,payload,created_at FROM events WHERE project_id=? ORDER BY seq", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Event
	for rows.Next() {
		var e domain.Event
		var payload string
		if err := rows.Scan(&e.Seq, &e.ID, &e.Type, &payload, &e.CreatedAt); err != nil {
			return nil, err
		}
		e.ProjectID = projectID
		if payload != "" {
			_ = json.Unmarshal([]byte(payload), &e.Payload)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (s *Store) EventCount(ctx context.Context, projectID string) (int, error) {
	var n int
	if err := s.db.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE project_id=?", projectID).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

// ---- sources ----

func (s *Store) InsertSource(ctx context.Context, src domain.Source) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sources(id,project_id,layer,version,parent_id,format,raw,payload,diag,accepted,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		src.ID, src.ProjectID, string(src.Layer), src.Version, src.ParentID, src.Format, src.Raw,
		string(jb(src.Payload)), string(jb(src.Diag)), src.Accepted, src.CreatedAt)
	return err
}

func scanSource(row interface {
	Scan(...any) error
}) (domain.Source, error) {
	var src domain.Source
	var layer, payload, diag, parentID sql.NullString
	var accepted int
	if err := row.Scan(&src.ID, &src.ProjectID, &layer, &src.Version, &parentID, &src.Format, &src.Raw, &payload, &diag, &accepted, &src.CreatedAt); err != nil {
		return src, err
	}
	src.Layer = domain.SourceLayer(layer.String)
	src.ParentID = parentID.String
	_ = json.Unmarshal([]byte(payload.String), &src.Payload)
	_ = json.Unmarshal([]byte(diag.String), &src.Diag)
	src.Accepted = accepted != 0
	return src, nil
}

const sourceCols = `id,project_id,layer,version,parent_id,format,raw,payload,diag,accepted,created_at`

func (s *Store) CurrentSource(ctx context.Context, projectID string, layer domain.SourceLayer) (*domain.Source, error) {
	row := s.db.QueryRowContext(ctx,
		`SELECT `+sourceCols+` FROM sources WHERE project_id=? AND layer=? AND accepted=1 ORDER BY version DESC LIMIT 1`,
		projectID, string(layer))
	src, err := scanSource(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &src, nil
}

func (s *Store) SourceByID(ctx context.Context, id string) (*domain.Source, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE id=?`, id)
	src, err := scanSource(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &src, nil
}

func (s *Store) AllSources(ctx context.Context, projectID string) ([]domain.Source, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE project_id=? ORDER BY layer, version`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Source
	for rows.Next() {
		src, err := scanSource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, rows.Err()
}

// ---- sessions ----

func (s *Store) InsertSession(ctx context.Context, ses domain.Session) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO sessions(id,project_id,status,submitted_at,fragments) VALUES(?,?,?,?,?)",
		ses.ID, ses.ProjectID, ses.Status, ses.Submitted, string(jb(ses.Fragments)))
	return err
}

func (s *Store) Sessions(ctx context.Context, projectID string) ([]domain.Session, error) {
	rows, err := s.db.QueryContext(ctx,
		"SELECT id,project_id,status,submitted_at,fragments FROM sessions WHERE project_id=? ORDER BY submitted_at,id", projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Session
	for rows.Next() {
		var ses domain.Session
		var frags string
		if err := rows.Scan(&ses.ID, &ses.ProjectID, &ses.Status, &ses.Submitted, &frags); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(frags), &ses.Fragments)
		out = append(out, ses)
	}
	return out, rows.Err()
}

// ---- params ----

func (s *Store) InsertParams(ctx context.Context, projectID string, version int, fp, parentFP string, p domain.Params, createdAt string) error {
	_, err := s.db.ExecContext(ctx,
		"INSERT INTO params_versions(project_id,version,fingerprint,parent_fp,json,created_at) VALUES(?,?,?,?,?,?)",
		projectID, version, fp, parentFP, string(jb(p)), createdAt)
	return err
}

func (s *Store) CurrentParams(ctx context.Context, projectID string) (domain.Params, string, error) {
	var p domain.Params
	var js, fp, parent sql.NullString
	var version int
	err := s.db.QueryRowContext(ctx,
		"SELECT version,fingerprint,parent_fp,json FROM params_versions WHERE project_id=? ORDER BY version DESC LIMIT 1", projectID).
		Scan(&version, &fp, &parent, &js)
	if err == sql.ErrNoRows {
		def := domain.DefaultParams()
		return def, def.Fingerprint(), nil
	}
	if err != nil {
		return p, "", err
	}
	if err := json.Unmarshal([]byte(js.String), &p); err != nil {
		return p, "", err
	}
	return p, fp.String, nil
}

func (s *Store) ParamsAtFP(ctx context.Context, projectID, fp string) (domain.Params, error) {
	var p domain.Params
	var js string
	err := s.db.QueryRowContext(ctx, "SELECT json FROM params_versions WHERE project_id=? AND fingerprint=?", projectID, fp).Scan(&js)
	if err == sql.ErrNoRows {
		return domain.DefaultParams(), nil
	}
	if err != nil {
		return p, err
	}
	return p, json.Unmarshal([]byte(js), &p)
}
