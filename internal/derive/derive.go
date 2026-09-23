// Package derive implements the TGA derivation pipeline: normalization with
// unit/coordinate bases, balance drift and blank correction, DTG, reaction
// windows (gas delay + temperature lag), overlapping stage decomposition and
// the residue calculation chain.
//
// Domain rules encoded here:
//   - valve switch time is never treated as the sample reaction time;
//   - stages may overlap and their mass losses are never forced to sum to 100%;
//   - data gaps are never interpolated across and stages crossing a gap keep
//     every legal boundary candidate instead of guessing one.
package derive

import (
	"math"
	"strings"

	"tga-review/internal/domain"
)

// AlgorithmVersion is stamped on every derived node.
const AlgorithmVersion = "tga-derive/1.0.0"

// massScale converts source mass units to canonical milligrams.
var massScale = map[string]float64{"mg": 1, "g": 1000, "kg": 1e6}

// timeScale converts source time units to canonical seconds.
var timeScale = map[string]float64{"s": 1, "min": 60, "h": 3600}

func findCol(src *domain.Source, prefixes ...string) string {
	for _, p := range prefixes {
		for _, h := range src.Header {
			if eqCol(h, p) {
				return h
			}
		}
	}
	return ""
}

// dimOf strips the unit suffix so mass_mg matches the mass dimension.
func dimOf(h string) string {
	if i := strings.Index(h, "_"); i >= 0 {
		return h[:i]
	}
	return h
}

func num(raw string) (float64, bool) {
	var v float64
	neg := false
	i := 0
	if len(raw) > 0 && raw[0] == '-' {
		neg = true
		i = 1
	}
	got := false
	frac := 0.0
	div := 1.0
	expDigits := 0
	for ; i < len(raw); i++ {
		c := raw[i]
		if c == ' ' && got {
			break
		}
		if c == '.' {
			frac = 0
			div = 1
			expDigits = 1
			continue
		}
		if c < '0' || c > '9' {
			if c == ' ' {
				continue
			}
			return 0, false
		}
		got = true
		d := float64(c - '0')
		if expDigits == 0 {
			v = v*10 + d
		} else {
			frac = frac*10 + d
			div *= 10
		}
	}
	if !got {
		return 0, false
	}
	v += frac / div
	if neg {
		v = -v
	}
	return v, true
}

func convert(raw, unit string, scale map[string]float64) (float64, bool) {
	v, ok := num(raw)
	if !ok {
		return 0, false
	}
	f, known := scale[unit]
	if !known {
		return v, false
	}
	return v * f, true
}

func tempConvert(raw, unit string) (float64, bool) {
	v, ok := num(raw)
	if !ok {
		return 0, false
	}
	switch unit {
	case "C", "c", "degC":
		return v, true
	case "K":
		return v - 273.15, true
	default:
		return v, false
	}
}

// clampMonotone removes non-monotonic upticks by clipping at the running
// minimum. The original upticks are preserved as noise evidence.
func clampMonotone(points []domain.Point) ([]domain.Point, []domain.NonMonotone) {
	var mono []domain.Point
	var noises []domain.NonMonotone
	minV := math.Inf(1)
	for _, p := range points {
		if p.V > minV {
			noises = append(noises, domain.NonMonotone{
				T: p.T, Unit: p.UnitX, Observed: p.V,
				MonotoneV: minV, Delta: p.V - minV,
			})
			mono = append(mono, domain.Point{T: p.T, V: minV, UnitX: p.UnitX, UnitY: p.UnitY})
		} else {
			minV = p.V
			mono = append(mono, p)
		}
	}
	return mono, noises
}

// interp is linear interpolation (never extrapolates, never called across a
// gap by callers).
func interp(points []domain.Point, t float64) (float64, bool) {
	if len(points) == 0 {
		return 0, false
	}
	if t <= points[0].T {
		return points[0].V, true
	}
	last := points[len(points)-1]
	if t >= last.T {
		return last.V, true
	}
	for i := 1; i < len(points); i++ {
		if t <= points[i].T {
			a, b := points[i-1], points[i]
			if b.T == a.T {
				return a.V, true
			}
			return a.V + (b.V-a.V)*(t-a.T)/(b.T-a.T), true
		}
	}
	return last.V, true
}

// inGap reports whether time t lies strictly inside a non-interpolatable gap.
func inGap(gaps []domain.Gap, t float64) bool {
	for _, g := range gaps {
		if t > g.From && t < g.To {
			return true
		}
	}
	return false
}
