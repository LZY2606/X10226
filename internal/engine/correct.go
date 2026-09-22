package engine

import (
	"math"
	"sort"

	"tga-review/internal/domain"
)

func linearAt(xs, ys []float64, x float64) (float64, string) {
	if len(xs) == 0 {
		return 0, "no_correction_data"
	}
	if x <= xs[0] {
		return ys[0], "clamped_to_first"
	}
	if x >= xs[len(xs)-1] {
		return ys[len(ys)-1], "clamped_to_last"
	}
	i := sort.Search(len(xs)-1, func(i int) bool { return xs[i+1] >= x })
	x0, x1 := xs[i], xs[i+1]
	if x1 == x0 {
		return ys[i], ""
	}
	return ys[i]+(ys[i+1]-ys[i])*(x-x0)/(x1-x0), ""
}

func normDrifts(records []domain.DriftRecord) (xs, ys []float64, err error) {
	type kv struct{ x, y float64 }
	var kvs []kv
	for _, r := range records {
		t, e1 := normQ(r.Time)
		o, e2 := normQ(r.Offset)
		if e1 != nil || e2 != nil {
			return nil, nil, firstErr(e1, e2)
		}
		kvs = append(kvs, kv{t, o})
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].x < kvs[j].x })
	for _, k := range kvs {
		xs = append(xs, k.x)
		ys = append(ys, k.y)
	}
	return xs, ys, nil
}

func normBlanks(points []domain.BlankPoint) (xs, ms, ts []float64, err error) {
	type kv struct{ x, m, t float64 }
	var kvs []kv
	for _, b := range points {
		t, e1 := normQ(b.Time)
		tk, e2 := normQ(b.Temperature)
		m, e3 := normQ(b.Mass)
		if e := firstErr(e1, e2, e3); e != nil {
			return nil, nil, nil, e
		}
		kvs = append(kvs, kv{t, m, tk})
	}
	sort.Slice(kvs, func(i, j int) bool { return kvs[i].x < kvs[j].x })
	for _, k := range kvs {
		xs = append(xs, k.x)
		ms = append(ms, k.m)
		ts = append(ts, k.t)
	}
	return xs, ms, ts, nil
}

func firstErr(errs ...error) error {
	for _, e := range errs {
		if e != nil {
			return e
		}
	}
	return nil
}

// ApplyCorrections subtracts interpolated balance drift and blank-curve mass.
// Interpolation here evaluates corrections at *existing* sample times only; it
// never creates points across a sampling gap.
func ApplyCorrections(projectID, batchID string, fp domain.Fingerprint, norm *domain.DerivedNode, driftVer *domain.SourceVersion, blankVer *domain.SourceVersion, p domain.Params) (*domain.DerivedNode, error) {
	series, ok := norm.Payload.(domain.SeriesPayload)
	if !ok {
		return fail("normalized node payload mismatch")
	}
	var dxs, dys []float64
	var bxs, bms []float64
	srcIDs := append([]string{}, norm.SourceVersionIDs...)
	var err error
	if p.DriftApply && driftVer != nil {
		dxs, dys, err = normDrifts(driftVer.Drifts)
		if err != nil {
			return fail("drift normalization: " + err.Error())
		}
		srcIDs = append(srcIDs, driftVer.ID)
	}
	if p.BlankApply && blankVer != nil {
		bxs, bms, _, err = normBlanks(blankVer.Blanks)
		if err != nil {
			return fail("blank normalization: " + err.Error())
		}
		srcIDs = append(srcIDs, blankVer.ID)
	}
	out := make([]domain.CorrectedPoint, 0, len(series.Points))
	for _, pt := range series.Points {
		cp := domain.CorrectedPoint{Row: pt.Row, TMin: pt.TMin, TK: pt.TK, RawMassMg: pt.MassMg}
		if p.DriftApply && len(dxs) > 0 {
			d, note := linearAt(dxs, dys, pt.TMin)
			cp.DriftMg = d
			cp.Note = note
		}
		if p.BlankApply && len(bxs) > 0 {
			b, _ := linearAt(bxs, bms, pt.TMin)
			cp.BlankMg = b
			cp.BlankApplied = true
		}
		cp.CorrectedMg = cp.RawMassMg - cp.DriftMg - cp.BlankMg
		if math.IsNaN(cp.CorrectedMg) || math.IsInf(cp.CorrectedMg, 0) {
			return fail("non-finite corrected mass at row " + itoa(pt.Row))
		}
		out = append(out, cp)
	}
	payload := domain.CorrectedPayload{Points: out}
	id, err := domain.ContentID(domain.PrefixDerived, string(domain.DKCorrectedSeries), struct {
		In    string
		Drift string
		Blank string
		FP    domain.Fingerprint
		P     domain.CorrectedPayload
	}{norm.ID, versionID(driftVer), versionID(blankVer), fp, payload})
	if err != nil {
		return nil, err
	}
	return &domain.DerivedNode{
		ID: id, ProjectID: projectID, Kind: domain.DKCorrectedSeries, BatchID: batchID,
		Fingerprint: fp, SourceVersionIDs: srcIDs, InputNodeIDs: []string{norm.ID},
		Payload: payload,
	}, nil
}

func versionID(v *domain.SourceVersion) string {
	if v == nil {
		return ""
	}
	return v.ID
}
