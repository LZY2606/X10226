package engine

import "tgaline/internal/domain"

// furnaceTrace samples the programmed furnace temperature at sample times by
// piecewise-linear interpolation within the program support. Sample points
// outside the programmed range remain unknown (no extrapolation).
func furnaceTrace(in layerInput) (*domain.Series, int) {
	n := len(in.sample)
	s := &domain.Series{TimeS: make([]float64, n), TK: make([]float64, n),
		Mask: make([]bool, n)}
	unknown := 0
	for i, pt := range in.sample {
		s.TimeS[i] = pt.t
		if v, ok := interp(in.furnace, pt.t); ok {
			s.TK[i] = v
			s.Mask[i] = true
		} else {
			unknown++
		}
	}
	return s, unknown
}
