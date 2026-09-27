package compute

import (
	"fmt"
	"math"

	"tga/internal/domain"
)

// furnaceTempSeries fills missing furnace temperatures from the programmed
// ramp segments (program time basis). This is programmed temperature, clearly
// distinguished from the lagged sample temperature.
func furnaceTempSeries(in Inputs, pts []pt) {
	if len(in.Furnace) == 0 {
		return
	}
	for i := range pts {
		if pts[i].FurnaceT != 0 || !pts[i].Present {
			continue
		}
		t := pts[i].Time
		for _, seg := range in.Furnace {
			if seg.FromTime.Unit == "" || seg.ToTime.Unit == "" {
				continue
			}
			t0, t1 := seg.FromTime.Value, seg.ToTime.Value
			if t >= t0 && t <= t1 && t1 > t0 {
				if seg.Kind == "hold" {
					pts[i].FurnaceT = seg.FromTemp.Value
				} else {
					frac := (t - t0) / (t1 - t0)
					pts[i].FurnaceT = seg.FromTemp.Value + frac*(seg.ToTemp.Value-seg.FromTemp.Value)
				}
			}
		}
	}
}

// correctDrift subtracts the balance baseline. Mass is converted to mg; drift
// rate in ug/min is applied relative to its anchor time. Blank mode subtracts
// an interpolated empty-pan baseline without extrapolating beyond its range.
func correctDrift(in Inputs, pts []pt, res *domain.AnalysisResult) {
	mode := in.Params.DriftModel
	if mode == "" {
		mode = "none"
	}
	switch mode {
	case "linear":
		if in.Drift == nil {
			res.Findings = append(res.Findings, domain.Finding{Code: "DRIFT_MODEL_UNAVAILABLE",
				Severity: "warning",
				Message:  "params request linear drift correction but no drift_correction layer is present; raw mass retained"})
			return
		}
		d := in.Drift
		if d.Rate.Unit == "" || d.AnchorAt.Unit == "" {
			res.Findings = append(res.Findings, domain.Finding{Code: "DRIFT_UNCONVERTIBLE",
				Severity: "error",
				Message:  "drift rate or anchor unconvertible; raw mass retained, correction not applied"})
			return
		}
		rateMgMin := d.Rate.Value / 1000.0 // ug/min -> mg/min
		for i := range pts {
			if !pts[i].Present {
				continue
			}
			corr := rateMgMin * (pts[i].Time - d.AnchorAt.Value) / 60.0
			pts[i].MassCorr = pts[i].MassCorr - corr
		}
		res.Findings = append(res.Findings, domain.Finding{Code: "DRIFT_CORRECTED_LINEAR",
			Severity: "info",
			Message: fmt.Sprintf("linear balance drift %.4f ug/min removed relative to anchor %.0fs",
				d.Rate.Value, d.AnchorAt.Value),
			Data: map[string]any{"rate_ug_min": d.Rate.Value, "anchor_s": d.AnchorAt.Value}})
	case "blank":
		if len(in.Blank) == 0 {
			res.Findings = append(res.Findings, domain.Finding{Code: "BLANK_UNAVAILABLE",
				Severity: "warning", Message: "blank drift mode requested without blank_curve; raw mass retained"})
			return
		}
		bt, bm := make([]float64, 0, len(in.Blank)), make([]float64, 0, len(in.Blank))
		for _, b := range in.Blank {
			if b.Time.Unit != "" && b.Mass.Unit != "" {
				bt = append(bt, b.Time.Value)
				bm = append(bm, b.Mass.Value)
			}
		}
		applied := 0
		for i := range pts {
			if !pts[i].Present {
				continue
			}
			v, ok := interp(bt, bm, pts[i].Time)
			if !ok {
				continue // never extrapolate beyond blank range
			}
			pts[i].MassCorr -= v
			applied++
		}
		res.Findings = append(res.Findings, domain.Finding{Code: "DRIFT_CORRECTED_BLANK",
			Severity: "info", Data: map[string]any{"points_corrected": applied},
			Message: fmt.Sprintf("blank curve subtraction applied to %d samples inside blank range", applied)})
	default:
		res.Findings = append(res.Findings, domain.Finding{Code: "DRIFT_NOT_CORRECTED",
			Severity: "info", Message: "drift_model=none; raw mass used as corrected mass"})
	}
}

func normalize(in Inputs, pts []pt) {
	m0 := in.Init.Mass.Value
	for i := range pts {
		if !pts[i].Present || m0 == 0 {
			continue
		}
		pts[i].TG = pts[i].MassCorr / m0 * 100
	}
}

// sampleTemp applies the constant temperature lag: the sample temperature
// seen by the reaction is the furnace reading shifted on the time axis.
func sampleTemp(in Inputs, pts []pt) {
	lag := in.Params.TempLagSec
	if lag <= 0 {
		for i := range pts {
			pts[i].SampleT = pts[i].FurnaceT
		}
		return
	}
	ts := make([]float64, 0, len(pts))
	fs := make([]float64, 0, len(pts))
	for _, p := range pts {
		if p.Present {
			ts = append(ts, p.Time)
			fs = append(fs, p.FurnaceT)
		}
	}
	for i := range pts {
		v, ok := interp(ts, fs, pts[i].Time-lag)
		if ok {
			pts[i].SampleT = v
		} else {
			pts[i].SampleT = pts[i].FurnaceT
		}
	}
}

// dtg computes d(TG%)/dt in %/min via centred regression over WindowSec,
// independently on each contiguous segment; holes never contribute.
func dtg(in Inputs, pts []pt, res *domain.AnalysisResult) {
	w := in.Params.WindowSec
	for i := range pts {
		if !pts[i].Present {
			continue
		}
		var sx, sy, sxx, sxy, n float64
		ti := pts[i].Time
		for j := range pts {
			if !pts[j].Present || math.Abs(pts[j].Time-ti) > w {
				continue
			}
			if crossesGap(pts, i, j) {
				continue
			}
			x := (pts[j].Time - ti) / 60
			y := pts[j].TG
			n++
			sx += x
			sy += y
			sxx += x * x
			sxy += x * y
		}
		if n >= 3 {
			den := n*sxx - sx*sx
			if math.Abs(den) > 1e-12 {
				pts[i].DTG = (n*sxy - sx*sy) / den
			}
		}
	}
}

func crossesGap(pts []pt, a, b int) bool {
	lo, hi := a, b
	if lo > hi {
		lo, hi = hi, lo
	}
	for k := lo; k <= hi; k++ {
		if !pts[k].Present {
			return true
		}
	}
	return false
}

// markNoise flags retained non-monotonic samples: positive mass jumps between
// adjacent present points are noise, kept visible rather than smoothed away.
func markNoise(pts []pt, res *domain.AnalysisResult) {
	for i := 1; i < len(pts); i++ {
		if !pts[i].Present || !pts[i-1].Present {
			continue
		}
		jump := pts[i].MassCorr - pts[i-1].MassCorr
		if jump > noiseJumpMg {
			pts[i].NonMono = true
			res.Noise = append(res.Noise, domain.NoiseFlag{
				Row: pts[i].Row, T: pts[i].Time, M: pts[i].MassCorr, D: jump})
		}
	}
	if len(res.Noise) > 0 {
		res.Findings = append(res.Findings, domain.Finding{Code: "NONMONOTONIC_NOISE",
			Severity: "warning", T: res.Noise[0].T,
			Message: fmt.Sprintf("%d positive mass jump(s) retained as non-monotonic noise; no smoothing removal",
				len(res.Noise)),
			Data: map[string]any{"count": len(res.Noise), "threshold_mg": noiseJumpMg}})
	}
}

func interp(xs, ys []float64, x float64) (float64, bool) {
	if len(xs) == 0 || x < xs[0] || x > xs[len(xs)-1] {
		return 0, false
	}
	lo, hi := 0, len(xs)-1
	for hi-lo > 1 {
		mid := (lo + hi) / 2
		if xs[mid] <= x {
			lo = mid
		} else {
			hi = mid
		}
	}
	if xs[hi] == xs[lo] {
		return ys[lo], true
	}
	frac := (x - xs[lo]) / (xs[hi] - xs[lo])
	return ys[lo] + frac*(ys[hi]-ys[lo]), true
}

func contiguousSegments(pts []pt) [][2]int {
	var segs [][2]int
	start := -1
	for i := range pts {
		if pts[i].Present && start < 0 {
			start = i
		}
		if !pts[i].Present && start >= 0 {
			segs = append(segs, [2]int{start, i - 1})
			start = -1
		}
	}
	if start >= 0 {
		segs = append(segs, [2]int{start, len(pts) - 1})
	}
	return segs
}
