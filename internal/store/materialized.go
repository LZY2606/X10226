package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"tga-review/internal/domain"
)

func jstr(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func jraw(s string) json.RawMessage {
	if s == "" {
		return json.RawMessage("null")
	}
	return json.RawMessage(s)
}

// ---- sources ----

func (s *Store) PutSource(ctx context.Context, tx *sql.Tx, src domain.Source) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO sources(id,project_id,layer,version,parent_id,event_id,submitted_at_step,fingerprint,status,raw,diagnostics)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		src.ID, src.ProjectID, src.Layer, src.Version, nullable(src.ParentID), src.EventID,
		src.SubmittedAtStep, src.Fingerprint, src.Status, string(src.Raw), jstr(src.Diagnostics))
	return err
}

func nullable(v string) sql.NullString {
	return sql.NullString{String: v, Valid: v != ""}
}

func scanSource(row interface{ Scan(...any) error }) (domain.Source, error) {
	var src domain.Source
	var parent, diags sql.NullString
	if err := row.Scan(&src.ID, &src.ProjectID, &src.Layer, &src.Version, &parent,
		&src.EventID, &src.SubmittedAtStep, &src.Fingerprint, &src.Status, &src.Raw, &diags); err != nil {
		return src, err
	}
	if parent.Valid {
		src.ParentID = parent.String
	}
	if diags.Valid && diags.String != "" {
		_ = json.Unmarshal([]byte(diags.String), &src.Diagnostics)
	}
	return src, nil
}

const sourceCols = `id,project_id,layer,version,parent_id,event_id,submitted_at_step,fingerprint,status,raw,diagnostics`

func (s *Store) Source(ctx context.Context, id string) (domain.Source, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+sourceCols+` FROM sources WHERE id=?`, id)
	src, err := scanSource(row)
	if errors.Is(err, sql.ErrNoRows) {
		return src, ErrNotFound
	}
	return src, err
}

// SourcesByLayer returns all versions of a layer, ascending.
func (s *Store) SourcesByLayer(ctx context.Context, projectID, layer string) ([]domain.Source, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+sourceCols+` FROM sources WHERE project_id=? AND layer=? ORDER BY version`, projectID, layer)
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

func (s *Store) AllSources(ctx context.Context, projectID string) ([]domain.Source, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+sourceCols+` FROM sources WHERE project_id=? ORDER BY layer, version`, projectID)
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

// ---- runs ----

func (s *Store) PutRun(ctx context.Context, tx *sql.Tx, r domain.Run) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO runs(id,project_id,event_id,at_step,basis,algo_vers) VALUES(?,?,?,?,?,?)`,
		r.ID, r.ProjectID, r.EventID, r.AtStep, jstr(r.Basis), jstr(r.AlgoVers))
	return err
}

// ---- artifacts ----

func (s *Store) PutArtifact(ctx context.Context, tx *sql.Tx, a domain.Artifact) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO artifacts(id,project_id,run_id,event_id,kind,algo_ver,param_fp,depends_on,current,superseded_by,payload,diagnostics)
		 VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		a.ID, a.ProjectID, a.RunID, a.EventID, a.Kind, a.AlgoVer, a.ParamFP, jstr(a.DependsOn),
		boolInt(a.Current), nullable(a.SupersededBy), string(a.Payload), jstr(a.Diagnostics))
	return err
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// MarkArtifactsNotCurrent revokes current flag for a kind, optionally only
// for the artifacts whose ids are in the set.
func (s *Store) MarkArtifactsNotCurrent(ctx context.Context, tx *sql.Tx, projectID, kind string, ids []string) error {
	if len(ids) == 0 {
		_, err := tx.ExecContext(ctx,
			`UPDATE artifacts SET current=0 WHERE project_id=? AND kind=? AND current=1`, projectID, kind)
		return err
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx,
			`UPDATE artifacts SET current=0 WHERE id=?`, id); err != nil {
			return err
		}
	}
	return nil
}

func scanArtifact(row interface{ Scan(...any) error }) (domain.Artifact, error) {
	var a domain.Artifact
	var sup, deps, diags sql.NullString
	var cur int
	if err := row.Scan(&a.ID, &a.ProjectID, &a.RunID, &a.EventID, &a.Kind, &a.AlgoVer,
		&a.ParamFP, &deps, &cur, &sup, &a.Payload, &diags); err != nil {
		return a, err
	}
	a.Current = cur == 1
	if sup.Valid {
		a.SupersededBy = sup.String
	}
	if deps.Valid {
		_ = json.Unmarshal([]byte(deps.String), &a.DependsOn)
	}
	if diags.Valid {
		_ = json.Unmarshal([]byte(diags.String), &a.Diagnostics)
	}
	return a, nil
}

const artifactCols = `id,project_id,run_id,event_id,kind,algo_ver,param_fp,depends_on,current,superseded_by,payload,diagnostics`

func (s *Store) Artifact(ctx context.Context, id string) (domain.Artifact, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT `+artifactCols+` FROM artifacts WHERE id=?`, id)
	a, err := scanArtifact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

func (s *Store) CurrentArtifacts(ctx context.Context, projectID string) ([]domain.Artifact, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+artifactCols+` FROM artifacts WHERE project_id=? AND current=1 ORDER BY kind`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

func (s *Store) ArtifactsByRun(ctx context.Context, runID string) ([]domain.Artifact, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+artifactCols+` FROM artifacts WHERE run_id=? ORDER BY kind`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	return scanArtifacts(rows)
}

func scanArtifacts(rows *sql.Rows) ([]domain.Artifact, error) {
	var out []domain.Artifact
	for rows.Next() {
		a, err := scanArtifact(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ---- decisions ----

func (s *Store) PutDecision(ctx context.Context, tx *sql.Tx, d domain.DecisionRecord) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO decisions(id,project_id,event_id,at_step,outcome,target_ref,payload,undone,undone_by,note)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		d.ID, d.ProjectID, d.EventID, d.AtStep, d.Outcome, jstr(d.TargetRef), string(d.Payload),
		boolInt(d.Undone), nullable(d.UndoneBy), d.Note)
	return err
}

func (s *Store) MarkDecisionUndone(ctx context.Context, tx *sql.Tx, id, undoneBy string) error {
	_, err := tx.ExecContext(ctx, `UPDATE decisions SET undone=1, undone_by=? WHERE id=?`, undoneBy, id)
	return err
}

func scanDecision(row interface{ Scan(...any) error }) (domain.DecisionRecord, error) {
	var d domain.DecisionRecord
	var ref, payload, undoneBy sql.NullString
	var undone int
	if err := row.Scan(&d.ID, &d.ProjectID, &d.EventID, &d.AtStep, &d.Outcome,
		&ref, &payload, &undone, &undoneBy, &d.Note); err != nil {
		return d, err
	}
	_ = json.Unmarshal([]byte(ref.String), &d.TargetRef)
	d.Payload = jraw(payload.String)
	d.Undone = undone == 1
	if undoneBy.Valid {
		d.UndoneBy = undoneBy.String
	}
	return d, nil
}

const decisionCols = `id,project_id,event_id,at_step,outcome,target_ref,payload,undone,undone_by,note`

func (s *Store) AllDecisions(ctx context.Context, projectID string) ([]domain.DecisionRecord, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT `+decisionCols+` FROM decisions WHERE project_id=? ORDER BY at_step,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.DecisionRecord
	for rows.Next() {
		d, err := scanDecision(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ---- conflicts ----

func (s *Store) PutConflict(ctx context.Context, tx *sql.Tx, c domain.ConflictRecord) error {
	_, err := tx.ExecContext(ctx,
		`INSERT INTO conflicts(id,project_id,event_id,at_step,object_ref,decision_ids,reason,resolved,resolved_by,resolution)
		 VALUES(?,?,?,?,?,?,?,?,?,?)`,
		c.ID, c.ProjectID, c.EventID, c.AtStep, jstr(c.ObjectRef), jstr(c.DecisionIDs), c.Reason,
		boolInt(c.Resolved), nullable(c.ResolvedBy), c.Resolution)
	return err
}

func (s *Store) ResolveConflict(ctx context.Context, tx *sql.Tx, id, resolvedBy, resolution string) error {
	_, err := tx.ExecContext(ctx,
		`UPDATE conflicts SET resolved=1, resolved_by=?, resolution=? WHERE id=?`, resolvedBy, resolution, id)
	return err
}

func (s *Store) AllConflicts(ctx context.Context, projectID string) ([]domain.ConflictRecord, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,event_id,at_step,object_ref,decision_ids,reason,resolved,resolved_by,resolution
		 FROM conflicts WHERE project_id=? ORDER BY at_step,id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.ConflictRecord
	for rows.Next() {
		var c domain.ConflictRecord
		var obj, ids, resBy, resolution sql.NullString
		var resolved int
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.EventID, &c.AtStep, &obj, &ids,
			&c.Reason, &resolved, &resBy, &resolution); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(obj.String), &c.ObjectRef)
		_ = json.Unmarshal([]byte(ids.String), &c.DecisionIDs)
		c.Resolved = resolved == 1
		if resBy.Valid {
			c.ResolvedBy = resBy.String
		}
		if resolution.Valid {
			c.Resolution = resolution.String
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// ---- exports ----

func (s *Store) RecordExport(ctx context.Context, tx *sql.Tx, projectID, eventID string, atStep int64, sha string, n int) error {
	id := eventID + ":export"
	_, err := tx.ExecContext(ctx,
		`INSERT INTO exports(id,project_id,event_id,at_step,sha256,event_count) VALUES(?,?,?,?,?,?)`,
		id, projectID, eventID, atStep, sha, n)
	return err
}

// ResetMaterialized deletes all derived/materialized rows but keeps the
// ledger, enabling Replay() verification.
func (s *Store) ResetMaterialized(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `
		DELETE FROM artifacts; DELETE FROM runs; DELETE FROM sources;
		DELETE FROM decisions; DELETE FROM conflicts; DELETE FROM exports;
		DELETE FROM projects;`)
	return err
}
