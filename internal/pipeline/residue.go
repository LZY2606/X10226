package pipeline

import "tgaaudit/internal/domain"

// buildResidue computes the final residual mass fraction from the last tail
// window of real measured points in the terminal segment. Tail mean and
// standard deviation are first-class; if the tail window cannot be populated
// without crossing a gap the residue is "unknown", not guessed.
func (d *Derivation) buildResidue(p domain.Params) {
	pts := d.Corrected.Points
	if len(pts) == 0 {
		return
	}
	last := -1
	seg := -1
	for i := range pts {
		if !pts[i].Missing && pts[i].MassFraction != nil {
			last = i
			seg = d.segOf[i]
		}
	}
	if last < 0 {
		return
	}
	tEnd := pts[last].TS
	tFrom := tEnd - p.ResidueTailS
	var vals []float64
	crossesGap := false
	for i := range pts {
		if pts[i].Missing || pts[i].MassFraction == nil || d.segOf[i] != seg {
			continue
		}
		if pts[i].TS >= tFrom && pts[i].TS <= tEnd {
			vals = append(vals, *pts[i].MassFraction)
		}
	}
	for _, g := range d.Gaps {
		if intervalsTouch(tFrom, tEnd, g.FromT, g.ToT) {
			crossesGap = true
		}
	}
	for _, set := range d.PeakSets {
		rd := ResidueData{Key: "residue-" + set.Hypothesis, Hypothesis: set.Hypothesis,
			InitialMassKg: d.Corrected.InitialMassKg}
		rd.TailFromT = &tFrom
		rd.TailToT = &tEnd
		if len(vals) < 2 || crossesGap {
			rd.Status = domain.StatusUnknown
			if len(vals) < 2 {
				rd.RejectReasons = append(rd.RejectReasons,
					"residue tail contains fewer than 2 measured points")
			}
			if crossesGap {
				rd.RejectReasons = append(rd.RejectReasons,
					"residue tail window touches a data gap; no interpolation allowed")
			}
			d.Residues[set.Hypothesis] = rd
			continue
		}
		mean := 0.0
		for _, v := range vals {
			mean += v
		}
		mean /= float64(len(vals))
		var sd float64
		for _, v := range vals {
			sd += (v - mean) * (v - mean)
		}
		sd = sqrtF(sd / float64(len(vals)-1))
		pct := mean * 100
		u := 2 * sd
		rd.Value, rd.Percent, rd.Uncertainty = &mean, &pct, &u
		rd.NPoints = len(vals)
		rd.Status = domain.StatusRetained
		if d.Corrected.InitialMassKg != nil {
			im := *d.Corrected.InitialMassKg
			rd.Chain = []ChainStep{
				{Order: 1, Label: "declared/corrected initial mass m0",
					Value: im, Unit: "kg", Path: "corrected.initial_mass_kg"},
				{Order: 2, Label: "tail window start t1", Value: tFrom,
					Unit: "s", Path: "residue.tail_from_t"},
				{Order: 3, Label: "tail window end t2", Value: tEnd,
					Unit: "s", Path: "residue.tail_to_t"},
				{Order: 4, Label: "mean corrected mass over measured tail points",
					Value: mean * im, Unit: "kg",
					Path: "residue.mean_corrected_mass_kg"},
				{Order: 5, Label: "residue = mean(m_corr)/m0", Value: mean,
					Unit: "fraction", Path: "residue.value"},
				{Order: 6, Label: "residue percent", Value: pct,
					Unit: "%", Path: "residue.percent"},
			}
		}
		d.Residues[set.Hypothesis] = rd
	}
}

func sqrtF(x float64) float64 {
	if x <= 0 {
		return 0
	}
	z := x
	for i := 0; i < 20; i++ {
		z = (z + x/z) / 2
	}
	return z
}
