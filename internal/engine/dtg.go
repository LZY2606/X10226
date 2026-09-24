package engine

import (
	"tgaline/internal/domain"
)

// buildDTG computes -dm/dt (mg/min, positive for mass loss) within each
// continuous segment. A sampling gap splits segments: derivatives never span
// a gap, and the endpoints adjacent to a gap are marked unknown.
func buildDTG(s *domain.Series, p map[string]float64) []float64 {
	n := len(s.TimeS)
	raw := make([]float64, n) // positive = loss rate
	known := make([]bool, n)

	gapSet := map[int]bool{}
	for _, g := range s.GapAfter {
		gapSet[g] = true
	}

	// raw central differences, blocked at gap boundaries.
	for i := 1; i < n-1; i++ {
		if gapSet[i-1] || gapSet[i] || !s.Mask[i-1] || !s.Mask[i] || !s.Mask[i+1] {
			continue
		}
		dt := s.TimeS[i+1] - s.TimeS[i-1]
		if dt <= 0 {
			continue
		}
		raw[i] = -(s.MassMG[i+1] - s.MassMG[i-1]) / dt * 60.0
		known[i] = true
	}

	// boxcar smoothing inside the window, only across consecutive known points.
	winS := p["dtg_window_s"]
	out := make([]float64, n)
	ok := make([]bool, n)
	for i := 0; i < n; i++ {
		if !known[i] {
			continue
		}
		sum, w := 0.0, 0.0
		for j := i; j >= 0; j-- {
			if !known[j] {
				break
			}
			if s.TimeS[i]-s.TimeS[j] > winS/2 {
				break
			}
			sum += raw[j]
			w++
		}
		for j := i + 1; j < n; j++ {
			if !known[j] {
				break
			}
			if s.TimeS[j]-s.TimeS[i] > winS/2 {
				break
			}
			sum += raw[j]
			w++
		}
		if w > 0 {
			out[i] = sum / w
			ok[i] = true
		}
	}
	_ = ok
	return out
}
