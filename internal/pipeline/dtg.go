package pipeline

import "tga-review/internal/domain"

// laggedSampleTemp evaluates the furnace program and applies a first-order
// thermal lag to estimate the *sample* temperature (deg C).
func laggedSampleTemp(pts []domain.SamplePoint, furnace []domain.FurnaceSegment, tauS float64) map[int]float64 {
	out := map[int]float64{}
	if len(pts) == 0 {
		return out
	}
	furnaceT := func(t float64) float64 {
		if len(furnace) == 0 {
			return 0
		}
		for _, seg := range furnace {
			if t >= seg.StartT && t <= seg.EndT {
				if seg.EndT == seg.StartT {
					return seg.T1
				}
				f := (t - seg.StartT) / (seg.EndT - seg.StartT)
				return seg.T0 + f*(seg.T1-seg.T0)
			}
		}
		last := furnace[len(furnace)-1]
		if t > last.EndT {
			return last.T1
		}
		return furnace[0].T0
	}
	tau := tauS
	if tau <= 0 {
		tau = 1
	}
	ts := pts[0].T
	tsamp := furnaceT(ts)
	for i, p := range pts {
		if p.GapBefore {
			// Do not carry dynamics across a gap; reinitialize.
			tsamp = furnaceT(p.T)
		}
		dt := p.T - ts
		if dt > 0 {
			target := furnaceT(p.T)
			// exact first-order step over dt
			k := 1 - expFast(-dt/tau)
			tsamp = tsamp + k*(target-tsamp)
		}
		out[i] = tsamp
		ts = p.T
	}
	return out
}

func expFast(x float64) float64 {
	// math-free approximation would be fragile; use stdlib via wrapper.
	return expmath(x)
}

// DTGPoint is derivative of mass vs time in mg/min; NaN across gaps.
type DTGPoint struct {
	T    float64
	Rate float64 // mg/min (negative for mass loss)
}

// computeDTG uses centered differences when neighbors exist on both sides
// without crossing a sampling gap; otherwise the rate is left NaN.
func computeDTG(pts []domain.SamplePoint) []DTGPoint {
	out := make([]DTGPoint, len(pts))
	for i := range pts {
		out[i].T = pts[i].T
		out[i].Rate = nanValue
	}
	for i := 1; i < len(pts)-1; i++ {
		if pts[i].GapBefore || pts[i].GapAfter ||
			pts[i-1].GapAfter || pts[i+1].GapBefore {
			continue
		}
		if pts[i].Status != domain.RowOK {
			continue
		}
		dt := pts[i+1].T - pts[i-1].T
		if dt <= 0 {
			continue
		}
		rate := (pts[i+1].Mass - pts[i-1].Mass) / dt * 60.0
		out[i].Rate = rate
	}
	return out
}
