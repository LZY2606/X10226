package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"tga/internal/domain"
)

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func (s *Store) ListProjects(ctx context.Context) ([]*domain.Project, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,name,note,seed,created_at FROM projects ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Project
	for rows.Next() {
		var p domain.Project
		var created string
		if err := rows.Scan(&p.ID, &p.Name, &p.Note, &p.Seed, &created); err != nil {
			return nil, err
		}
		p.CreatedAt = parseTime(created)
		out = append(out, &p)
	}
	return out, nil
}

func (s *Store) GetProject(ctx context.Context, id string) (*domain.Project, error) {
	var p domain.Project
	var created string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,name,note,seed,created_at FROM projects WHERE id=?`, id).
		Scan(&p.ID, &p.Name, &p.Note, &p.Seed, &created)
	if err != nil {
		return nil, err
	}
	p.CreatedAt = parseTime(created)
	return &p, nil
}

// LatestSourceVersion returns the highest version recorded for a kind.
func (s *Store) LatestSourceVersion(ctx context.Context, projectID, kind string) (int, string, error) {
	var v sql.NullInt64
	var parent sql.NullString
	err := s.DB.QueryRowContext(ctx,
		`SELECT version,id FROM sources WHERE project_id=? AND kind=? ORDER BY version DESC LIMIT 1`,
		projectID, kind).Scan(&v, &parent)
	if err == sql.ErrNoRows {
		return 0, "", nil
	}
	if err != nil {
		return 0, "", err
	}
	return int(v.Int64), parent.String, nil
}

func (s *Store) ListSources(ctx context.Context, projectID string) ([]*domain.Source, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,kind,version,parent_source_id,label,raw_name,raw_text,raw_encoding,checksum,status,rejected_rows,accepted_rows,seq,created_at
		 FROM sources WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Source
	for rows.Next() {
		var src domain.Source
		var created string
		if err := rows.Scan(&src.ID, &src.ProjectID, &src.Kind, &src.Version, &src.ParentSourceID,
			&src.Label, &src.RawName, &src.RawText, &src.RawEncoding, &src.Checksum, &src.Status,
			&src.RejectedRows, &src.AcceptedRows, &src.Seq, &created); err != nil {
			return nil, err
		}
		src.CreatedAt = parseTime(created)
		out = append(out, &src)
	}
	return out, nil
}

func (s *Store) GetSource(ctx context.Context, id string) (*domain.Source, error) {
	var src domain.Source
	var created string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,project_id,kind,version,parent_source_id,label,raw_name,raw_text,raw_encoding,checksum,status,rejected_rows,accepted_rows,seq,created_at
		 FROM sources WHERE id=?`, id).
		Scan(&src.ID, &src.ProjectID, &src.Kind, &src.Version, &src.ParentSourceID,
			&src.Label, &src.RawName, &src.RawText, &src.RawEncoding, &src.Checksum, &src.Status,
			&src.RejectedRows, &src.AcceptedRows, &src.Seq, &created)
	if err != nil {
		return nil, err
	}
	src.CreatedAt = parseTime(created)
	return &src, nil
}

func (s *Store) ListRows(ctx context.Context, sourceID string) ([]*domain.SourceRow, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,source_id,row_index,raw_line,fields_json,parse_ok,diagnostic
		 FROM source_rows WHERE source_id=? ORDER BY row_index`, sourceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.SourceRow
	for rows.Next() {
		var r domain.SourceRow
		if err := rows.Scan(&r.ID, &r.SourceID, &r.RowIndex, &r.RawLine, &r.Fields, &r.ParseOK, &r.Diagnostic); err != nil {
			return nil, err
		}
		out = append(out, &r)
	}
	return out, nil
}

func (s *Store) ListDiagnostics(ctx context.Context, projectID string) ([]*domain.Diagnostic, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,source_id,run_id,row_index,severity,code,message,raw_ref
		 FROM diagnostics WHERE project_id=? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Diagnostic
	for rows.Next() {
		var d domain.Diagnostic
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.SourceID, &d.RunID, &d.RowIndex,
			&d.Severity, &d.Code, &d.Message, &d.RawRef); err != nil {
			return nil, err
		}
		out = append(out, &d)
	}
	return out, nil
}

func (s *Store) ListEvents(ctx context.Context, projectID string) ([]*domain.Event, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT seq,project_id,type,at,actor,subject_id,payload_json
		 FROM events WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Event
	for rows.Next() {
		var e domain.Event
		var at, pj string
		if err := rows.Scan(&e.Seq, &e.ProjectID, &e.Type, &at, &e.Actor, &e.SubjectID, &pj); err != nil {
			return nil, err
		}
		e.At = parseTime(at)
		_ = json.Unmarshal([]byte(pj), &e.Payload)
		out = append(out, &e)
	}
	return out, nil
}

func (s *Store) EventAt(ctx context.Context, projectID string, seq int64) (*domain.Event, error) {
	var e domain.Event
	var at, pj string
	err := s.DB.QueryRowContext(ctx,
		`SELECT seq,project_id,type,at,actor,subject_id,payload_json
		 FROM events WHERE project_id=? AND seq<=? ORDER BY seq DESC LIMIT 1`, projectID, seq).
		Scan(&e.Seq, &e.ProjectID, &e.Type, &at, &e.Actor, &e.SubjectID, &pj)
	if err != nil {
		return nil, err
	}
	e.At = parseTime(at)
	_ = json.Unmarshal([]byte(pj), &e.Payload)
	return &e, nil
}

func (s *Store) ListParameters(ctx context.Context, projectID string) ([]*domain.Parameters, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,version,parent_id,fingerprint,algorithm_version,atmos_delay_s,
		        thermal_lag_s,dtg_peak_prominence,dtg_peak_min_sep_s,smooth_window_s,allow_overlap,
		        gap_max_interp_s,note,created_at
		 FROM parameters WHERE project_id=? ORDER BY version`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Parameters
	for rows.Next() {
		p, created, err := scanParams(rows)
		if err != nil {
			return nil, err
		}
		p.CreatedAt = parseTime(created)
		out = append(out, p)
	}
	return out, nil
}

func (s *Store) GetParameters(ctx context.Context, id string) (*domain.Parameters, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id,project_id,version,parent_id,fingerprint,algorithm_version,atmos_delay_s,
		        thermal_lag_s,dtg_peak_prominence,dtg_peak_min_sep_s,smooth_window_s,allow_overlap,
		        gap_max_interp_s,note,created_at
		 FROM parameters WHERE id=?`, id)
	p, created, err := scanParams(row)
	if err != nil {
		return nil, err
	}
	p.CreatedAt = parseTime(created)
	return p, nil
}

func (s *Store) LatestParameters(ctx context.Context, projectID string) (*domain.Parameters, error) {
	row := s.DB.QueryRowContext(ctx,
		`SELECT id,project_id,version,parent_id,fingerprint,algorithm_version,atmos_delay_s,
		        thermal_lag_s,dtg_peak_prominence,dtg_peak_min_sep_s,smooth_window_s,allow_overlap,
		        gap_max_interp_s,note,created_at
		 FROM parameters WHERE project_id=? ORDER BY version DESC LIMIT 1`, projectID)
	p, created, err := scanParams(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.CreatedAt = parseTime(created)
	return p, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanParams(r rowScanner) (*domain.Parameters, string, error) {
	var p domain.Parameters
	var created string
	var overlap bool
	err := r.Scan(&p.ID, &p.ProjectID, &p.Version, &p.ParentID, &p.Fingerprint, &p.AlgorithmVer,
		&p.AtmosDelayS, &p.ThermalLagS, &p.PeakProminence, &p.PeakMinSepS, &p.SmoothWindowS,
		&overlap, &p.GapMaxS, &p.Note, &created)
	p.MergeOverlap = overlap
	return &p, created, err
}

func (s *Store) NextRunSeq(ctx context.Context, projectID string) (int64, error) {
	var max sql.NullInt64
	err := s.DB.QueryRowContext(ctx,
		`SELECT COALESCE(MAX(seq),0) FROM runs WHERE project_id=?`, projectID).Scan(&max)
	return max.Int64 + 1, err
}

func (s *Store) ListRuns(ctx context.Context, projectID string) ([]*domain.Run, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,seq,algorithm_version,param_id,param_fingerprint,source_set,current,superseded_by,note,created_at
		 FROM runs WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Run
	for rows.Next() {
		var r domain.Run
		var created string
		var cur bool
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Seq, &r.AlgorithmVer, &r.ParamID,
			&r.ParamFP, &r.SourceSet, &cur, &r.SupersededBy, &r.Note, &created); err != nil {
			return nil, err
		}
		r.Current = cur
		r.CreatedAt = parseTime(created)
		if refs, err := s.runSources(ctx, r.ID); err == nil {
			r.Sources = refs
		}
		out = append(out, &r)
	}
	return out, nil
}

func (s *Store) GetRun(ctx context.Context, id string) (*domain.Run, error) {
	var r domain.Run
	var created string
	var cur bool
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,project_id,seq,algorithm_version,param_id,param_fingerprint,source_set,current,superseded_by,note,created_at
		 FROM runs WHERE id=?`, id).
		Scan(&r.ID, &r.ProjectID, &r.Seq, &r.AlgorithmVer, &r.ParamID,
			&r.ParamFP, &r.SourceSet, &cur, &r.SupersededBy, &r.Note, &created)
	if err != nil {
		return nil, err
	}
	r.Current = cur
	r.CreatedAt = parseTime(created)
	r.Sources, _ = s.runSources(ctx, r.ID)
	return &r, nil
}

func (s *Store) runSources(ctx context.Context, runID string) ([]domain.SourceRef, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT kind,source_id,version,row_indexes_json FROM run_sources WHERE run_id=?`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.SourceRef
	for rows.Next() {
		var ref domain.SourceRef
		var rij string
		if err := rows.Scan(&ref.Kind, &ref.SourceID, &ref.Version, &rij); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(rij), &ref.RowIndexes)
		out = append(out, ref)
	}
	return out, nil
}

func (s *Store) ListStages(ctx context.Context, runID string) ([]*domain.StageCandidate, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,run_id,idx,start_s,end_s,peak_s,peak_dtg,mass_loss_pct,mass_loss_err_pct,
		        overlaps_json,forced_sum_100,state,rejected_reason,unknown,unknown_reason,
		        atmos_reaction_s,valve_s,sources_json
		 FROM stage_candidates WHERE run_id=? ORDER BY idx`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.StageCandidate
	for rows.Next() {
		var c domain.StageCandidate
		var oj, sj string
		var forced, unknown bool
		if err := rows.Scan(&c.ID, &c.RunID, &c.Index, &c.StartS, &c.EndS, &c.PeakS, &c.PeakDTG,
			&c.MassLossPct, &c.MassLossErrPct, &oj, &forced, &c.State, &c.RejectedReason,
			&unknown, &c.UnknownReason, &c.AtmosReactionS, &c.ValveS, &sj); err != nil {
			return nil, err
		}
		c.ForcedSum = forced
		c.Unknown = unknown
		_ = json.Unmarshal([]byte(oj), &c.OverlapsWith)
		_ = json.Unmarshal([]byte(sj), &c.Sources)
		out = append(out, &c)
	}
	return out, nil
}

func (s *Store) GetStage(ctx context.Context, id string) (*domain.StageCandidate, error) {
	var c domain.StageCandidate
	var oj, sj string
	var forced, unknown bool
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,run_id,idx,start_s,end_s,peak_s,peak_dtg,mass_loss_pct,mass_loss_err_pct,
		        overlaps_json,forced_sum_100,state,rejected_reason,unknown,unknown_reason,
		        atmos_reaction_s,valve_s,sources_json
		 FROM stage_candidates WHERE id=?`, id).
		Scan(&c.ID, &c.RunID, &c.Index, &c.StartS, &c.EndS, &c.PeakS, &c.PeakDTG,
			&c.MassLossPct, &c.MassLossErrPct, &oj, &forced, &c.State, &c.RejectedReason,
			&unknown, &c.UnknownReason, &c.AtmosReactionS, &c.ValveS, &sj)
	if err != nil {
		return nil, err
	}
	c.ForcedSum = forced
	c.Unknown = unknown
	_ = json.Unmarshal([]byte(oj), &c.OverlapsWith)
	_ = json.Unmarshal([]byte(sj), &c.Sources)
	return &c, nil
}

func (s *Store) ListResidues(ctx context.Context, runID string) ([]*domain.ResidueResult, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,run_id,at_s,residue_pct,err_pct,unknown,unknown_reason,chain_json,sources_json
		 FROM residues WHERE run_id=? ORDER BY at_s`, runID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.ResidueResult
	for rows.Next() {
		var r domain.ResidueResult
		var cj, sj string
		var unk bool
		if err := rows.Scan(&r.ID, &r.RunID, &r.AtS, &r.Percent, &r.ErrPct, &unk,
			&r.UnknownReason, &cj, &sj); err != nil {
			return nil, err
		}
		r.Unknown = unk
		_ = json.Unmarshal([]byte(cj), &r.Chain)
		_ = json.Unmarshal([]byte(sj), &r.Sources)
		out = append(out, &r)
	}
	return out, nil
}

func (s *Store) ListRules(ctx context.Context, projectID string) ([]*domain.RuleEvidence, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT project_id,code,statement,subject_id,run_id,data_json,created_at
		 FROM rule_evidence WHERE project_id=? ORDER BY id`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.RuleEvidence
	for rows.Next() {
		var r domain.RuleEvidence
		var created, dj string
		if err := rows.Scan(&r.ProjectID, &r.Code, &r.Statement, &r.SubjectID, &r.RunID, &dj, &created); err != nil {
			return nil, err
		}
		r.Data = dj
		r.CreatedAt = parseTime(created)
		out = append(out, &r)
	}
	return out, nil
}

func (s *Store) ListDecisions(ctx context.Context, projectID string) ([]*domain.Decision, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,run_id,target_kind,target_id,verdict,note,param_id,revoked,revokes_id,conflict_of,seq,created_at
		 FROM decisions WHERE project_id=? ORDER BY seq`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*domain.Decision
	for rows.Next() {
		var d domain.Decision
		var created string
		if err := rows.Scan(&d.ID, &d.ProjectID, &d.RunID, &d.TargetKind, &d.TargetID,
			&d.Verdict, &d.Note, &d.ParamID, &d.Revoked, &d.RevokesID, &d.ConflictOf,
			&d.Seq, &created); err != nil {
			return nil, err
		}
		d.CreatedAt = parseTime(created)
		out = append(out, &d)
	}
	return out, nil
}

func (s *Store) GetDecision(ctx context.Context, id string) (*domain.Decision, error) {
	var d domain.Decision
	var created string
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,project_id,run_id,target_kind,target_id,verdict,note,param_id,revoked,revokes_id,conflict_of,seq,created_at
		 FROM decisions WHERE id=?`, id).
		Scan(&d.ID, &d.ProjectID, &d.RunID, &d.TargetKind, &d.TargetID,
			&d.Verdict, &d.Note, &d.ParamID, &d.Revoked, &d.RevokesID, &d.ConflictOf,
			&d.Seq, &created)
	if err != nil {
		return nil, err
	}
	d.CreatedAt = parseTime(created)
	return &d, nil
}
