package derive

import (
	"math"
	"sort"

	"tga-review/internal/domain"
)

func fptr(v float64) *float64 { return &v }

// linInterp evaluates the piecewise linear anchor series (time, value) at t.
// Outside the anchor range it returns nil plus ok=false; callers must emit a
// diagnostic rather than extrapolating silently.
func linInterp(times, vals []float64, t float64) (float64, bool) {
	if len(times) == 0 {
		return 0, false
	}
	if t <= times[0] {
		if t == times[0] {
			return vals[0], true
		}
		return vals[0], false
	}
	if t >= times[len(times)-1] {
		if t == times[len(times)-1] {
			return vals[len(vals)-1], true
		}
		return vals[len(vals)-1], false
	}
	i := sort.SearchFloat64s(times, t)
	if i < len(times) && times[i] == t {
		return vals[i], true
	}
	t0, t1 := times[i-1], times[i]
	v0, v1 := vals[i-1], vals[i]
	return v0 + (v1-v0)*(t-t0)/(t1-t0), true
}

func validSeries(samples []domain.SamplePoint) (times, mass []float64, idx []int) {
	for i, s := range samples {
		if s.Time.Canon != nil && s.Mass.Canon != nil {
			times = append(times, *s.Time.Canon)
			mass = append(mass, *s.Mass.Canon)
			idx = append(idx, i)
		}
	}
	return
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }
func round4(v float64) float64 { return math.Round(v*10000) / 10000 }

func minF(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
func maxF(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func absF(v float64) float64 { return math.Abs(v) }
