package pipeline

import (
	"fmt"
	"math"
	"sort"

	"tga/internal/domain"
)

// fingerprint serialises the parameters so every derived value can quote the
// exact parameter set it was computed with.
func fingerprint(p domain.AnalysisParams) string {
	return hnum("fp",
		p.GapThresholdS, "|", p.GasDelayS, "|", p.TempLagS, "|", p.DTGWindowS, "|",
		p.PeakProminenceMGPM, "|", p.NoiseThresholdMG, "|", p.ShoulderDepthMGPM, "|", p.BlankSubtract)
}

// Run executes the pinned algorithm over one imported snapshot. It is pure and
// deterministic: identical inputs always produce identical stable identities.
func Run(layers *domain.ImportedLayers, params domain.AnalysisParams, sourceVersion, paramsVersion int) *Analysis {
	an := &Analysis{
		SourceVersion:    sourceVersion,
		ParamsVersion:    paramsVersion,
		AlgoVersion:      domain.AlgorithmVersion,
		ParamFingerprint: fingerprint(params),
	}
	an.AnalysisID = hnum("ana", domain.AlgorithmVersion, sourceVersion, an.ParamFingerprint, len(layers.Samples))

	kept := orderedKept(layers, &an.Findings)
	buildSegments(an, kept, params)
	correctMasses(an, layers, kept, params)
	computeDTG(an)
	flagNoise(an, params)
	findPeakGroups(an, params)
	buildAtmosphere(an, layers, params)
	buildCandidates(an, layers, params)
	buildResidue(an, layers)

	sort.SliceStable(an.Findings, func(i, j int) bool {
		if an.Findings[i].StartS != an.Findings[j].StartS {
			return an.Findings[i].StartS < an.Findings[j].StartS
		}
		return an.Findings[i].Code < an.Findings[j].Code
	})
	return an
}

func orderedKept(layers *domain.ImportedLayers, findings *[]domain.Finding) []domain.SamplePoint {
	var kept []domain.SamplePoint
	badCount := 0
	for _, s := range layers.Samples {
		if !s.Kept {
			badCount++
			continue
		}
		kept = append(kept, s)
	}
	if badCount > 0 {
		*findings = append(*findings, domain.Finding{
			Code: "unconvertible_points_retained", Severity: "warn",
			Message: fmt.Sprintf("%d sample row(s) kept in raw layer but excluded from derived curves", badCount),
		})
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Time.Value < kept[j].Time.Value })
	return kept
}

// buildSegments splits the ordered points wherever dt exceeds the gap threshold.
// Nothing is interpolated across the resulting gaps.
func buildSegments(an *Analysis, kept []domain.SamplePoint, p domain.AnalysisParams) {
	if len(kept) == 0 {
		an.Findings = append(an.Findings, domain.Finding{Code: "no_usable_points", Severity: "error", Message: "no normalisable sample points"})
		return
	}
	segFrom := 0
	segIdx := 0
	flushSeg := func(to int) {
		an.Segments = append(an.Segments, Segment{
			ID: hnum("seg", an.AnalysisID, segIdx), Index: segIdx,
			StartS: kept[segFrom].Time.Value, EndS: kept[to].Time.Value,
			PointFrom: segFrom, PointTo: to,
		})
		segIdx++
	}
	for i := 1; i < len(kept); i++ {
		dt := kept[i].Time.Value - kept[i-1].Time.Value
		if dt > p.GapThresholdS {
			gid := len(an.Gaps)
			an.Gaps = append(an.Gaps, Gap{
				ID: hnum("gap", an.AnalysisID, gid),
				StartS: kept[i-1].Time.Value, EndS: kept[i].Time.Value, WidthS: dt,
			})
			an.Findings = append(an.Findings, domain.Finding{
				Code: "sampling_gap", Severity: "warn",
				Message: fmt.Sprintf("sampling gap %.1fs exceeds threshold %.0fs; no interpolation across it", dt, p.GapThresholdS),
				StartS: kept[i-1].Time.Value, EndS: kept[i].Time.Value,
				Ref: an.Gaps[gid].ID,
			})
			flushSeg(i - 1)
			segFrom = i
		}
	}
	flushSeg(len(kept) - 1)
}

// interpolateCorrection evaluates a calibration table at t via linear
// interpolation, but only between bracketing calibration points: it never
// extrapolates beyond the calibration range.
func interpTable(times, vals []float64, t float64) (float64, bool) {
	if len(times) == 0 || t < times[0] || t > times[len(times)-1] {
		return 0, false
	}
	for i := 1; i < len(times); i++ {
		if t <= times[i] {
			if times[i] == times[i-1] {
				return vals[i], true
			}
			f := (t - times[i-1]) / (times[i]-times[i-1])
			return vals[i-1] + f*(vals[i]-vals[i-1]), true
		}
	}
	return vals[len(vals)-1], true
}

func correctMasses(an *Analysis, layers *domain.ImportedLayers, kept []domain.SamplePoint, p domain.AnalysisParams) {
	if len(an.Segments) == 0 {
		return
	}
	driftT, driftV := calibration(layers.Drift, func(d domain.DriftPoint) (float64, float64) { return d.Time.Value, d.Offset.Value })
	blankT, blankV := calibration(layers.Blank, func(b domain.BlankPoint) (float64, float64) { return b.Time.Value, b.Mass.Value })
	if len(layers.Drift) > 1 {
		slope := (driftV[len(driftV)-1] - driftV[0]) / (driftT[len(driftT)-1] - driftT[0])
		if math.Abs(slope) > 1e-6 {
			an.Findings = append(an.Findings, domain.Finding{
				Code: "balance_baseline_drift", Severity: "info",
				Message: fmt.Sprintf("balance baseline drift %.5f mg/min corrected pointwise", slope*60),
			})
		}
	}
	segOf := make([]int, len(kept))
	pi := 0
	for i := range kept {
		for pi < len(an.Segments) && i > an.Segments[pi].PointTo {
			pi++
		}
		segOf[i] = pi
	}
	for i, s := range kept {
		t := s.Time.Value
		drift, dOk := interpTable(driftT, driftV, t)
		blank, bOk := interpTable(blankT, blankV, t)
		if !dOk && len(driftT) > 0 {
			an.Findings = append(an.Findings, domain.Finding{
				Code: "drift_outside_calibration", Severity: "warn",
				Message: fmt.Sprintf("t=%.1fs outside drift calibration range; no drift correction applied", t), StartS: t,
			})
		}
		if p.BlankSubtract && !bOk && len(blankT) > 0 {
			an.Findings = append(an.Findings, domain.Finding{
				Code: "blank_outside_range", Severity: "warn",
				Message: fmt.Sprintf("t=%.1fs outside blank curve range; blank term set 0", t), StartS: t,
			})
		}
		if !p.BlankSubtract {
			blank = 0
		}
		corr := s.Mass.Value - drift - blank
		an.Points = append(an.Points, AnalyzedPoint{
			ID: hnum("pt", an.AnalysisID, s.Index), RawIndex: s.Index, Segment: segOf[i],
			TimeS: t, TempC: s.Temp.Value, RawMassMG: s.Mass.Value,
			DriftMG: drift, BlankMG: blank, CorrMassMG: corr,
		})
	}
}

func calibration[T any](rows []T, f func(T) (float64, float64)) ([]float64, []float64) {
	type tv struct{ t, v float64 }
	var rows2 []tv
	for _, r := range rows {
		t, v := f(r)
		rows2 = append(rows2, tv{t, v})
	}
	sort.SliceStable(rows2, func(i, j int) bool { return rows2[i].t < rows2[j].t })
	ts, vs := make([]float64, len(rows2)), make([]float64, len(rows2))
	for i, r := range rows2 {
		ts[i], vs[i] = r.t, r.v
	}
	return ts, vs
}

// computeDTG uses central difference restricted to inside one segment; gap
// boundary points get one-sided derivatives rather than values bridged across.
func computeDTG(an *Analysis) {
	for _, seg := range an.Segments {
		for i := seg.PointFrom; i <= seg.PointTo; i++ {
			var dtg float64
			switch {
			case seg.PointFrom == seg.PointTo:
				dtg = 0
			case i == seg.PointFrom:
				dtg = deriv(an.Points[i], an.Points[i+1])
			case i == seg.PointTo:
				dtg = deriv(an.Points[i-1], an.Points[i])
			default:
				a, b := an.Points[i-1], an.Points[i+1]
				dt := b.TimeS - a.TimeS
				if dt > 0 {
					dtg = (b.CorrMassMG - a.CorrMassMG) / dt * 60
				}
			}
			an.Points[i].DTGMGPM = dtg
		}
	}
}

func deriv(a, b AnalyzedPoint) float64 {
	dt := b.TimeS - a.TimeS
	if dt == 0 {
		return 0
	}
	return (b.CorrMassMG - a.CorrMassMG) / dt * 60
}

func flagNoise(an *Analysis, p domain.AnalysisParams) {
	for _, seg := range an.Segments {
		for i := seg.PointFrom + 1; i <= seg.PointTo; i++ {
			dm := an.Points[i].CorrMassMG - an.Points[i-1].CorrMassMG
			if dm > p.NoiseThresholdMG {
				an.Points[i].Uptick = true
				an.Findings = append(an.Findings, domain.Finding{
					Code: "non_monotonic_uptick", Severity: "warn",
					Message: fmt.Sprintf("non-monotonic mass uptick %.4f mg at t=%.1fs kept, not removed", dm, an.Points[i].TimeS),
					StartS: an.Points[i].TimeS,
				})
			}
		}
	}
}
