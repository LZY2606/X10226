package store

import (
	"context"
	"database/sql"
	"encoding/json"

	"tga-review/internal/domain"
	"tga-review/internal/importer"
)

// Layers loads the current contents of every raw layer for a project. All
// accepted submissions remain present (newer revisions are new rows, never
// overwrites); the engine selects "current" semantics such as the latest
// initial-mass revision.
func (s *Store) Layers(ctx context.Context, projectID string) (importer.LayerData, error) {
	var d importer.LayerData

	srows, err := s.db.QueryContext(ctx,
		`SELECT id,ordinal,raw_t,unit_t,t,raw_mass,unit_mass,mass,raw_temp,unit_temp,temp,diagnostic
		 FROM samples WHERE project_id=? ORDER BY ordinal, id`, projectID)
	if err != nil {
		return d, err
	}
	for srows.Next() {
		var p domain.TimePoint
		var rt, ut, rm, um, rtp, utp sql.NullString
		var t, m, tmp sql.NullFloat64
		var diagnostic sql.NullString
		if err := srows.Scan(&p.ID, &p.Ordinal, &rt, &ut, &t, &rm, &um, &m, &rtp, &utp, &tmp, &diagnostic); err != nil {
			srows.Close()
			return d, err
		}
		p.T = rebuildQ(rt, ut, t, sql.NullString{})
		p.Mass = rebuildQ(rm, um, m, sql.NullString{})
		p.Temp = rebuildQ(rtp, utp, tmp, sql.NullString{})
		_ = diagnostic
		if diagnostic.Valid && diagnostic.String != "" {
			p.T.Diagnostic, p.Mass.Diagnostic, p.Temp.Diagnostic = diagnostic.String, diagnostic.String, diagnostic.String
		}
		d.Samples = append(d.Samples, p)
	}
	srows.Close()

	drows, err := s.db.QueryContext(ctx,
		`SELECT id,ordinal,kind,raw_t,unit_t,t,raw_offset,unit_offset,offset_v,diagnostic
		 FROM drift WHERE project_id=? ORDER BY ordinal,id`, projectID)
	if err != nil {
		return d, err
	}
	for drows.Next() {
		var x domain.DriftCorrection
		var rt, ro, ut, uo, diagS sql.NullString
		var t, o sql.NullFloat64
		if err := drows.Scan(&x.ID, &x.Ordinal, &x.Kind, &rt, &ut, &t, &ro, &uo, &o, &diagS); err != nil {
			drows.Close()
			return d, err
		}
		x.T = rebuildQ(rt, ut, t, diagS)
		x.Offset = rebuildQ(ro, uo, o, diagS)
		d.Drift = append(d.Drift, x)
	}
	drows.Close()

	prows, err := s.db.QueryContext(ctx,
		`SELECT id,ordinal,label,t_start,t_end,temp_start,temp_end FROM program WHERE project_id=? ORDER BY ordinal,id`, projectID)
	if err != nil {
		return d, err
	}
	for prows.Next() {
		var x domain.ProgramSegment
		var label sql.NullString
		var t1, t2, tp1, tp2 sql.NullFloat64
		if err := prows.Scan(&x.ID, &x.Ordinal, &label, &t1, &t2, &tp1, &tp2); err != nil {
			prows.Close()
			return d, err
		}
		x.Label = label.String
		x.TStart = canonicalQ(t1, domain.UnitSecond)
		x.TEnd = canonicalQ(t2, domain.UnitSecond)
		x.TempStart = canonicalQ(tp1, domain.UnitCelsius)
		x.TempEnd = canonicalQ(tp2, domain.UnitCelsius)
		d.Program = append(d.Program, x)
	}
	prows.Close()

	grows, err := s.db.QueryContext(ctx,
		`SELECT id,ordinal,from_gas,to_gas,t,flow,raw_t,unit_t,diagnostic FROM gas WHERE project_id=? ORDER BY ordinal,id`, projectID)
	if err != nil {
		return d, err
	}
	for grows.Next() {
		var x domain.GasEvent
		var rt, ut, diagS sql.NullString
		var t, f sql.NullFloat64
		if err := grows.Scan(&x.ID, &x.Ordinal, &x.FromGas, &x.ToGas, &t, &f, &rt, &ut, &diagS); err != nil {
			grows.Close()
			return d, err
		}
		x.T = rebuildQ(rt, ut, t, diagS)
		x.Flow = canonicalQ(f, domain.UnitSCCM)
		d.Gas = append(d.Gas, x)
	}
	grows.Close()

	blrows, err := s.db.QueryContext(ctx,
		`SELECT id,ordinal,t,mass,raw_t,unit_t,raw_mass,unit_mass,diagnostic FROM blank WHERE project_id=? ORDER BY ordinal,id`, projectID)
	if err != nil {
		return d, err
	}
	for blrows.Next() {
		var x domain.BlankPoint
		var rt, ut, rm, um, diagS sql.NullString
		var t, m sql.NullFloat64
		if err := blrows.Scan(&x.ID, &x.Ordinal, &t, &m, &rt, &ut, &rm, &um, &diagS); err != nil {
			blrows.Close()
			return d, err
		}
		x.T = rebuildQ(rt, ut, t, diagS)
		x.Mass = rebuildQ(rm, um, m, diagS)
		d.Blank = append(d.Blank, x)
	}
	blrows.Close()

	irows, err := s.db.QueryContext(ctx,
		`SELECT id,parent_id,ordinal,raw_mass,unit_mass,mass,reason,diagnostic
		 FROM initial_mass WHERE project_id=? ORDER BY ordinal,id`, projectID)
	if err != nil {
		return d, err
	}
	for irows.Next() {
		var x domain.InitialMass
		var parent, rm, um, reason, diagS sql.NullString
		var m sql.NullFloat64
		if err := irows.Scan(&x.ID, &parent, &x.Ordinal, &rm, &um, &m, &reason, &diagS); err != nil {
			irows.Close()
			return d, err
		}
		x.ParentID = parent.String
		x.Reason = reason.String
		x.Mass = rebuildQ(rm, um, m, diagS)
		d.Initial = append(d.Initial, x)
	}
	irows.Close()
	return d, nil
}

func canonicalQ(v sql.NullFloat64, unit string) domain.Quantity {
	q := domain.Quantity{Canonical: unit, Unit: unit}
	if v.Valid {
		q.ConvertedValue = v.Float64
		q.Value = v.Float64
		q.Convertible = true
	}
	return q
}

func rebuildQ(raw, unit sql.NullString, v sql.NullFloat64, diagnostic sql.NullString) domain.Quantity {
	q := domain.Quantity{Raw: raw.String, Unit: unit.String, Diagnostic: diagnostic.String}
	if v.Valid {
		q.ConvertedValue = v.Float64
		q.Value = v.Float64
		q.Convertible = true
	}
	switch unit.String {
	case domain.UnitSecond, domain.UnitMinute:
		q.Canonical = domain.UnitSecond
	case domain.UnitMilligram, domain.UnitGram, domain.UnitMicrogram:
		q.Canonical = domain.UnitMilligram
	case domain.UnitCelsius, domain.UnitKelvin:
		q.Canonical = domain.UnitCelsius
	case domain.UnitSCCM:
		q.Canonical = domain.UnitSCCM
	}
	return q
}

// Batches lists import submissions in insertion order.
func (s *Store) Batches(ctx context.Context, projectID string) ([]domain.Batch, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id,created_at,kind,source_ref,accepted_layers,rejected_layers,note,payload
		 FROM batches WHERE project_id=? ORDER BY rowid`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.Batch
	for rows.Next() {
		var b domain.Batch
		var acc, rej, note, payload sql.NullString
		if err := rows.Scan(&b.ID, &b.CreatedAt, &b.Kind, &b.SourceRef, &acc, &rej, &note, &payload); err != nil {
			return nil, err
		}
		if payload.Valid {
			b.Payload = json.RawMessage(payload.String)
		}
		if acc.String != "" {
			_ = json.Unmarshal([]byte(acc.String), &b.AcceptedLayers)
		}
		if rej.String != "" {
			_ = json.Unmarshal([]byte(rej.String), &b.RejectedLayers)
		}
		b.Note = note.String
		out = append(out, b)
	}
	return out, rows.Err()
}

// ProjectName returns a project name.
func (s *Store) ProjectName(ctx context.Context, id string) (string, error) {
	var name string
	err := s.db.QueryRowContext(ctx, `SELECT name FROM projects WHERE id=?`, id).Scan(&name)
	return name, err
}

// ProjectExists reports whether a project exists.
func (s *Store) ProjectExists(ctx context.Context, id string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(1) FROM projects WHERE id=?`, id).Scan(&n)
	return n > 0, err
}

func unmarshalCSV(_ string, _ *[]string)        {}
func unmarshalJSON(_ string, _ any)             {}
