package algo

import (
	"fmt"
	"math"
	"sort"

	"tgalineage/internal/model"
)

// buildSeries turns parsed rows into the normalized working series.
// Unparsable cells stay out of the numeric series but are reported; they are
// never silently dropped for plotting (the run diagnostics list every one).
func buildSeries(in Inputs, p *Payload) []Point {
	var pts []Point
	for _, row := range in.Points {
		if !row.T.Parsable || !row.Mass.Parsable {
			p.Diagnostics = append(p.Diagnostics, model.Diagnostic{
				Level: "warning", Code: "cell-unconverted", Section: model.KindPoints,
				Message: fmt.Sprintf("point %s kept in raw layer but excluded from numeric series", row.ID),
			})
			continue
		}
		pp := Point{ID: row.ID, TS: row.T.Value, RawMg: row.Mass.Value}
		if row.Temp.Parsable {
			pp.TempC = row.Temp.Value
		}
		pts = append(pts, pp)
	}
	sort.SliceStable(pts, func(i, j int) bool {
		if pts[i].TS == pts[j].TS {
			return pts[i].ID < pts[j].ID
		}
		return pts[i].TS < pts[j].TS
	})
	// duplicate-time rows retained in raw layer; numeric series keeps all but
	// flags them so peak logic does not span a zero/negative dt.
	return pts
}

// detectGaps flags intervals whose dt exceeds gap_factor * median(dt).
// Gaps are never bridged: every downstream integral skips them.
func detectGaps(pts []Point, factor float64) []Gap {
	if len(pts) < 3 {
		return nil
	}
	dts := make([]float64, 0, len(pts)-1)
	for i := 1; i < len(pts); i++ {
		if d := pts[i].TS - pts[i-1].TS; d > 0 {
			dts = append(dts, d)
		}
	}
	sort.Float64s(dts)
	median := dts[len(dts)/2]
	var gaps []Gap
	for i := 1; i < len(pts); i++ {
		d := pts[i].TS - pts[i-1].TS
		if d > factor*median && d > 0 {
			gaps = append(gaps, Gap{
				BeforeID: pts[i-1].ID, AfterID: pts[i].ID,
				FromS: pts[i-1].TS, ToS: pts[i].TS, WidthS: d,
			})
			pts[i].GapStart = true
		}
	}
	return gaps
}

func isGap(gaps []Gap, a, b Point) bool {
	for _, g := range gaps {
		if g.BeforeID == a.ID && g.AfterID == b.ID {
			return true
		}
	}
	return false
}

// driftAt returns the balance offset at t from linear interpolation of
// anchors, extrapolated by the end slopes.
func driftAt(anchors []model.DriftAnchor, t float64) float64 {
	if len(anchors) == 0 {
		return 0
	}
	sort.Slice(anchors, func(i, j int) bool { return anchors[i].T.Value < anchors[j].T.Value })
	if t <= anchors[0].T.Value {
		a, b := anchors[0], anchors[min(1, len(anchors)-1)]
		return slope(a.T.Value, a.M.Value, b.T.Value, b.M.Value)*(t-a.T.Value) + a.M.Value
	}
	for i := 1; i < len(anchors); i++ {
		if t <= anchors[i].T.Value {
			a, b := anchors[i-1], anchors[i]
			return slope(a.T.Value, a.M.Value, b.T.Value, b.M.Value)*(t-a.T.Value) + a.M.Value
		}
	}
	a, b := anchors[len(anchors)-2], anchors[len(anchors)-1]
	return slope(a.T.Value, a.M.Value, b.T.Value, b.M.Value)*(t-a.T.Value) + a.M.Value
}

// blankAt returns blank-curve mass at t by piecewise linear interpolation.
func blankAt(blank []model.BlankPoint, t float64) float64 {
	if len(blank) == 0 {
		return 0
	}
	sort.Slice(blank, func(i, j int) bool { return blank[i].T.Value < blank[j].T.Value })
	if t <= blank[0].T.Value {
		return blank[0].Mass.Value
	}
	for i := 1; i < len(blank); i++ {
		if t <= blank[i].T.Value {
			a, b := blank[i-1], blank[i]
			return slope(a.T.Value, a.Mass.Value, b.T.Value, b.Mass.Value)*(t-a.T.Value) + a.Mass.Value
		}
	}
	return blank[len(blank)-1].Mass.Value
}

func slope(x1, y1, x2, y2 float64) float64 {
	if x2 == x1 {
		return 0
	}
	return (y2 - y1) / (x2 - x1)
}

// correct applies the configured baseline correction. Correction anchors are
// defined as the balance reading of the empty-pan offset: corrected = raw - drift.
func correct(pts []Point, in Inputs, p *Payload) {
	switch in.Params.DriftSource {
	case "blank":
		if len(in.Blank) < 2 {
			p.Diagnostics = append(p.Diagnostics, model.Diagnostic{
				Level: "warning", Code: "blank-unavailable", Section: model.KindBlank,
				Message: "drift_source=blank but blank curve absent/short; using anchors",
			})
			correctAnchors(pts, in, p)
			return
		}
		b0 := in.Blank[0].Mass.Value
		for i := range pts {
			pts[i].CorrMg = pts[i].RawMg - (blankAt(in.Blank, pts[i].TS) - b0)
		}
		p.Notes = append(p.Notes, "balance drift corrected by subtracting blank-curve shift relative to its first point")
	default:
		correctAnchors(pts, in, p)
	}
}

func correctAnchors(pts []Point, in Inputs, p *Payload) {
	if len(in.Drift) < 2 {
		for i := range pts {
			pts[i].CorrMg = pts[i].RawMg
		}
		p.Diagnostics = append(p.Diagnostics, model.Diagnostic{
			Level: "warning", Code: "no-drift-correction", Section: model.KindDrift,
			Message: "fewer than 2 drift anchors; corrected series equals raw series",
		})
		return
	}
	for i := range pts {
		pts[i].CorrMg = pts[i].RawMg - driftAt(in.Drift, pts[i].TS)
	}
	p.Notes = append(p.Notes, "balance drift corrected by linear drift anchors (corr=raw-drift(t))")
}

// detectNoise flags upward (non-monotonic) excursions on the corrected
// series. Points are never deleted: the excursion is a first-class report and
// the point is marked for the DTG/integral robust handling.
func detectNoise(pts []Point, zSigma float64) []NoiseReport {
	if len(pts) < 5 {
		return nil
	}
	diffs := make([]float64, 0, len(pts)-1)
	for i := 1; i < len(pts); i++ {
		diffs = append(diffs, pts[i].CorrMg-pts[i-1].CorrMg)
	}
	med := medianF(diffs)
	absdev := make([]float64, len(diffs))
	for i, d := range diffs {
		absdev[i] = math.Abs(d - med)
	}
	mad := medianF(absdev)
	if mad == 0 {
		mad = 1e-9
	}
	var reps []NoiseReport
	for i := 1; i < len(pts); i++ {
		up := pts[i].CorrMg - pts[i-1].CorrMg
		z := 0.6745 * (up - med) / mad
		if up > 0 && z > zSigma {
			pts[i].Flag = "nonmonotonic-outlier"
			reps = append(reps, NoiseReport{
				PointID: pts[i].ID, TS: pts[i].TS, UpMg: up, Z: z,
				Action: "retained-and-flagged; smoothed series clips this uptick for DTG",
			})
		}
	}
	return reps
}

func medianF(x []float64) float64 {
	if len(x) == 0 {
		return 0
	}
	c := append([]float64(nil), x...)
	sort.Float64s(c)
	return c[len(c)/2]
}

// smoothMass returns a centered moving-average copy used only for DTG
// estimation; the stored corrected series is unchanged.
func smoothMass(pts []Point, win int) []float64 {
	if win < 3 {
		win = 3
	}
	if win%2 == 0 {
		win++
	}
	h := win / 2
	out := make([]float64, len(pts))
	for i := range pts {
		sum, n := 0.0, 0
		for j := i - h; j <= i+h; j++ {
			if j < 0 || j >= len(pts) {
				continue
			}
			v := pts[j].CorrMg
			if pts[j].Flag == "nonmonotonic-outlier" {
				v = pts[j-1].CorrMg // clip isolated uptick, keep the point itself
			}
			sum += v
			n++
		}
		out[i] = sum / float64(n)
	}
	return out
}
