package tga

import "math"

type blankPt struct{ t, m float64 }

func blankSeries(bs []*Batch) []blankPt {
	out := []blankPt{}
	b := latestBatch(bs)
	if b == nil {
		return out
	}
	for _, it := range b.Items {
		if it.Kind != "blank" && it.Kind != "point" && it.Kind != "" {
			continue
		}
		rawT, _ := rawGet(it.Raw, "time_s", "time", "t")
		rawM, _ := rawGet(it.Raw, "mass_mg", "mass", "m", "blank_mass_mg", "drift_mg")
		t, _, okT := parseTimeS(rawT, "s")
		m, _, okM := parseMassMg(rawM)
		if okT && okM {
			out = append(out, blankPt{t, m})
		}
	}
	return out
}

// driftAnchors parses the dedicated drift-correction layer as (time, offset)
// anchors used by the "linear" model.
func driftAnchors(bs []*Batch) []blankPt {
	out := []blankPt{}
	for _, b := range bs {
		for _, it := range b.Items {
			rawT, _ := rawGet(it.Raw, "time_s", "time", "t")
			rawM, _ := rawGet(it.Raw, "offset_mg", "drift_mg", "correction_mg", "mass", "m")
			t, _, okT := parseTimeS(rawT, "s")
			m, _, okM := parseMassMg(rawM)
			if okT && okM {
				out = append(out, blankPt{t, m})
			}
		}
	}
	return out
}

// applyDrift corrects sample mass. "blank" subtracts an interpolated blank
// baseline shifted to zero at the first sample time; "linear" subtracts a
// least-squares line fitted to the drift-correction anchors.
func applyDrift(rep *Report, blankBatches, driftBatches []*Batch, p Params) {
	blanks := blankSeries(blankBatches)
	anchors := driftAnchors(driftBatches)

	var model func(t float64) (float64, bool)
	if p.DriftModel == "linear" && len(anchors) >= 2 {
		a, b := linfit(anchors)
		model = func(t float64) (float64, bool) { return a*t + b, true }
	} else if len(blanks) >= 2 {
		// Blank curve is stored as absolute empty-pan mass; normalize so its
		// correction at the earliest sample time is zero.
		vp := validPoints(rep)
		if len(vp) == 0 {
			return
		}
		t0 := vp[0].TimeS
		base0 := interp1(blanks, t0)
		model = func(t float64) (float64, bool) {
			v, ok := interp1OK(blanks, t)
			return v - base0, ok
		}
	} else if len(anchors) >= 2 {
		a, b := linfit(anchors)
		model = func(t float64) (float64, bool) { return a*t + b, true }
	} else {
		rep.Warnings = append(rep.Warnings, "缺少可用空白曲线或漂移锚点，漂移校正按 0 处理")
		model = func(t float64) (float64, bool) { return 0, true }
	}

	for i := range rep.Points {
		pt := &rep.Points[i]
		cp := CorrectedPoint{
			ID:        HashID("cpt", map[string]any{"pt": pt.ID, "model": p.DriftModel}),
			PtID:      pt.ID,
			GapAfter:  pt.GapAfter,
			NoiseFlag: pt.NoiseFlag,
		}
		if !pt.Valid {
			cp.Valid = false
			rep.Corrected = append(rep.Corrected, cp)
			continue
		}
		d, ok := model(pt.TimeS)
		if !ok {
			cp.Valid = false
			cp.TimeS, cp.TempC = pt.TimeS, pt.TempC
			rep.Warnings = append(rep.Warnings, "样品点超出空白曲线时间范围，未校正，留为未知")
			rep.Corrected = append(rep.Corrected, cp)
			continue
		}
		cp.TimeS, cp.TempC, cp.DriftMg = pt.TimeS, pt.TempC, d
		cp.CorrMg = pt.MassMg - d
		cp.Valid = true
		rep.Corrected = append(rep.Corrected, cp)
	}
}

func interp1(pts []blankPt, t float64) float64 {
	v, _ := interp1OK(pts, t)
	return v
}

// interp1OK does piecewise-linear interpolation, clamped only within range;
// outside the anchor range it returns ok=false (no extrapolation across gaps).
func interp1OK(pts []blankPt, t float64) (float64, bool) {
	if len(pts) == 0 {
		return 0, false
	}
	if t < pts[0].t || t > pts[len(pts)-1].t {
		return 0, false
	}
	for i := 1; i < len(pts); i++ {
		if t <= pts[i].t {
			a, b := pts[i-1], pts[i]
			if b.t == a.t {
				return b.m, true
			}
			f := (t - a.t) / (b.t - a.t)
			return a.m+(b.m-a.m)*f, true
		}
	}
	return pts[len(pts)-1].m, true
}

// linfit fits m = a*t + b.
func linfit(pts []blankPt) (a, b float64) {
	var sx, sy, sxx, sxy float64
	n := float64(len(pts))
	for _, p := range pts {
		sx += p.t
		sy += p.m
		sxx += p.t * p.t
		sxy += p.t * p.m
	}
	den := n*sxx - sx*sx
	if math.Abs(den) < 1e-12 {
		return 0, sy / n
	}
	a = (n*sxy - sx*sy) / den
	b = (sy - a*sx) / n
	return a, b
}
