package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"tgalineage/internal/model"
)

// NextSeq allocates the next monotonic value for a named per-project counter.
func (s *Store) NextSeq(ctx context.Context, tx *sql.Tx, projectID, name string) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx,
		`INSERT INTO counters(project_id,name,value) VALUES(?,?,1)
		 ON CONFLICT(project_id,name) DO UPDATE SET value=value+1 RETURNING value`,
		projectID, name).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("counter %s: %w", name, err)
	}
	return n, nil
}

// CreateProject inserts a project.
func (s *Store) CreateProject(ctx context.Context, p model.Project) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO projects(id,title,conflict,archived_of,created_at)
		 VALUES(?,?,?,?,?)`,
		p.ID, p.Title, b2i(p.Conflict), p.ArchivedOf, p.CreatedAt)
	return err
}

// GetProject loads project metadata.
func (s *Store) GetProject(ctx context.Context, id string) (model.Project, error) {
	var p model.Project
	var conflict int
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,title,conflict,archived_of,created_at FROM projects WHERE id=?`,
		id).Scan(&p.ID, &p.Title, &conflict, &p.ArchivedOf, &p.CreatedAt)
	if err == nil {
		p.Conflict = conflict != 0
	}
	return p, err
}

// ListProjects returns all projects, oldest first.
func (s *Store) ListProjects(ctx context.Context) ([]model.Project, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,title,conflict,archived_of,created_at FROM projects ORDER BY created_at,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Project
	for rows.Next() {
		var p model.Project
		var conflict int
		if err := rows.Scan(&p.ID, &p.Title, &conflict, &p.ArchivedOf, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.Conflict = conflict != 0
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetProjectConflict marks/clears the unresolved-conflict flag.
func (s *Store) SetProjectConflict(ctx context.Context, id string, conflict bool) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE projects SET conflict=? WHERE id=?`, b2i(conflict), id)
	return err
}

// InsertInput stores one immutable raw revision.
func (s *Store) InsertInput(ctx context.Context, tx *sql.Tx, in model.RawInput) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO inputs(id,project_id,kind,version,parent_version,content_type,
		   raw_text,import_batch,valid,seed,parsed_json,diagnostics,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		in.ID, in.ProjectID, in.Kind, in.Version, in.ParentVersion, in.ContentType,
		in.RawText, in.ImportBatch, b2i(in.Valid), in.Seed,
		string(in.Parsed), jsonOrEmpty(in.Diagnostics), in.CreatedAt)
	return err
}

// CurrentInputVersion returns the highest version for a kind (0 if none).
func (s *Store) CurrentInputVersion(ctx context.Context, projectID, kind string) (int, error) {
	var v sql.NullInt64
	err := s.DB.QueryRowContext(ctx,
		`SELECT MAX(version) FROM inputs WHERE project_id=? AND kind=?`, projectID, kind).Scan(&v)
	if err != nil {
		return 0, err
	}
	return int(v.Int64), nil
}

// GetInput loads one raw revision by id.
func (s *Store) GetInput(ctx context.Context, id string) (model.RawInput, error) {
	return scanInput(s.DB.QueryRowContext(ctx,
		`SELECT id,project_id,kind,version,parent_version,content_type,raw_text,
		        import_batch,valid,seed,parsed_json,diagnostics,created_at
		 FROM inputs WHERE id=?`, id))
}

// CurrentInputs returns the latest valid revision of each stratified layer.
func (s *Store) CurrentInputs(ctx context.Context, projectID string) ([]model.RawInput, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT i.id,i.project_id,i.kind,i.version,i.parent_version,i.content_type,i.raw_text,
		        i.import_batch,i.valid,i.seed,i.parsed_json,i.diagnostics,i.created_at
		 FROM inputs i
		 JOIN (SELECT project_id,kind,MAX(version) mv FROM inputs WHERE project_id=? GROUP BY kind) m
		   ON m.project_id=i.project_id AND m.kind=i.kind AND i.version=m.mv`,
		projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RawInput
	for rows.Next() {
		in, err := scanInput(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// AllInputs returns every revision of every layer (replay/export).
func (s *Store) AllInputs(ctx context.Context, projectID string) ([]model.RawInput, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,kind,version,parent_version,content_type,raw_text,
		        import_batch,valid,seed,parsed_json,diagnostics,created_at
		 FROM inputs WHERE project_id=? ORDER BY kind,version`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.RawInput
	for rows.Next() {
		in, err := scanInput(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

// InsertRun stores one derived run; older runs for the project are demoted.
func (s *Store) InsertRun(ctx context.Context, tx *sql.Tx, r model.Run) error {
	if _, err := tx.ExecContext(ctx,
		`UPDATE runs SET current=0 WHERE project_id=?`, r.ProjectID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx,
		`INSERT INTO runs(id,project_id,algo_version,param_fingerprint,sources_json,
		   current,diagnostics_json,payload_json,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.ProjectID, r.AlgoVersion, r.ParamFingerprint, jsonOrEmpty(r.Sources),
		b2i(r.Current), jsonOrEmpty(r.Diagnostics), string(r.Payload), r.CreatedAt)
	return err
}

// AllRuns returns all runs ordered by id (run_n) for replay.
func (s *Store) AllRuns(ctx context.Context, projectID string) ([]model.Run, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,algo_version,param_fingerprint,sources_json,current,
		        diagnostics_json,payload_json,created_at
		 FROM runs WHERE project_id=? ORDER BY rowid`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Run
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// CurrentRun returns the newest run (current flag), if any.
func (s *Store) CurrentRun(ctx context.Context, projectID string) (model.Run, error) {
	return scanRun(s.DB.QueryRowContext(ctx,
		`SELECT id,project_id,algo_version,param_fingerprint,sources_json,current,
		        diagnostics_json,payload_json,created_at
		 FROM runs WHERE project_id=? AND current=1`, projectID))
}

// GetRun loads one run by id.
func (s *Store) GetRun(ctx context.Context, id string) (model.Run, error) {
	return scanRun(s.DB.QueryRowContext(ctx,
		`SELECT id,project_id,algo_version,param_fingerprint,sources_json,current,
		        diagnostics_json,payload_json,created_at FROM runs WHERE id=?`, id))
}

// InsertCandidate stores a candidate.
func (s *Store) InsertCandidate(ctx context.Context, tx *sql.Tx, c model.Candidate) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO candidates(id,project_id,run_id,type,alt_group,parent_id,
		   status,summary,detail_json,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.ProjectID, c.RunID, c.Type, c.AltGroup, c.ParentID, c.Status,
		c.Summary, string(c.Detail), c.CreatedAt)
	return err
}

// UpdateCandidateStatus changes lifecycle status.
func (s *Store) UpdateCandidateStatus(ctx context.Context, tx *sql.Tx, id, status string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE candidates SET status=? WHERE id=?`, status, id)
	return err
}

// GetCandidate loads one candidate.
func (s *Store) GetCandidate(ctx context.Context, id string) (model.Candidate, error) {
	return scanCandidate(s.DB.QueryRowContext(ctx,
		`SELECT id,project_id,run_id,type,alt_group,parent_id,status,summary,detail_json,created_at
		 FROM candidates WHERE id=?`, id))
}

// AllCandidates returns candidates for a project.
func (s *Store) AllCandidates(ctx context.Context, projectID string) ([]model.Candidate, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,run_id,type,alt_group,parent_id,status,summary,detail_json,created_at
		 FROM candidates WHERE project_id=? ORDER BY rowid`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Candidate
	for rows.Next() {
		c, err := scanCandidate(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// InsertEvent appends an event (seq supplied by caller via NextSeq).
func (s *Store) InsertEvent(ctx context.Context, tx *sql.Tx, e model.Event) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO events(project_id,seq,id,type,undone,undoes,actor,payload_json,created_at)
		 VALUES(?,?,?,?,?,?,?,?,?)`,
		e.ProjectID, e.Seq, e.ID, e.Type, b2i(e.Undone), e.Undoes, e.Actor,
		string(e.Payload), e.CreatedAt)
	return err
}

// MarkEventUndone flags an event (and, by event-store convention, its dependents
// are emitted by the project layer as separate entries).
func (s *Store) MarkEventUndone(ctx context.Context, tx *sql.Tx, projectID string, seq int) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE events SET undone=1 WHERE project_id=? AND seq=?`, projectID, seq)
	return err
}

// AllEvents returns the full append-only history.
func (s *Store) AllEvents(ctx context.Context, projectID string) ([]model.Event, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT project_id,seq,id,type,undone,undoes,actor,payload_json,created_at
		 FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Event
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// InsertBatch records one import attempt.
func (s *Store) InsertBatch(ctx context.Context, b model.Batch) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO batches(id,project_id,label,sections_json,created_at) VALUES(?,?,?,?,?)`,
		b.ID, b.ProjectID, b.Label, jsonOrEmpty(b.Sections), b.CreatedAt)
	return err
}

// AllBatches returns import attempts ordered by insertion.
func (s *Store) AllBatches(ctx context.Context, projectID string) ([]model.Batch, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,label,sections_json,created_at FROM batches
		 WHERE project_id=? ORDER BY rowid`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Batch
	for rows.Next() {
		var b model.Batch
		var secs string
		if err := rows.Scan(&b.ID, &b.ProjectID, &b.Label, &secs, &b.CreatedAt); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(secs), &b.Sections); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// Meta helpers.
func (s *Store) GetMeta(ctx context.Context, key string) (string, bool, error) {
	var v string
	err := s.DB.QueryRowContext(ctx, `SELECT value FROM meta WHERE key=?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

func (s *Store) SetMeta(ctx context.Context, key, value string) error {
	_, err := s.DB.ExecContext(ctx,
		`INSERT INTO meta(key,value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		key, value)
	return err
}

// WipeAll deletes every row (used by the export/delete/reimport acceptance step).
func (s *Store) WipeAll(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx,
		`DELETE FROM events; DELETE FROM candidates; DELETE FROM runs; DELETE FROM inputs;
		 DELETE FROM batches; DELETE FROM counters; DELETE FROM projects; DELETE FROM meta;`)
	return err
}
