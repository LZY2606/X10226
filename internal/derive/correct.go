package derive

import (
	"sort"

	"tga-review/internal/domain"
)

// correctMass applies monotone clipping, then subtracts balance baseline
// drift (interpolated within segments, never across sample gaps) and the
// blank curve. The original upticks remain evidence in the sample layer.
func correctMass(layer *domain.SampleLayer, drift []domain.DriftPoint, blank []domain.Point) []domain.Point {
	mono, _ := clampMonotone(layer.Mass)
	var out []domain.Point
	for _, p := range mono {
		if inGap(layer.Gaps, p.T) {
			continue
		}
		d, ok := interpDrift(drift, p.T)
		if !ok {
			d = 0
		}
		b := 0.0
		if len(blank) > 0 {
			bv, ok := interp(blank, p.T)
			if ok {
				b = bv
			}
		}
		out = append(out, domain.Point{
			T: p.T, V: p.V - d - b, UnitX: "s", UnitY: "mg",
			Reference: p.Reference + ";corrected"})
	}
	return out
}

// interpDrift interpolates drift anchors piecewise. It refuses to interpolate
// outside the anchor span; the caller then treats drift as zero with a
// diagnostic rather than inventing a correction.
func interpDrift(drift []domain.DriftPoint, t float64) (float64, bool) {
	if len(drift) == 0 {
		return 0, false
	}
	sort.Slice(drift, func(i, j int) bool { return drift[i].T < drift[j].T })
	if t < drift[0].T || t > drift[len(drift)-1].T {
		return 0, false
	}
	pts := make([]domain.Point, len(drift))
	for i, d := range drift {
		pts[i] = domain.Point{T: d.T, V: d.Offset}
	}
	v, ok := interp(pts, t)
	return v, ok
}

// computeDTG uses a 3-point central derivative on the corrected series.
// Pairs separated by a sampling gap are not differentiated across.
func computeDTG(corrected []domain.Point, gaps []domain.Gap) []domain.Point {
	var out []domain.Point
	for i := 0; i < len(corrected); i++ {
		var dv, dt float64
		if i > 0 && i < len(corrected)-1 {
			a, c := corrected[i-1], corrected[i+1]
			if crossesAnyGap(gaps, a.T, c.T) || c.T == a.T {
				if i > 0 && !crossesAnyGap(gaps, corrected[i-1].T, corrected[i].T) {
					dv = corrected[i].V - corrected[i-1].V
					dt = corrected[i].T - corrected[i-1].T
				} else {
					continue
				}
			} else {
				dv = c.V - a.V
				dt = c.T - a.T
			}
		} else if i == 0 && len(corrected) > 1 {
			dv = corrected[1].V - corrected[0].V
			dt = corrected[1].T - corrected[0].T
		} else if i == len(corrected)-1 && len(corrected) > 1 {
			dv = corrected[i].V - corrected[i-1].V
			dt = corrected[i].T - corrected[i-1].T
		}
		if dt == 0 {
			continue
		}
		out = append(out, domain.Point{T: corrected[i].T, V: dv / dt, UnitX: "s", UnitY: "mg/s"})
	}
	return out
}

func crossesAnyGap(gaps []domain.Gap, a, b float64) bool {
	if a > b {
		a, b = b, a
	}
	for _, g := range gaps {
		if a < g.To && b > g.From {
			return true
		}
	}
	return false
}
