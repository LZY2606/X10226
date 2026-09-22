package derive

import "tga-archive/internal/domain"

// buildResidues emits the initial-basis residue, one residue at every accepted
// or candidate stage endpoint, and the final residue. Each carries a chain.
func buildResidues(pts []Point, basis Basis, cands []StageCandidate, in Inputs, p domain.Params) []Residue {
	var out []Residue
	basePct := domain.Measure{Value: 100, Unit: "%", Valid: true}
	out = append(out, Residue{
		ID: "res:initial", Label: "initial_mass", At: basis.At, Temp: tempAt(pts, basis.At.Value),
		MassCorr: basis.Mass, Percent: basePct,
		Chain: []ChainStep{
			{Step: 1, Label: "initial_mass", Value: basis.Mass, Note: basis.Note, Inputs: []string{sourcesLayer(in, domain.LayerInitialMass)}},
			{Step: 2, Label: "initial_residue_percent", Value: basePct, Formula: "m0/m0*100"},
		},
	})
	// residues at stage endpoints, ordered by time (overlapping stages kept)
	type ep struct {
		t    float64
		cand StageCandidate
	}
	var eps []ep
	for _, c := range cands {
		if c.EndTime.Valid {
			eps = append(eps, ep{c.EndTime.Value, c})
		}
	}
	for i := 1; i < len(eps); i++ {
		for j := i; j > 0 && eps[j-1].t > eps[j].t; j-- {
			eps[j-1], eps[j] = eps[j], eps[j-1]
		}
	}
	seen := map[string]bool{}
	for _, e := range eps {
		id := domain.HashID("res:stage", e.cand.ID, e.cand.EndTime.Value)
		if seen[id] {
			continue
		}
		seen[id] = true
		pt := nearestPoint(pts, e.t)
		percent := invalid("%")
		chainNote := "residual at stage endpoint / m0 * 100"
		if basis.Mass.Valid && basis.Mass.Value != 0 && pt.MassCorr.Valid {
			percent = measure(pt.MassCorr.Value/basis.Mass.Value*100, "%")
		}
		out = append(out, Residue{
			ID: "res:" + e.cand.Key, Label: "residue_after_" + e.cand.Key, At: e.cand.EndTime, Temp: e.cand.EndTemp,
			MassCorr: pt.MassCorr, Percent: percent, Unknown: !percent.Valid,
			Chain: []ChainStep{
				{Step: 1, Label: "m0", Value: basis.Mass, Inputs: []string{sourcesLayer(in, domain.LayerInitialMass)}},
				{Step: 2, Label: "corrected_mass_at_endpoint", Value: pt.MassCorr, Inputs: []string{sourcesLayer(in, domain.LayerSamples), sourcesLayer(in, domain.LayerDrift), sourcesLayer(in, domain.LayerBlank)}},
				{Step: 3, Label: "residue_percent", Value: percent, Formula: "m_corr(t1)/m0*100", Note: chainNote, Inputs: []string{e.cand.ID}},
			},
		})
	}
	final := finalResidue(pts, basis, in)
	out = append(out, final)
	return out
}

func finalResidue(pts []Point, basis Basis, in Inputs) Residue {
	last := Point{}
	found := false
	for i := len(pts) - 1; i >= 0; i-- {
		if pts[i].Time.Valid && pts[i].MassCorr.Valid {
			last, found = pts[i], true
			break
		}
	}
	if !found {
		return unknownFinalResidue(pts)
	}
	pct := invalid("%")
	if basis.Mass.Valid && basis.Mass.Value != 0 {
		pct = measure(last.MassCorr.Value/basis.Mass.Value*100, "%")
	}
	return Residue{
		ID: "res:final", Label: "final_residue", At: last.Time, Temp: last.Temp, MassCorr: last.MassCorr, Percent: pct, Unknown: !pct.Valid,
		Chain: []ChainStep{
			{Step: 1, Label: "initial_mass", Value: basis.Mass, Note: basis.Note, Inputs: []string{sourcesLayer(in, domain.LayerInitialMass)}},
			{Step: 2, Label: "drift_rate", Value: driftRate(in), Formula: "subtract rate*(t-t_anchor)", Inputs: []string{sourcesLayer(in, domain.LayerDrift)}},
			{Step: 3, Label: "blank_offset_at_t", Value: blankOffsetNote(in), Note: "subtract blank curve at endpoint (no extrapolation)", Inputs: []string{sourcesLayer(in, domain.LayerBlank)}},
			{Step: 4, Label: "final_corrected_mass", Value: last.MassCorr, Formula: "m_raw - drift - blank", Inputs: []string{sourcesLayer(in, domain.LayerSamples)}},
			{Step: 5, Label: "final_residue_percent", Value: pct, Formula: "m_corr(final)/m0*100", Inputs: []string{"res:final#step4", sourcesLayer(in, domain.LayerInitialMass)}},
		},
	}
}

func driftRate(in Inputs) domain.Measure {
	if in.Drift != nil && in.Drift.Drift != nil {
		return in.Drift.Drift.Rate
	}
	return measure(0, domain.UnitMgPerS)
}

func blankOffsetNote(in Inputs) domain.Measure {
	if in.Blank != nil && len(in.Blank.Blank) > 0 {
		return measure(0, domain.UnitMg)
	}
	m := invalid(domain.UnitMg)
	m.Note = "no blank curve"
	return m
}

func nearestPoint(pts []Point, t float64) Point {
	best := 0
	for i := range pts {
		if pts[i].Time.Valid {
			if abs(pts[i].Time.Value-t) < abs(pts[best].Time.Value-t) {
				best = i
			}
		}
	}
	return pts[best]
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

