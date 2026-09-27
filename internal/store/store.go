package store

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"tga-review/internal/domain"
)

// Store is the append-only persistence layer. All IDs are stored as given so
// identity survives export/delete/import; sequence counters come from the meta
// table and are reset on import from the persisted high-water marks.
type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)")
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("schema: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

// NextSeq returns a process-wide monotonic id segment for a named counter.
func (s *Store) NextSeq(ctx context.Context, key string) (int64, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var v int64
	row := tx.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key)
	switch err := row.Scan(&v); {
	case err == sql.ErrNoRows:
		v = 1
		if _, err := tx.ExecContext(ctx, `INSERT INTO meta(key,value) VALUES(?,?)`, key, fmt.Sprint(v)); err != nil {
			return 0, err
		}
	case err != nil:
		return 0, err
	default:
		v++
		if _, err := tx.ExecContext(ctx, `UPDATE meta SET value=? WHERE key=?`, fmt.Sprint(v), key); err != nil {
			return 0, err
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return v, nil
}

func (s *Store) EnsureSeq(ctx context.Context, key string, atLeast int64) error {
	v, err := s.NextSeq(ctx, key)
	if err != nil {
		return err
	}
	// NextSeq already incremented once; fast-forward by repeating is simple but
	// inefficient for imports; instead directly bump through a dedicated upsert.
	if v >= atLeast {
		return nil
	}
	_, err = s.db.ExecContext(ctx,
		`INSERT INTO meta(key,value) VALUES(?,?)
		 ON CONFLICT(key) DO UPDATE SET value=MAX(CAST(excluded.value AS INTEGER), CAST(meta.value AS INTEGER))`,
		key, fmt.Sprint(atLeast))
	return err
}

// ---- projects ----

func (s *Store) CreateProject(ctx context.Context, p domain.Project) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects(id,name,created_at,seed) VALUES(?,?,?,?)`,
		p.ID, p.Name, p.CreatedAt, p.Seed)
	return err
}

func (s *Store) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	var p domain.Project
	err := s.db.QueryRowContext(ctx,
		`SELECT id,name,created_at,seed FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.CreatedAt, &p.Seed)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (s *Store) ListProjects(ctx context.Context) ([]domain.Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,created_at,seed FROM projects ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Project
	for rows.Next() {
		var p domain.Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedAt, &p.Seed); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// ---- batches ----

func (s *Store) InsertBatch(ctx context.Context, b domain.Batch) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO batches(id,project_id,seq,status,reason,created_at) VALUES(?,?,?,?,?,?)`,
		b.ID, b.ProjectID, b.Seq, string(b.Status), b.Reason, b.CreatedAt)
	return err
}

func (s *Store) ListBatches(ctx context.Context, projectID string) ([]domain.Batch, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,project_id,seq,status,reason,created_at FROM batches WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Batch
	for rows.Next() {
		var b domain.Batch
		var st string
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.Seq, &st, &b.Reason, &b.CreatedAt); err != nil {
			return nil, err
		}
		b.Status = domain.Status(st)
		out = append(out, b)
	}
	return out, rows.Err()
}

// ---- sources ----

func (s *Store) InsertSource(ctx context.Context, src domain.Source) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO sources(id,project_id,batch_id,kind,version,parent_id,status,raw_text,diagnostic,created_at,seq)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		src.ID, src.ProjectID, src.BatchID, string(src.Kind), src.Version, src.ParentID,
		string(src.Status), src.RawText, src.Diagnostic, src.CreatedAt, src.Seq)
	return err
}

// LatestSources returns the newest accepted/diagnostic version of every kind.
func (s *Store) LatestSources(ctx context.Context, projectID string) (map[domain.Kind]domain.Source, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,project_id,batch_id,kind,version,parent_id,status,raw_text,diagnostic,created_at,seq
		 FROM sources WHERE project_id=? ORDER BY kind, version`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	m := map[domain.Kind]domain.Source{}
	for rows.Next() {
		var src domain.Source
		var kind, status string
		if err := rows.Scan(&src.ID, &src.ProjectID, &src.BatchID, &kind, &src.Version, &src.ParentID,
			&status, &src.RawText, &src.Diagnostic, &src.CreatedAt, &src.Seq); err != nil {
			return nil, err
		}
		src.Kind = domain.Kind(kind)
		src.Status = domain.Status(status)
		m[src.Kind] = src
	}
	return m, rows.Err()
}

func (s *Store) GetSource(ctx context.Context, id string) (*domain.Source, error) {
	var src domain.Source
	var kind, status string
	err := s.db.QueryRowContext(ctx,
		`SELECT id,project_id,batch_id,kind,version,parent_id,status,raw_text,diagnostic,created_at,seq
		 FROM sources WHERE id=?`, id).
		Scan(&src.ID, &src.ProjectID, &src.BatchID, &kind, &src.Version, &src.ParentID,
			&status, &src.RawText, &src.Diagnostic, &src.CreatedAt, &src.Seq)
	if err != nil {
		return nil, err
	}
	src.Kind = domain.Kind(kind)
	src.Status = domain.Status(status)
	return &src, nil
}

func (s *Store) ListSources(ctx context.Context, projectID string) ([]domain.Source, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,project_id,batch_id,kind,version,parent_id,status,raw_text,diagnostic,created_at,seq
		 FROM sources WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Source
	for rows.Next() {
		var src domain.Source
		var kind, status string
		if err := rows.Scan(&src.ID, &src.ProjectID, &src.BatchID, &kind, &src.Version, &src.ParentID,
			&status, &src.RawText, &src.Diagnostic, &src.CreatedAt, &src.Seq); err != nil {
			return nil, err
		}
		src.Kind = domain.Kind(kind)
		src.Status = domain.Status(status)
		out = append(out, src)
	}
	return out, rows.Err()
}
