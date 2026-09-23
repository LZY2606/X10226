package calc

import (
	"fmt"
	"sort"

	"gotga/internal/domain"
)

// buildTemperature derives the furnace-temperature axis. When the furnace
// program exists it is authoritative; otherwise samples must carry temperature
// (kept raw) — here we approximate from the first sample time and ramp.
func buildTemperature(pts []domain.SamplePoint, in *Inputs) []float64 {
	out := make([]float64, len(pts))
	if len(in.Furnace) > 0 {
		for i, p := range pts {
			out[i] = furnaceTemp(*p.T, in.Furnace)
		}
		return out
	}
	// No furnace program: do not invent one. Mark the axis unknown (0) and
	// let diagnostics explain it.
	return out
}

func furnaceTemp(t float64, steps []FurnaceStep) float64 {
	temp := 25.0
	for _, s := range steps {
		if t < s.StartT {
			break
		}
		if t <= s.EndT {
			if s.RateC != 0 {
				temp += s.RateC * (t - s.StartT) / 60
				return temp
			}
			return s.TargetC
		}
		if s.RateC != 0 {
			temp += s.RateC * (s.EndT - s.StartT) / 60
		} else {
			temp = s.TargetC
		}
	}
	return temp
}

// applyDrift removes balance baseline drift. Linear coefficients come from the
// drift layer; blank mode subtracts the blank curve at matching times,
// extrapolating its own slope never (out-of-range => flag, no correction).
func applyDrift(pts []domain.SamplePoint, in *Inputs, p domain.Params) ([]float64, string) {
	m := make([]float64, len(pts))
	for i, sp := range pts {
		m[i] = *sp.M
	}
	mode := p.DriftMode
	if in.Drift != nil && in.Drift.Mode != "" {
		mode = in.Drift.Mode
	}
	switch mode {
	case "none":
		return m, "drift correction disabled"
	case "blank":
		if len(in.Blank) < 2 {
			return m, "blank mode requested but blank curve unusable; no correction applied"
		}
		blanks := append([]BlankPoint(nil), in.Blank...)
		sort.Slice(blanks, func(i, j int) bool { return blanks[i].T < blanks[j].T })
		note := fmt.Sprintf("blank subtraction over %s range", fmt.Sprintf("%.0f-%.0fs", blanks[0].T, blanks[len(blanks)-1].T))
		for i, sp := range pts {
			t := *sp.T
			if t < blanks[0].T || t > blanks[len(blanks)-1].T {
				continue // no extrapolation
			}
			bv := interpBlank(t, blanks)
			m[i] = *sp.M - (bv - blanks[0].M)
		}
		return m, note
	default:
		if in.Drift == nil {
			return m, "linear drift mode but no drift layer; baseline estimated as zero"
		}
		d := in.Drift
		tRef := *pts[0].T
		for i, sp := range pts {
			m[i] = *sp.M - (d.Slope*(*sp.T-tRef)/60 + d.Offset)
		}
		return m, fmt.Sprintf("linear drift corrected: slope %.4f mg/min offset %.4f mg", d.Slope, d.Offset)
	}
}

func interpBlank(t float64, b []BlankPoint) float64 {
	i := sort.Search(len(b), func(i int) bool { return b[i].T >= t })
	if i == 0 {
		return b[0].M
	}
	if i >= len(b) {
		return b[len(b)-1].M
	}
	w := (t - b[i-1].T) / (b[i].T - b[i-1].T)
	return b[i-1].M + w*(b[i].M-b[i-1].M)
}

// detectNoise flags runs of repeated DTG sign flips inconsistent with a
// monotonic mass loss: non-monotonic noise is annotated, not smoothed away.
func detectNoise(pts []domain.Point, p domain.Params) []domain.NoiseRun {
	if p.NoiseMinRun < 2 {
		return nil
	}
	var runs []domain.NoiseRun
	i := 1
	for i < len(pts)-1 {
		if pts[i-1].D*pts[i].D < 0 {
			start := i - 1
			flips := 0
			j := i
			for j < len(pts)-1 && pts[j-1].D*pts[j].D < 0 {
				flips++
				j++
			}
			if flips >= p.NoiseMinRun {
				runs = append(runs, domain.NoiseRun{
					StartT: pts[start].T, EndT: pts[j].T, SignFlips: flips, Kind: "non_monotonic",
				})
			}
			i = j
			continue
		}
		i++
	}
	return runs
}
