// Package store is the SQLite-backed, append-only persistence layer.
//
// The event ledger is the source of truth; submissions and derived rows are
// immutable content-addressed materializations referenced by events. Nothing
// is ever updated in place, which makes export -> delete local DB -> import
// preserve identities, ordering and provenance edges exactly.
package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"

	"tgaline/internal/domain"
)

type Store struct {
	db *sql.DB
}

func Open(ctx context.Context, path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=foreign_keys(on)&_pragma=journal_mode(WAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

func (s *Store) migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS projects(
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			description TEXT,
			created_at TEXT NOT NULL,
			head_seq INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS events(
			project_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			id TEXT NOT NULL UNIQUE,
			type TEXT NOT NULL,
			at TEXT NOT NULL,
			actor TEXT NOT NULL,
			summary TEXT NOT NULL,
			payload TEXT,
			raw_refs TEXT,
			derived_refs TEXT,
			undoes_seq INTEGER NOT NULL DEFAULT 0,
			active INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY(project_id, seq)
		)`,
		`CREATE TABLE IF NOT EXISTS submissions(
			project_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			version INTEGER NOT NULL,
			id TEXT NOT NULL UNIQUE,
			parent_id TEXT,
			body TEXT NOT NULL,
			created_seq INTEGER NOT NULL,
			PRIMARY KEY(project_id, kind, version)
		)`,
		`CREATE TABLE IF NOT EXISTS param_revisions(
			project_id TEXT NOT NULL,
			version INTEGER NOT NULL,
			id TEXT NOT NULL UNIQUE,
			parent_id TEXT,
			fingerprint TEXT NOT NULL,
			body TEXT NOT NULL,
			created_seq INTEGER NOT NULL,
			PRIMARY KEY(project_id, version)
		)`,
		`CREATE TABLE IF NOT EXISTS derived(
			project_id TEXT NOT NULL,
			kind TEXT NOT NULL,
			id TEXT NOT NULL UNIQUE,
			algo TEXT NOT NULL,
			params_fp TEXT NOT NULL,
			sources TEXT NOT NULL,
			body TEXT NOT NULL,
			status TEXT NOT NULL,
			created_seq INTEGER NOT NULL,
			PRIMARY KEY(project_id, kind, id)
		)`,
		`CREATE TABLE IF NOT EXISTS decisions(
			project_id TEXT NOT NULL,
			seq INTEGER NOT NULL,
			id TEXT NOT NULL UNIQUE,
			subject_id TEXT NOT NULL,
			subject_kind TEXT NOT NULL,
			verdict TEXT NOT NULL,
			params_id TEXT,
			rationale TEXT NOT NULL,
			overrides_decision TEXT,
			active INTEGER NOT NULL DEFAULT 1,
			PRIMARY KEY(project_id, seq)
		)`,
	}
	for _, st := range stmts {
		if _, err := s.db.ExecContext(ctx, st); err != nil {
			return fmt.Errorf("migrate: %w", err)
		}
	}
	return nil
}

// ---- projects ----

func (s *Store) CreateProject(ctx context.Context, p domain.ProjectSeed) error {
	_, err := s.db.ExecContext(ctx,
		`INSERT INTO projects(id,name,description,created_at,head_seq) VALUES(?,?,?,?,?)`,
		p.ID, p.Name, p.Description, p.CreatedAt, 0)
	return err
}

type ProjectRow struct {
	ID, Name, Description, CreatedAt string
	HeadSeq                          int64
}

func (s *Store) ListProjects(ctx context.Context) ([]ProjectRow, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id,name,description,created_at,head_seq FROM projects ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProjectRow
	for rows.Next() {
		var p ProjectRow
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.HeadSeq); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetProject(ctx context.Context, id string) (ProjectRow, error) {
	var p ProjectRow
	err := s.db.QueryRowContext(ctx,
		`SELECT id,name,description,created_at,head_seq FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Description, &p.CreatedAt, &p.HeadSeq)
	return p, err
}

// ---- ledger append (single transaction with all materializations) ----

// Tx groups one ledger append plus its referenced materialized rows.
type Tx struct {
	Event          domain.Event
	Submission     *SubmissionRow
	ParamRevision  *ParamRow
	Derived        []DerivedRow
	Decision       *DecisionRow
	DeactivateDecisions []string // decision IDs marked inactive (undo/conflict)
}

type SubmissionRow struct {
	Kind     domain.Kind
	Version  int
	ID       string
	ParentID string
	Body     string
}

type ParamRow struct {
	Version     int
	ID          string
	ParentID    string
	Fingerprint string
	Body        string
}

type DerivedRow struct {
	Kind     domain.Kind
	ID       string
	Algo     string
	ParamsFP string
	Sources  []domain.Source
	Body     string
	Status   domain.Status
}

type DecisionRow struct {
	ID                string
	SubjectID         string
	SubjectKind       domain.Kind
	Verdict           string
	ParamsID          string
	Rationale         string
	OverridesDecision string
}

// Append writes one event and its materializations atomically, returning the
// finalized event (with assigned seq and id).
func (s *Store) Append(ctx context.Context, projectID string, tx Tx) (domain.Event, error) {
	err := doTx(ctx, s.db, func(db *sql.Tx) error {
		var head int64
		if err := db.QueryRowContext(ctx, `SELECT head_seq FROM projects WHERE id=?`, projectID).Scan(&head); err != nil {
			return err
		}
		seq := head + 1
		tx.Event.Seq = seq
		tx.Event.ProjectID = projectID
		if tx.Event.ID == "" {
			tx.Event.ID = EventID(projectID, seq, string(tx.Event.Type), tx.Event.Summary)
		}
		if tx.Event.Active == false {
			// zero value handling below
		}
		tx.Event.Active = true
		if tx.Event.At == "" {
			return fmt.Errorf("event requires timestamp")
		}
		payload := nullable(tx.Event.Payload)
		rawRefs, _ := json.Marshal(tx.Event.RawRefs)
		derivedRefs, _ := json.Marshal(tx.Event.DerivedRefs)
		if _, err := db.ExecContext(ctx,
			`INSERT INTO events(project_id,seq,id,type,at,actor,summary,payload,raw_refs,derived_refs,undoes_seq,active)
			 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
			projectID, seq, tx.Event.ID, string(tx.Event.Type), tx.Event.At, tx.Event.Actor,
			tx.Event.Summary, payload, string(rawRefs), string(derivedRefs), tx.Event.UndoesSeq, 1); err != nil {
			return err
		}
		if tx.Submission != nil {
			if _, err := db.ExecContext(ctx,
				`INSERT INTO submissions(project_id,kind,version,id,parent_id,body,created_seq)
				 VALUES(?,?,?,?,?,?,?)`,
				projectID, string(tx.Submission.Kind), tx.Submission.Version,
				tx.Submission.ID, nullableString(tx.Submission.ParentID), tx.Submission.Body, seq); err != nil {
				return err
			}
		}
		if tx.ParamRevision != nil {
			pr := tx.ParamRevision
			if _, err := db.ExecContext(ctx,
				`INSERT INTO param_revisions(project_id,version,id,parent_id,fingerprint,body,created_seq)
				 VALUES(?,?,?,?,?,?,?)`,
				projectID, pr.Version, pr.ID, nullableString(pr.ParentID), pr.Fingerprint, pr.Body, seq); err != nil {
				return err
			}
		}
		for _, d := range tx.Derived {
			src, _ := json.Marshal(d.Sources)
			if _, err := db.ExecContext(ctx,
				`INSERT OR IGNORE INTO derived(project_id,kind,id,algo,params_fp,sources,body,status,created_seq)
				 VALUES(?,?,?,?,?,?,?,?,?)`,
				projectID, string(d.Kind), d.ID, d.Algo, d.ParamsFP, string(src), d.Body, string(d.Status), seq); err != nil {
				return err
			}
		}
		if tx.Decision != nil {
			dc := tx.Decision
			active := 1
			if _, err := db.ExecContext(ctx,
				`INSERT INTO decisions(project_id,seq,id,subject_id,subject_kind,verdict,params_id,rationale,overrides_decision,active)
				 VALUES(?,?,?,?,?,?,?,?,?,?)`,
				projectID, seq, dc.ID, dc.SubjectID, string(dc.SubjectKind), dc.Verdict,
				nullableString(dc.ParamsID), dc.Rationale, nullableString(dc.OverridesDecision), active); err != nil {
				return err
			}
		}
		for _, id := range tx.DeactivateDecisions {
			if _, err := db.ExecContext(ctx,
				`UPDATE decisions SET active=0 WHERE project_id=? AND id=?`, projectID, id); err != nil {
				return err
			}
		}
		if _, err := db.ExecContext(ctx, `UPDATE projects SET head_seq=? WHERE id=?`, seq, projectID); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return domain.Event{}, err
	}
	return tx.Event, nil
}

func doTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

func nullable(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
