package calc

import (
	"sort"

	"tga-review/internal/domain"
)

func initialMassMg(s *domain.Source) float64 {
	im := initialMassX(s)
	if !im.Mass.Valid {
		return 0
	}
	return im.Mass.Value
}

// ComputeDTG derives mass-loss rates. No interpolation across gaps: rates are
// computed inside each corrected region only. Smoothing is a centered moving
// average over neighbors in the same region; gapped windows stay invalid.
func ComputeDTG(in Inputs, corr Corrected, trace FurnaceTrace) DTG {
	m0 := initialMassMg(in.InitMass)
	effAt := traceInterp(trace)
	w := in.Params.DTGSmoothWindow
	if w < 1 {
		w = 1
	}
	pts := make([]DTGPoint, len(corr.Points))
	for i, cp := range corr.Points {
		p := DTGPoint{TS: cp.TS, Region: cp.Region}
		_, eff := effAt(cp.TS)
		p.EffTempC = eff
		if !cp.Usable || i == 0 || i == len(corr.Points)-1 {
			pts[i] = p
			continue
		}
		prev := corr.Points[i-1]
		next := corr.Points[i+1]
		if !prev.Usable || !next.Usable || prev.Region != cp.Region || next.Region != cp.Region {
			pts[i] = p
			continue
		}
		dt := next.TS - prev.TS
		if dt <= 0 {
			pts[i] = p
			continue
		}
		p.DmDtMgS = (next.CorrMg - prev.CorrMg) / dt
		_, e1 := effAt(prev.TS)
		_, e2 := effAt(next.TS)
		dT := e2 - e1
		if dT > 1e-6 {
			p.DmDtMgK = p.DmDtMgS * (dt / dT)
		}
		p.Valid = true
		pts[i] = p
	}
	// smoothing per region
	byRegion := map[int][]int{}
	for i := range pts {
		if pts[i].Valid {
			byRegion[pts[i].Region] = append(byRegion[pts[i].Region], i)
		}
	}
	for _, idxs := range byRegion {
		smoothed := make([]float64, len(idxs))
		for k := range idxs {
			sum, n := 0.0, 0
			lo, hi := k-w, k+w
			if lo < 0 {
				lo = 0
			}
			if hi > len(idxs)-1 {
				hi = len(idxs) - 1
			}
			for j := lo; j <= hi; j++ {
				sum += pts[idxs[j]].DmDtMgS
				n++
			}
			smoothed[k] = sum / float64(n)
		}
		for k := range idxs {
			if m0 > 0 {
				pts[idxs[k]].SmoothPctMin = smoothed[k] / m0 * 100 * 60
			}
		}
	}
	return DTG{Points: pts, M0Mg: m0, ThresholdPct: in.Params.DTGThreshold}
}

func traceInterp(tr FurnaceTrace) func(float64) (float64, float64) {
	pts := tr.Points
	return func(t float64) (float64, float64) {
		if len(pts) == 0 {
			return 0, 0
		}
		i := sort.Search(len(pts), func(i int) bool { return pts[i].TS >= t })
		if i == 0 {
			return pts[0].SetpointC, pts[0].EffectiveC
		}
		if i >= len(pts) {
			p := pts[len(pts)-1]
			return p.SetpointC, p.EffectiveC
		}
		if pts[i].TS == t {
			return pts[i].SetpointC, pts[i].EffectiveC
		}
		a, b := pts[i-1], pts[i]
		f := (t - a.TS) / (b.TS - a.TS)
		return a.SetpointC + f*(b.SetpointC-a.SetpointC), a.EffectiveC + f*(b.EffectiveC-a.EffectiveC)
	}
}
