package calc

import (
	"math"
	"sort"

	"tga-review/internal/domain"
)

// payloadSamples extracts typed sample points from a source payload.
func payloadSamples(s *domain.Source) []domain.SamplePoint {
	if s == nil {
		return nil
	}
	raw, ok := s.Payload["points"].([]domain.SamplePoint)
	if !ok {
		return nil
	}
	out := make([]domain.SamplePoint, len(raw))
	copy(out, raw)
	return out
}

func payloadBlank(s *domain.Source) []domain.BlankPoint {
	if s == nil {
		return nil
	}
	raw, ok := s.Payload["points"].([]domain.BlankPoint)
	if !ok {
		return nil
	}
	out := make([]domain.BlankPoint, len(raw))
	copy(out, raw)
	return out
}

func driftCoeffs(s *domain.Source) (float64, float64) {
	if s == nil {
		return 0, 0
	}
	var ic, sl float64
	asType(s.Payload["intercept_mg"], &ic)
	asType(s.Payload["slope_mg_per_s"], &sl)
	return ic, sl
}

func interpBlank(points []domain.BlankPoint, t float64) float64 {
	if len(points) == 0 {
		return 0
	}
	if t <= points[0].T {
		if points[0].Mass.Valid {
			return points[0].Mass.Value
		}
		return 0
	}
	if t >= points[len(points)-1].T {
		p := points[len(points)-1]
		if p.Mass.Valid {
			return p.Mass.Value
		}
		return 0
	}
	i := sort.Search(len(points)-1, func(i int) bool { return points[i+1].T >= t })
	a, b := points[i], points[i+1]
	if !a.Mass.Valid || !b.Mass.Valid {
		return 0
	}
	if b.T == a.T {
		return a.Mass.Value
	}
	f := (t - a.T) / (b.T - a.T)
	return a.Mass.Value + f*(b.Mass.Value-a.Mass.Value)
}

// ComputeCorrected applies blank subtraction and linear balance-drift
// correction, detects acquisition gaps and quarantines non-monotonic spikes.
// It NEVER interpolates across gaps: unusable points stay on the series and
// usable points receive region ids that break at gaps/spikes.
func ComputeCorrected(in Inputs) Corrected {
	samples := payloadSamplesX(in.Samples)
	blank := payloadBlankX(in.Blank)
	intercept, slope := driftCoeffs(in.Drift)

	// median nominal spacing of convertible, time-positive input
	dts := make([]float64, 0, len(samples))
	for i := 1; i < len(samples); i++ {
		if samples[i].T > samples[i-1].T {
			dts = append(dts, samples[i].T-samples[i-1].T)
		}
	}
	medDT := median(dts)
	if medDT <= 0 {
		medDT = 1
	}

	pts := make([]CorrectedPoint, len(samples))
	// first pass: correction and usability
	for i, sp := range samples {
		cp := CorrectedPoint{TS: sp.T, Row: sp.Row, RawMg: sp.Mass.Value}
		cp.BlankMg = interpBlank(blank, sp.T)
		cp.DriftMg = intercept + slope*sp.T
		if sp.Mass.Valid {
			cp.CorrMg = sp.Mass.Value - cp.BlankMg - cp.DriftMg
			cp.Usable = true
		} else {
			cp.Quarantine = "unconvertible"
			cp.CorrMg = 0
		}
		pts[i] = cp
	}

	// gap flags (on the point following a large spacing or unusable point)
	var gaps []Gap
	for i := 1; i < len(pts); i++ {
		prevOK := pts[i-1].Usable
		dt := pts[i].TS - pts[i-1].TS
		if prevOK && pts[i].Usable && dt > 2.2*medDT {
			pts[i].GapBefore = true
			gaps = append(gaps, Gap{FromS: pts[i-1].TS, ToS: pts[i].TS, Rows: [2]int{pts[i-1].Row, pts[i].Row}})
		}
	}

	// non-monotonic quarantine on usable points: a rise much larger than the
	// acquisition noise floor, immediately recovered, is an instrument spike.
	diffs := make([]float64, 0, len(pts))
	for i := 1; i < len(pts); i++ {
		if pts[i].Usable && pts[i-1].Usable {
			diffs = append(diffs, pts[i].CorrMg-pts[i-1].CorrMg)
		}
	}
	mad := medianAbs(diffs)
	riseLimit := math.Max(0.02, 4.0*mad)
	var spikes []float64
	for i := 1; i < len(pts)-1; i++ {
		if !pts[i].Usable {
			continue
		}
		if !pts[i-1].Usable || !pts[i+1].Usable {
			continue
		}
		up := pts[i].CorrMg - pts[i-1].CorrMg
		down := pts[i].CorrMg - pts[i+1].CorrMg
		if up > riseLimit && down > riseLimit {
			pts[i].Usable = false
			pts[i].Quarantine = "nonmonotonic"
			spikes = append(spikes, pts[i].TS)
		}
	}
	for _, t := range spikes {
		for i := range pts {
			if pts[i].TS == t {
				if i+1 < len(pts) && pts[i+1].Usable {
					pts[i+1].GapBefore = true
					gaps = append(gaps, Gap{FromS: pts[i].TS, ToS: pts[i+1].TS, Rows: [2]int{pts[i].Row, pts[i+1].Row}})
				}
			}
		}
	}

	region := -1
	prevUsable := false
	for i := range pts {
		if pts[i].Usable {
			if !prevUsable || pts[i].GapBefore {
				region++
			}
			pts[i].Region = region
		}
		prevUsable = pts[i].Usable
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].FromS < gaps[j].FromS })
	return Corrected{Points: pts, Gaps: mergeGaps(gaps), Nonmonotonic: spikes, InterceptMg: intercept, SlopeMgS: slope}
}

func mergeGaps(gaps []Gap) []Gap {
	if len(gaps) == 0 {
		return gaps
	}
	sort.Slice(gaps, func(i, j int) bool { return gaps[i].FromS < gaps[j].FromS })
	out := []Gap{gaps[0]}
	for _, g := range gaps[1:] {
		last := &out[len(out)-1]
		if g.FromS <= last.ToS {
			if g.ToS > last.ToS {
				last.ToS = g.ToS
			}
		} else {
			out = append(out, g)
		}
	}
	return out
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64(nil), xs...)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

func medianAbs(xs []float64) float64 {
	abs := make([]float64, len(xs))
	for i, x := range xs {
		abs[i] = math.Abs(x)
	}
	return median(abs)
}
