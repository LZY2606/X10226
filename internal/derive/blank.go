package derive

import "tga-archive/internal/domain"

type bp struct{ t, m float64 }

func blankCurve(sp *domain.SourcePayload) []bp {
	if sp == nil {
		return nil
	}
	var out []bp
	for _, b := range sp.Blank {
		if b.Time.Valid && b.Mass.Valid {
			out = append(out, bp{b.Time.Value, b.Mass.Value})
		}
	}
	return out
}

// blankAt linearly interpolates the blank curve. Extrapolation is refused
// unless explicitly enabled by parameters (off by default).
func blankAt(points []bp, t float64, extrapolate bool) (float64, bool) {
	if len(points) == 0 {
		return 0, false
	}
	if t < points[0].t || t > points[len(points)-1].t {
		if !extrapolate {
			return 0, false
		}
	}
	for i := 1; i < len(points); i++ {
		if t <= points[i].t {
			a, b := points[i-1], points[i]
			if b.t == a.t {
				return a.m, true
			}
			frac := (t - a.t) / (b.t - a.t)
			return a.m + frac*(b.m-a.m), true
		}
	}
	// beyond the end: clamp or refuse
	if extrapolate {
		return points[len(points)-1].m, true
	}
	return 0, false
}

func buildBlank(sp *domain.SourcePayload, _ bool, _ *Report) *domain.SourcePayload {
	return sp
}
