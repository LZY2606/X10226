package tga

import "math"

// buildSeries normalizes raw samples into a canonical (t seconds, m mg,
// T °C) series and applies balance drift / blank-curve correction. It never
// interpolates mass across a gap; gaps are explicit first-class state.
func buildSeries(samples, drift, blank *InputNode, p Params, an *Analysis) []Point {
	if samples == nil || len(samples.Rows) == 0 {
		if samples == nil {
			an.Diagnostics = append(an.Diagnostics, Diag{Code: "no-samples", Severity: "warning",
				Message: "no raw-samples layer; TG series is empty"})
		}
		return nil
	}

	type rowInfo struct{ row int }
	_ = rowInfo{}
	points := make([]Point, 0, len(samples.Rows))
	for r, row := range samples.Rows {
		tCell := cellAt(row, 0)
		mCell := cellAt(row, 1)
		tcCell := cellAt(row, 2)
		pt := Point{}
		if tCell.OK {
			pt.T = tCell.Value
		} else {
			an.Diagnostics = append(an.Diagnostics, Diag{Code: "sample-t-gap", Severity: "error",
				Layer: LayerSamples, Row: r + 1, Message: "sample row has no placeable time coordinate"})
			continue
		}
		if mCell.OK {
			v := mCell.Value
			pt.MassRaw = &v
		} else {
			pt.Missing = true
			pt.Note = "gap:" + mCell.Diagnostic
		}
		if tcCell.OK {
			tv := tcCell.Value
			pt.Temp = &tv
		} else if programTemp := programTempAt(p, pt.T, an); programTemp != nil {
			pt.Temp = programTemp
			pt.Note = "temp-from-program-lagged"
		}
		points = append(points, pt)
	}

	sortPoints(points)

	// Explicit sampling gaps: either a GAP row or a time delta beyond the
	// configured threshold. Gap boundaries are recorded on adjacent points so
	// every downstream derivative knows where interpolation is forbidden.
	for i := range points {
		if points[i].Missing {
			points[i].Note = appendNote(points[i].Note, "explicit-gap")
			continue
		}
		if i > 0 && points[i].T-points[i-1].T > p.GapThresholdS {
			points[i].Note = appendNote(points[i].Note, "gap-after")
			points[i-1].Note = appendNote(points[i-1].Note, "gap-before")
			an.Diagnostics = append(an.Diagnostics, Diag{Code: "sampling-gap", Severity: "info",
				Layer: LayerSamples,
				Message: f2("sampling gap %.1f s exceeds threshold %.1f s between t=%.1f and t=%.1f",
					points[i].T-points[i-1].T, p.GapThresholdS, points[i-1].T, points[i].T)})
		}
	}

	driftFn := piecewiseFn(drift, an, LayerDrift, "drift")
	blankMassFn := blankMassFunc(blank, p, an)

	var prevCorr *float64
	for i := range points {
		pt := &points[i]
		if pt.Missing {
			continue
		}
		var correction float64
		known := true
		if d, ok := driftFn(pt.T); ok {
			pt.DriftCorr = &d
			correction += d
		}
		if b, ok := blankMassFn(pt.T); ok {
			correction += b
			pt.DriftCorr = floatPtr(correction)
		} else if driftFn != nil || blankMassFn != nil {
			known = false
		}
		if known {
			corr := *pt.MassRaw - correction
			pt.MassCorr = &corr
			if prevCorr != nil && corr-*prevCorr > p.NoiseTolMg {
				pt.Noise = true
				pt.Note = appendNote(pt.Note, f2("non-monotone: +%.3f mg > tolerance %.3f mg",
					corr-*prevCorr, p.NoiseTolMg))
				an.Diagnostics = append(an.Diagnostics, Diag{Code: "non-monotone-noise", Severity: "warning",
					Layer: LayerSamples,
					Message: f2("mass increase %.3f mg at t=%.1f s exceeds noise tolerance", corr-*prevCorr, pt.T)})
			}
			prevCorr = &corr
		} else {
			pt.Note = appendNote(pt.Note, "correction-unknown")
		}
	}
	return points
}

func cellAt(row []Cell, i int) Cell {
	if i < len(row) {
		return row[i]
	}
	return Cell{Diagnostic: "missing-cell"}
}

func sortPoints(pts []Point) {
	for i := 1; i < len(pts); i++ {
		for j := i; j > 0 && pts[j-1].T > pts[j].T; j-- {
			pts[j-1], pts[j] = pts[j], pts[j-1]
		}
	}
}

func appendNote(a, b string) string {
	if a == "" {
		return b
	}
	return a + ";" + b
}

func floatPtr(v float64) *float64 { return &v }

// piecewiseFn returns a linear interpolation function over a two-column layer
// (t, value). Values outside the layer's time range clamp to the nearest
// endpoint; gaps inside the layer block interpolation.
func piecewiseFn(layer *InputNode, an *Analysis, name, valueCol string) func(float64) (float64, bool) {
	if layer == nil || len(layer.Rows) == 0 {
		return nil
	}
	type tv struct {
		t float64
		v float64
		ok bool
	}
	rows := make([]tv, 0, len(layer.Rows))
	for r, row := range layer.Rows {
		tc := cellAt(row, 0)
		vc := cellAt(row, 1)
		if !tc.OK {
			an.Diagnostics = append(an.Diagnostics, Diag{Code: name + "-t-gap", Severity: "warning",
				Layer: layer.Layer, Row: r + 1, Message: name + " row missing time"})
			continue
		}
		rows = append(rows, tv{t: tc.Value, v: vc.Value, ok: vc.OK})
	}
	if len(rows) == 0 {
		return nil
	}
	return func(t float64) (float64, bool) {
		if t <= rows[0].t {
			if !rows[0].ok {
				return 0, false
			}
			return rows[0].v, true
		}
		last := rows[len(rows)-1]
		if t >= last.t {
			if !last.ok {
				return 0, false
			}
			return last.v, true
		}
		for i := 1; i < len(rows); i++ {
			if t <= rows[i].t {
				a, b := rows[i-1], rows[i]
				if !a.ok || !b.ok {
					return 0, false // gap in the correction layer: no interpolation
				}
				if b.t-a.t > 0 {
					frac := (t - a.t) / (b.t - a.t)
					return a.v + frac*(b.v-a.v), true
				}
				return b.v, true
			}
		}
		return 0, false
	}
}

// blankMassFunc extracts the mass column of the blank-curve layer.
func blankMassFunc(blank *InputNode, p Params, an *Analysis) func(float64) (float64, bool) {
	if blank == nil {
		return nil
	}
	fn := piecewiseFn(blank, an, "blank", "m")
	if fn == nil {
		return nil
	}
	return fn
}

func programTempAt(p Params, t float64, an *Analysis) *float64 {
	if !math.IsNaN(p.TempLagS) {
		_ = p.TempLagS
	}
	return nil
}
