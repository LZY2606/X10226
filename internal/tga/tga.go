// Package tga implements the domain computations: balance-drift correction,
// DTG, overlapping stage detection, atmosphere-delay mapping and residue.
// Sampling gaps are never interpolated across; stages may overlap and their
// mass losses are never forced to sum to 100%.
package tga

import (
	"fmt"
	"math"
	"sort"

	"tgareview/internal/domain"
)

// interp returns the linear interpolation of (ts, vs) at t, but only between
// consecutive points that are not separated by a gap. ok=false when t lies
// outside the series or across a gap.
func interp(ts, vs []float64, gapAfter []bool, t float64) (float64, bool) {
	n := len(ts)
	if n == 0 || t < ts[0] || t > ts[n-1] {
		return 0, false
	}
	for i := 0; i+1 < n; i++ {
		if t >= ts[i] && t <= ts[i+1] {
			if gapAfter[i] && t > ts[i] && t < ts[i+1] {
				return 0, false // would require interpolating across a gap
			}
			if ts[i+1] == ts[i] {
				return vs[i], true
			}
			f := (t - ts[i]) / (ts[i+1] - ts[i])
			return vs[i] + f*(vs[i+1]-vs[i]), true
		}
	}
	return vs[n-1], true
}

// CorrectDrift subtracts the balance drift curve and the blank-curve
// deviation from the raw sample masses, then normalizes to the initial mass.
// Points whose correction would require interpolating across a sampling gap
// keep their raw mass (the correction is piecewise and never spans a gap).
func CorrectDrift(raw domain.RawPointsDoc, drift domain.DriftDoc, blank domain.BlankDoc, initialMassMg float64) domain.CorrectedCurve {
	dts, dvs := []float64{}, []float64{}
	for _, p := range drift.Points {
		if p.Time.OK && p.Drift.OK {
			dts = append(dts, p.Time.Value)
			dvs = append(dvs, p.Drift.Value)
		}
	}
	bts, bvs := []float64{}, []float64{}
	var blank0 float64
	for i, p := range blank.Points {
		if p.Time.OK && p.Mass.OK {
			bts = append(bts, p.Time.Value)
			bvs = append(bvs, p.Mass.Value)
			if i == 0 {
				blank0 = p.Mass.Value
			}
		}
	}
	noGaps := make([]bool, len(dts))
	curve := domain.CorrectedCurve{}
	for _, p := range raw.Points {
		if !p.Time.OK || !p.Mass.OK || !p.Temp.OK {
			continue // unconvertible values stay in the raw layer with diagnostics
		}
		t := p.Time.Value
		m := p.Mass.Value
		if d, ok := interp(dts, dvs, noGaps, t); ok {
			m -= d
		}
		if b, ok := interp(bts, bvs, noGaps, t); ok {
			m -= b - blank0
		}
		curve.Points = append(curve.Points, domain.CorrPoint{
			T: t, Temp: p.Temp.Value, MassMg: m, MassPct: 100 * m / initialMassMg,
		})
		if p.GapAfter {
			curve.GapAfter = append(curve.GapAfter, t)
		}
	}
	return curve
}

// ComputeDTG differentiates the corrected mass-% curve with a moving-average
// smoothing window so non-monotonic noise does not create false peaks.
func ComputeDTG(curve domain.CorrectedCurve, window int) domain.DTGCurve {
	pts := curve.Points
	n := len(pts)
	if n < 3 {
		return domain.DTGCurve{}
	}
	if window < 1 {
		window = 1
	}
	if window%2 == 0 {
		window++
	}
	half := window / 2
	smoothed := make([]float64, n)
	for i := 0; i < n; i++ {
		lo, hi := i-half, i+half
		if lo < 0 {
			lo = 0
		}
		if hi >= n {
			hi = n - 1
		}
		sum := 0.0
		for j := lo; j <= hi; j++ {
			sum += pts[j].MassPct
		}
		smoothed[i] = sum / float64(hi-lo+1)
	}
	out := domain.DTGCurve{}
	for i := 1; i < n; i++ {
		dt := pts[i].T - pts[i-1].T
		if dt <= 0 {
			continue
		}
		d := (smoothed[i] - smoothed[i-1]) / dt * 60 // %/min
		out.Points = append(out.Points, domain.DTGPoint{
			T: (pts[i].T + pts[i-1].T) / 2, Temp: (pts[i].Temp + pts[i-1].Temp) / 2, DTG: d,
		})
	}
	return out
}

// BuildAtmosphere maps valve events to delay-corrected reaction intervals.
// The valve switch time is never the sample reaction time: the gas must
// travel (AtmosphereDelayS) and the sample temperature lags the furnace
// (ThermalLagS).
func BuildAtmosphere(gas domain.GasEventsDoc, p domain.Params) domain.AtmosphereTimeline {
	delay := p.AtmosphereDelayS + p.ThermalLagS
	tl := domain.AtmosphereTimeline{}
	for _, e := range gas.Events {
		if !e.Time.OK {
			continue
		}
		tl.Segments = append(tl.Segments, domain.AtmoSegment{
			Gas: e.ToGas, ValveS: e.Time.Value, ReactionS: e.Time.Value + delay, DelayS: delay,
		})
	}
	sort.Slice(tl.Segments, func(i, j int) bool { return tl.Segments[i].ReactionS < tl.Segments[j].ReactionS })
	for i := range tl.Segments {
		if i+1 < len(tl.Segments) {
			tl.Segments[i].EndS = tl.Segments[i+1].ReactionS
		} else {
			tl.Segments[i].EndS = math.Inf(1)
		}
	}
	return tl
}

// GasAt returns the atmosphere active at reaction time t.
func GasAt(tl domain.AtmosphereTimeline, t float64) string {
	gas := "initial"
	for _, seg := range tl.Segments {
		if t >= seg.ReactionS {
			gas = seg.Gas
		}
	}
	return gas
}

// DetectStages finds mass-loss stage candidates from the DTG curve. Peaks
// are located on the loss rate (-DTG); onset/end are the half-prominence
// crossings, so neighbouring stages may overlap. Candidates failing the
// width/prominence gates are kept with their rejection reason. Mass losses
// are reported as-is and never normalized to 100%.
func DetectStages(dtg domain.DTGCurve, curve domain.CorrectedCurve, tl domain.AtmosphereTimeline, p domain.Params) domain.StageSet {
	set := domain.StageSet{}
	pts := dtg.Points
	n := len(pts)
	if n == 0 {
		return set
	}
	loss := make([]float64, n)
	for i, pt := range pts {
		loss[i] = -pt.DTG // positive = losing mass
	}
	// Local maxima of the loss rate.
	var peaks []int
	for i := 1; i+1 < n; i++ {
		if loss[i] > loss[i-1] && loss[i] >= loss[i+1] && loss[i] > 0 {
			peaks = append(peaks, i)
		}
	}
	massAt := func(t float64) (float64, bool) {
		ts := make([]float64, len(curve.Points))
		vs := make([]float64, len(curve.Points))
		gaps := make([]bool, len(curve.Points))
		gapSet := map[int]bool{}
		for gi, gt := range curve.GapAfter {
			_ = gi
			for i, cp := range curve.Points {
				if cp.T == gt {
					gapSet[i] = true
				}
			}
		}
		for i, cp := range curve.Points {
			ts[i] = cp.T
			vs[i] = cp.MassPct
			gaps[i] = gapSet[i]
		}
		return interp(ts, vs, gaps, t)
	}
	stageN := 0
	for _, pk := range peaks {
		// prominence: height above the higher of the two surrounding valleys
		lo, hi := pk, pk
		for lo > 0 && loss[lo] > 0 {
			lo--
		}
		for hi < n-1 && loss[hi] > 0 {
			hi++
		}
		valley := math.Max(loss[lo], loss[hi])
		prom := loss[pk] - valley
		peakT := pts[pk].T
		if prom < p.MinProminencePct {
			set.Rejected = append(set.Rejected, domain.RejectedStage{
				PeakS: peakT, Reason: fmt.Sprintf("prominence %.3f %%/min below gate %.3f", prom, p.MinProminencePct)})
			continue
		}
		// half-prominence crossings define onset/end (allows overlap)
		half := valley + prom/2
		onset, end := pts[lo].T, pts[hi].T
		for i := pk; i > lo; i-- {
			if loss[i] >= half && loss[i-1] < half {
				f := (half - loss[i-1]) / (loss[i] - loss[i-1])
				onset = pts[i-1].T + f*(pts[i].T-pts[i-1].T)
				break
			}
		}
		for i := pk; i < hi; i++ {
			if loss[i] >= half && loss[i+1] < half {
				f := (half - loss[i]) / (loss[i+1] - loss[i])
				end = pts[i].T + f*(pts[i+1].T-pts[i].T)
				break
			}
		}
		if end-onset < p.MinStageWidthS {
			set.Rejected = append(set.Rejected, domain.RejectedStage{
				PeakS: peakT, Reason: fmt.Sprintf("width %.0f s below gate %.0f s", end-onset, p.MinStageWidthS)})
			continue
		}
		m0, ok0 := massAt(onset)
		m1, ok1 := massAt(end)
		if !ok0 || !ok1 {
			set.Rejected = append(set.Rejected, domain.RejectedStage{
				PeakS: peakT, Reason: "boundary falls inside a sampling gap; no interpolation across gaps"})
			continue
		}
		stageN++
		set.Candidates = append(set.Candidates, domain.Stage{
			ID: fmt.Sprintf("stage-%d", stageN), Label: fmt.Sprintf("Stage %d", stageN),
			OnsetS: onset, PeakS: peakT, EndS: end, PeakTempC: pts[pk].Temp,
			MassLossPct: m0 - m1, Atmosphere: GasAt(tl, peakT),
		})
	}
	// Overlap detection: stages whose [onset,end] intervals intersect.
	for i := range set.Candidates {
		for j := i + 1; j < len(set.Candidates); j++ {
			a, b := set.Candidates[i], set.Candidates[j]
			if a.OnsetS < b.EndS && b.OnsetS < a.EndS {
				set.Candidates[i].Overlaps = append(set.Candidates[i].Overlaps, b.ID)
				set.Candidates[j].Overlaps = append(set.Candidates[j].Overlaps, a.ID)
			}
		}
	}
	domain.SortStages(set.Candidates)
	sum := 0.0
	for _, c := range set.Candidates {
		sum += c.MassLossPct
	}
	set.SumPct = sum
	// Machine-readable judgments.
	if len(set.Candidates) > 0 {
		overlapped := false
		for _, c := range set.Candidates {
			if len(c.Overlaps) > 0 {
				overlapped = true
			}
		}
		if overlapped {
			set.Judgments = append(set.Judgments, domain.Judgment{
				Code:   "overlap_allowed",
				Detail: "stage intervals overlap; candidates are kept side by side and mass losses are not forced to sum to 100%",
			})
		}
		set.Judgments = append(set.Judgments, domain.Judgment{
			Code:   "no_forced_sum",
			Detail: fmt.Sprintf("sum of stage mass losses is %.2f%%; reported as-is, never normalized to 100%%", sum),
		})
	}
	if len(curve.GapAfter) > 0 {
		affected := []string{}
		for _, g := range curve.GapAfter {
			affected = append(affected, fmt.Sprintf("t=%.0fs", g))
		}
		set.Judgments = append(set.Judgments, domain.Judgment{
			Code:     "gap_no_interpolate",
			Detail:   "sampling gaps split the curve; no interpolation across gaps; candidates whose boundary falls inside a gap are rejected",
			Affected: affected,
		})
	}
	return set
}

// ComputeResidue evaluates the final residue with an expandable calculation
// chain referencing the exact source versions.
func ComputeResidue(curve domain.CorrectedCurve, mass domain.MassLayersDoc, massRef, rawRef domain.SourceRef, p domain.Params) (domain.ResidueResult, error) {
	init, ok := mass.NetInitialMass()
	if !ok {
		return domain.ResidueResult{}, fmt.Errorf("mass_layers: no usable 'net' initial-mass layer")
	}
	var final domain.CorrPoint
	found := false
	for _, cp := range curve.Points {
		if cp.T >= p.ResidueFromS {
			final = cp
			found = true
		}
	}
	if !found {
		return domain.ResidueResult{}, fmt.Errorf("corrected curve has no points at/after residue_from_s=%g", p.ResidueFromS)
	}
	pct := 100 * final.MassMg / init.Value
	return domain.ResidueResult{
		FinalMassPct: pct,
		FinalMassMg:  final.MassMg,
		InitialMass:  init,
		Chain: []domain.ChainStep{
			{Desc: "sample initial mass (net layer)", Value: init.Value, Unit: init.Unit, Sources: []domain.SourceRef{massRef}},
			{Desc: fmt.Sprintf("final corrected mass at t=%.0f s", final.T), Value: final.MassMg, Unit: "mg", Sources: []domain.SourceRef{rawRef}},
			{Desc: "residue = final / initial × 100", Value: pct, Unit: "%", Sources: []domain.SourceRef{massRef, rawRef}},
		},
	}, nil
}
