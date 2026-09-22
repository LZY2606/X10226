package derive

import (
	"math"

	"tga/internal/domain"
)

// computeDTG returns dm/dt in mg/min. Each value needs valid neighbors inside
// one contiguous run; every gap or unknown neighbor yields NaN. A centered
// moving average of windowSize (>=1) smooths only within runs.
func computeDTG(times, mass []float64, valid []bool, gaps []domain.Gap, window int) []float64 {
	n := len(times)
	raw := make([]float64, n)
	for i := range raw {
		raw[i] = math.NaN()
	}
	for i := 1; i < n-1; i++ {
		if !valid[i] || !valid[i-1] || !valid[i+1] {
			continue
		}
		if !withinRun(gaps, i-1, i) || !withinRun(gaps, i, i+1) {
			continue
		}
		dt := (times[i+1] - times[i-1]) / 60.0 // seconds -> minutes
		if dt <= 0 {
			continue
		}
		raw[i] = (mass[i+1] - mass[i-1]) / dt
	}
	if window < 1 {
		window = 1
	}
	out := make([]float64, n)
	for i := range out {
		out[i] = math.NaN()
	}
	half := window / 2
	for i := 0; i < n; i++ {
		var sum float64
		cnt := 0
		for j := i - half; j <= i+half; j++ {
			if j < 0 || j >= n || math.IsNaN(raw[j]) || !withinRun(gaps, i, j) {
				continue
			}
			sum += raw[j]
			cnt++
		}
		if cnt > 0 {
			out[i] = sum / float64(cnt)
		}
	}
	return out
}
