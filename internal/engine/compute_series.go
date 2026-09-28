package engine

import "tga/internal/domain"

// buildSeries normalizes raw points and applies balance drift correction.
// Sampling gaps are never bridged when they exceed GapMaxBridgeN; even bridged
// gaps are recorded, and non-monotonic noise is flagged rather than hidden.
func buildSeries(src *domain.Source, driftSrc, blankSrc *domain.Source, p domain.Params) *domain.SeriesResult {
	res := &domain.SeriesResult{DriftApplied: "none"}
	if src == nil {
		res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
			Code: "missing_samples", Severity: "error",
			Message: "no raw sample source version is available; series cannot be built"})
		return res
	}

	pts := make([]domain.SeriesPoint, 0, len(src.Samples))
	for _, sp := range src.Samples {
		pt := domain.SeriesPoint{Index: sp.Index, Present: sp.Present}
		if t, ok := num(sp.Time); ok {
			pt.TimeS = t
		} else if !sp.Present {
			res.Unconverted = append(res.Unconverted, unconv(sp.Index, "time", sp.Time.Raw, sp.Time.RawUnit, sp.Time.Diagnostic))
		}
		if ft, ok := num(sp.FurnaceTime); ok {
			pt.FurnTimeS = ft
		}
		if sp.Present {
			if m, ok := num(sp.Mass); ok {
				mg := m
				pt.RawMassMg = &mg
			} else {
				res.Unconverted = append(res.Unconverted, unconv(sp.Index, "mass", sp.Mass.Raw, sp.Mass.RawUnit, sp.Mass.Diagnostic))
			}
			if tc, ok := num(sp.Temp); ok {
				pt.TempC = &tc
			} else {
				res.Unconverted = append(res.Unconverted, unconv(sp.Index, "temperature", sp.Temp.Raw, sp.Temp.RawUnit, sp.Temp.Diagnostic))
			}
		}
		pts = append(pts, pt)
	}

	// Record sampling gaps: missing rows or present-but-unconverted mass rows.
	markGaps(pts, res, p)

	// Drift correction on raw mass.
	applyDrift(pts, driftSrc, blankSrc, res)

	// Non-monotonic noise flagging (local increase during an overall loss is
	// noise only when it fits inside the configured window; bigger reversals
	// are real and stay visible).
	flagNonMonotonic(pts, p, res)

	if driftSrc != nil && driftSrc.Drift != nil {
		res.DriftApplied = driftSrc.Drift.Kind
	}
	res.Points = pts
	return res
}

func num(v domain.Val) (float64, bool) {
	if !v.ConvertOK {
		return 0, false
	}
	return v.Value, true
}

func unconv(idx int, col, text, unit, reason string) domain.UnconvertedCell {
	return domain.UnconvertedCell{Index: idx, Column: col, Raw: domain.Raw{Text: text, Unit: unit}, Reason: reason}
}

func markGaps(pts []domain.SeriesPoint, res *domain.SeriesResult, p domain.Params) {
	i := 0
	for i < len(pts) {
		if pts[i].Present && pts[i].RawMassMg != nil {
			i++
			continue
		}
		start := i
		for i < len(pts) && !(pts[i].Present && pts[i].RawMassMg != nil) {
			i++
		}
		end := i // first good index after the gap
		after := start - 1
		before := end
		g := domain.Gap{AfterIndex: after, BeforeIndex: before, NSkipped: end - start}
		if after >= 0 {
			g.FromS = pts[after].TimeS
		}
		if before < len(pts) {
			g.ToS = pts[before].TimeS
		}
		n := end - start
		g.Interpolated = n <= p.GapMaxBridgeN && after >= 0 && before < len(pts)
		for k := start; k < end; k++ {
			pts[k].Present = false
		}
		if g.Interpolated {
			// Linear interpolation is allowed only for a narrow, bracketed gap.
			for k := start; k < end; k++ {
				frac := float64(k-after) / float64(before-after)
				mg := pts[after].RawMassMg == nil
				_ = mg
				m := (*pts[after].RawMassMg)*(1-frac) + (*pts[before].RawMassMg)*frac
				pts[k].RawMassMg = &m
				if pts[after].TempC != nil && pts[before].TempC != nil {
					tc := (*pts[after].TempC)*(1-frac) + (*pts[before].TempC)*frac
					pts[k].TempC = &tc
				}
			}
			res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
				Code: "gap_interpolated", Severity: "info", StartS: g.FromS, EndS: g.ToS,
				Message: "narrow bracketed sampling gap bridged by linear interpolation"})
		} else {
			res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
				Code: "gap_not_bridged", Severity: "warning", StartS: g.FromS, EndS: g.ToS,
				Message: "sampling gap not interpolated across; stages and DTG stop at its edges"})
		}
		res.Gaps = append(res.Gaps, g)
	}
}

func applyDrift(pts []domain.SeriesPoint, driftSrc, blankSrc *domain.Source, res *domain.SeriesResult) {
	if driftSrc == nil || driftSrc.Drift == nil {
		for i := range pts {
			if pts[i].RawMassMg != nil {
				m := *pts[i].RawMassMg
				pts[i].MassMg = &m
			}
		}
		return
	}
	d := driftSrc.Drift
	blankAt := func(t float64) (float64, bool) {
		if d.Kind != "blank_curve" || blankSrc == nil || len(blankSrc.Blank) == 0 {
			return 0, false
		}
		bp := blankSrc.Blank
		if t <= bp[0].TimeS {
			return bp[0].MassMg, true
		}
		if t >= bp[len(bp)-1].TimeS {
			return bp[len(bp)-1].MassMg, true
		}
		for k := 1; k < len(bp); k++ {
			if t <= bp[k].TimeS {
				x0, x1 := bp[k-1].TimeS, bp[k].TimeS
				fr := 0.0
				if x1 != x0 {
					fr = (t - x0) / (x1 - x0)
				}
				return bp[k-1].MassMg + fr*(bp[k].MassMg-bp[k-1].MassMg), true
			}
		}
		return 0, false
	}
	switch d.Kind {
	case "linear":
		for i := range pts {
			if pts[i].RawMassMg != nil {
				m := *pts[i].RawMassMg - (d.Slope*pts[i].TimeS + d.Intercept)
				pts[i].MassMg = &m
			}
		}
		res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
			Code: "drift_linear", Severity: "info",
			Message: "balance baseline drift removed with linear correction"})
	case "blank_curve":
		used := false
		for i := range pts {
			if pts[i].RawMassMg != nil {
				if b, ok := blankAt(pts[i].TimeS); ok {
					m := *pts[i].RawMassMg - b
					pts[i].MassMg = &m
					used = true
				} else {
					m := *pts[i].RawMassMg
					pts[i].MassMg = &m
				}
			}
		}
		code, sev, msg := "drift_blank", "info", "balance baseline drift removed with blank curve"
		if !used {
			code, sev, msg = "drift_blank_unavailable", "warning", "blank-curve drift selected but no blank value in range; raw mass retained"
		}
		res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{Code: code, Severity: sev, Message: msg})
	default:
		for i := range pts {
			if pts[i].RawMassMg != nil {
				m := *pts[i].RawMassMg
				pts[i].MassMg = &m
			}
		}
	}
}

func flagNonMonotonic(pts []domain.SeriesPoint, p domain.Params, res *domain.SeriesResult) {
	w := p.NoiseMonotonicWindowN
	if w < 1 {
		return
	}
	for i := 1; i < len(pts)-1; i++ {
		if pts[i].MassMg == nil || pts[i-1].MassMg == nil || pts[i+1].MassMg == nil {
			continue
		}
		if *pts[i].MassMg > *pts[i-1].MassMg {
			// local uptick; noise if mass returns to/below the prior level within window
			recovers := false
			for k := i + 1; k <= i+w && k < len(pts); k++ {
				if pts[k].MassMg == nil {
					break
				}
				if *pts[k].MassMg <= *pts[i-1].MassMg {
					recovers = true
					break
				}
			}
			if recovers {
				pts[i].NonMonotonic = true
				res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
					Code: "nonmonotonic_noise", Severity: "info", StartS: pts[i].TimeS,
					Message: "local mass uptick classified as non-monotonic noise; no forced smoothing"})
			}
		}
	}
}
