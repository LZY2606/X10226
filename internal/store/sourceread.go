package store

import (
	"context"
	"database/sql"

	"tga-review/internal/model"
)

// SourceVersionRow is a source version with its raw document attached.
type SourceVersionRow struct {
	model.SourceVersion
	Raw    string
	Format string
	SHA    string
}

// GetSourceVersion loads a source version with raw text.
func (s *Store) GetSourceVersion(ctx context.Context, id string) (SourceVersionRow, error) {
	var r SourceVersionRow
	var ts string
	var active, ok int
	err := s.DB.QueryRowContext(ctx,
		`SELECT id,project_id,kind,parent_id,batch_id,revision,created_at,parse_ok,diagnostic,active,raw,format,sha256
		 FROM source_versions WHERE id=?`, id).Scan(
		&r.ID, &r.ProjectID, &r.Kind, &r.ParentID, &r.BatchID, &r.Revision, &ts,
		&ok, &r.Diagnostic, &active, &r.Raw, &r.Format, &r.SHA)
	if err == nil {
		r.CreatedAt = parseTime(ts)
		r.ParseOK = ok == 1
		r.Active = active == 1
	}
	return r, err
}

// ActiveVersions returns the active source version for each kind present.
func (s *Store) ActiveVersions(ctx context.Context, projectID string) (map[model.SourceKind]SourceVersionRow, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,kind,parent_id,batch_id,revision,created_at,parse_ok,diagnostic,active,raw,format,sha256
		 FROM source_versions WHERE project_id=? AND active=1 ORDER BY kind,revision`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[model.SourceKind]SourceVersionRow{}
	for rows.Next() {
		var r SourceVersionRow
		var ts string
		var active, ok int
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Kind, &r.ParentID, &r.BatchID, &r.Revision, &ts,
			&ok, &r.Diagnostic, &active, &r.Raw, &r.Format, &r.SHA); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(ts)
		r.ParseOK = ok == 1
		r.Active = active == 1
		out[model.SourceKind(r.Kind)] = r
	}
	return out, rows.Err()
}

// ListSourceVersions lists every version of one kind (history).
func (s *Store) ListSourceVersions(ctx context.Context, projectID string, kind model.SourceKind) ([]SourceVersionRow, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT id,project_id,kind,parent_id,batch_id,revision,created_at,parse_ok,diagnostic,active,raw,format,sha256
		 FROM source_versions WHERE project_id=? AND kind=? ORDER BY revision`, projectID, string(kind))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SourceVersionRow
	for rows.Next() {
		var r SourceVersionRow
		var ts string
		var active, ok int
		if err := rows.Scan(&r.ID, &r.ProjectID, &r.Kind, &r.ParentID, &r.BatchID, &r.Revision, &ts,
			&ok, &r.Diagnostic, &active, &r.Raw, &r.Format, &r.SHA); err != nil {
			return nil, err
		}
		r.CreatedAt = parseTime(ts)
		r.ParseOK = ok == 1
		r.Active = active == 1
		out = append(out, r)
	}
	return out, rows.Err()
}

func fPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	x := v.Float64
	return &x
}

// Samples loads parsed sample points.
func (s *Store) Samples(ctx context.Context, versionID string) ([]model.SamplePoint, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT seq,time_raw,temp_raw,mass_raw,time_v,temp_v,mass_v,time_p,temp_p,mass_p,unit_time,unit_temp,unit_mass,row_note
		 FROM sample_points WHERE version_id=? ORDER BY seq`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.SamplePoint
	for rows.Next() {
		var p model.SamplePoint
		var tv, te, mv sql.NullFloat64
		var tp, tep, mp int
		if err := rows.Scan(&p.Seq, &p.TimeRaw, &p.TempRaw, &p.MassRaw, &tv, &te, &mv,
			&tp, &tep, &mp, &p.UnitTime, &p.UnitTemp, &p.UnitMass, &p.RowNote); err != nil {
			return nil, err
		}
		p.VersionID = versionID
		p.TimeS, p.TempS, p.MassS = fPtr(tv), fPtr(te), fPtr(mv)
		p.TimeP, p.TempP, p.MassP = tp == 1, tep == 1, mp == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// Gaps loads declared sampling gaps.
func (s *Store) Gaps(ctx context.Context, versionID string) ([]model.Gap, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT seq,from_v,to_v,unit,raw FROM sample_gaps WHERE version_id=? ORDER BY seq`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.Gap
	for rows.Next() {
		var g model.Gap
		if err := rows.Scan(&g.Seq, &g.FromS, &g.ToS, &g.Unit, &g.Raw); err != nil {
			return nil, err
		}
		g.VersionID = versionID
		out = append(out, g)
	}
	return out, rows.Err()
}

// Drift loads drift nodes.
func (s *Store) Drift(ctx context.Context, versionID string) ([]model.DriftPoint, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT seq,time_raw,drift_raw,time_v,drift_v,time_p,drift_p,unit_time,unit_drift
		 FROM drift_points WHERE version_id=? ORDER BY seq`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.DriftPoint
	for rows.Next() {
		var p model.DriftPoint
		var tv, dv sql.NullFloat64
		var tp, dp int
		if err := rows.Scan(&p.Seq, &p.TimeRaw, &p.DriftRaw, &tv, &dv, &tp, &dp, &p.UnitTime, &p.UnitDrift); err != nil {
			return nil, err
		}
		p.VersionID = versionID
		p.TimeS, p.DriftS = fPtr(tv), fPtr(dv)
		p.TimeP, p.DriftP = tp == 1, dp == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// Furnace loads furnace segments.
func (s *Store) Furnace(ctx context.Context, versionID string) ([]model.FurnaceSeg, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT seq,time_raw,temp_raw,time_v,temp_v,time_p,temp_p,unit_time,unit_temp,seg_kind
		 FROM furnace_segs WHERE version_id=? ORDER BY seq`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.FurnaceSeg
	for rows.Next() {
		var p model.FurnaceSeg
		var tv, te sql.NullFloat64
		var tp, tep int
		if err := rows.Scan(&p.Seq, &p.TimeRaw, &p.TempRaw, &tv, &te, &tp, &tep,
			&p.UnitTime, &p.UnitTemp, &p.Kind); err != nil {
			return nil, err
		}
		p.VersionID = versionID
		p.TimeS, p.TempS = fPtr(tv), fPtr(te)
		p.TimeP, p.TempP = tp == 1, tep == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// Gas loads gas valve events.
func (s *Store) Gas(ctx context.Context, versionID string) ([]model.GasEvent, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT seq,time_raw,gas_raw,action_raw,time_v,time_p,unit_time,gas,action
		 FROM gas_events WHERE version_id=? ORDER BY seq`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.GasEvent
	for rows.Next() {
		var p model.GasEvent
		var tv sql.NullFloat64
		var tp int
		if err := rows.Scan(&p.Seq, &p.TimeRaw, &p.GasRaw, &p.ActionRaw, &tv, &tp,
			&p.UnitTime, &p.Gas, &p.Action); err != nil {
			return nil, err
		}
		p.VersionID = versionID
		p.TimeS = fPtr(tv)
		p.TimeP = tp == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// Blank loads the blank curve.
func (s *Store) Blank(ctx context.Context, versionID string) ([]model.BlankPoint, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT seq,time_raw,mass_raw,time_v,mass_v,time_p,mass_p,unit_time,unit_mass
		 FROM blank_points WHERE version_id=? ORDER BY seq`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.BlankPoint
	for rows.Next() {
		var p model.BlankPoint
		var tv, mv sql.NullFloat64
		var tp, mp int
		if err := rows.Scan(&p.Seq, &p.TimeRaw, &p.MassRaw, &tv, &mv, &tp, &mp,
			&p.UnitTime, &p.UnitMass); err != nil {
			return nil, err
		}
		p.VersionID = versionID
		p.TimeS, p.MassS = fPtr(tv), fPtr(mv)
		p.TimeP, p.MassP = tp == 1, mp == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// Initial loads initial-mass declarations.
func (s *Store) Initial(ctx context.Context, versionID string) ([]model.InitialMass, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT seq,mass_raw,mass_v,mass_p,unit,basis FROM initial_mass WHERE version_id=? ORDER BY seq`, versionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []model.InitialMass
	for rows.Next() {
		var p model.InitialMass
		var mv sql.NullFloat64
		var mp int
		if err := rows.Scan(&p.Seq, &p.MassRaw, &mv, &mp, &p.Unit, &p.Basis); err != nil {
			return nil, err
		}
		p.VersionID = versionID
		p.MassS = fPtr(mv)
		p.MassP = mp == 1
		out = append(out, p)
	}
	return out, rows.Err()
}

// Params loads an algorithm parameter set.
func (s *Store) Params(ctx context.Context, versionID string) (map[string]string, error) {
	rows, err := s.DB.QueryContext(ctx,
		`SELECT key,value FROM param_sets WHERE version_id=? ORDER BY key`, versionID)
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
	return out, rows.Err()
}
