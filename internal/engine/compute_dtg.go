package engine

import "tga/internal/domain"

// buildDTG differentiates the drift-corrected mass. Differentiation never
// crosses a non-bridged gap: points adjacent to a gap are not used to create a
// derivative across it.
func buildDTG(series *domain.SeriesResult, p domain.Params) *domain.DTGResult {
	out := &domain.DTGResult{}
	sm := smoothMass(series.Points, p.SmoothingWindowN)
	for i, pt := range series.Points {
		if pt.MassMg == nil {
			out.Points = append(out.Points, domain.DTGPoint{Index: pt.Index, TimeS: pt.TimeS, InGap: true})
			continue
		}
		if i == 0 || i == len(series.Points)-1 ||
			series.Points[i-1].MassMg == nil || series.Points[i+1].MassMg == nil {
			out.Points = append(out.Points, domain.DTGPoint{Index: pt.Index, TimeS: pt.TimeS, InGap: false})
			continue
		}
		dt := series.Points[i+1].TimeS - series.Points[i-1].TimeS
		var v float64
		if dt > 0 {
			v = (sm[i+1] - sm[i-1]) / dt * 60.0 // mg/min
		}
		out.Points = append(out.Points, domain.DTGPoint{
			Index: pt.Index, TimeS: pt.TimeS, DTGmgPerMin: v})
	}
	out.Peaks = findPeaks(series.Points, out.Points, p)
	if len(out.Peaks) == 0 {
		out.Diagnostics = append(out.Diagnostics, domain.Diagnostic{
			Code: "no_dtg_peak", Severity: "info",
			Message: "no DTG minimum above prominence threshold; only bounded mass-loss segments retained"})
	}
	return out
}

// smoothMass returns a gap-aware symmetric moving average. Nil masses are not
// averaged across.
func smoothMass(pts []domain.SeriesPoint, w int) []float64 {
	out := make([]float64, len(pts))
	for i := range pts {
		if pts[i].MassMg == nil {
			out[i] = 0
			continue
		}
		var sum float64
		n := 0
		for k := i - w; k <= i+w; k++ {
			if k < 0 || k >= len(pts) || pts[k].MassMg == nil {
				continue
			}
			sum += *pts[k].MassMg
			n++
		}
		out[i] = sum / float64(n)
	}
	return out
}

func findPeaks(pts []domain.SeriesPoint, dtg []domain.DTGPoint, p domain.Params) []domain.Peak {
	var peaks []domain.Peak
	for i := 1; i < len(dtg)-1; i++ {
		if dtg[i].InGap || dtg[i-1].InGap || dtg[i+1].InGap {
			continue
		}
		v := dtg[i].DTGmgPerMin
		// DTG minima (strongest negative slope) are the decomposition peaks.
		if v >= dtg[i-1].DTGmgPerMin || v >= dtg[i+1].DTGmgPerMin || v >= 0 {
			continue
		}
		prom := prominence(dtg, i)
		if prom < p.MinProminence {
			continue
		}
		pk := domain.Peak{Index: i, TimeS: dtg[i].TimeS, ValueMgPerMin: v, Prominence: prom}
		if pts[i].TempC != nil {
			pk.TempC = *pts[i].TempC
		}
		peaks = append(peaks, pk)
	}
	return peaks
}

// prominence is the depth of a minimum relative to the higher of the two
// surrounding shoulders up to the nearest higher minimum/zero crossing.
func prominence(dtg []domain.DTGPoint, i int) float64 {
	left, right := 0.0, 0.0
	for k := i - 1; k >= 0; k-- {
		if dtg[k].InGap {
			break
		}
		if dtg[k].DTGmgPerMin > left {
			left = dtg[k].DTGmgPerMin
		}
		if dtg[k].DTGmgPerMin >= 0 {
			break
		}
	}
	for k := i + 1; k < len(dtg); k++ {
		if dtg[k].InGap {
			break
		}
		if dtg[k].DTGmgPerMin > right {
			right = dtg[k].DTGmgPerMin
		}
		if dtg[k].DTGmgPerMin >= 0 {
			break
		}
	}
	shoulder := left
	if right > shoulder {
		shoulder = right
	}
	return shoulder - dtg[i].DTGmgPerMin
}
