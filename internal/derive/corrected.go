package derive

import (
	"math"
	"sort"

	"tgaline/internal/domain"
)

type validPoint struct {
	row                  int
	t, mass, temp        float64
}

// collectValidPoints returns numeric, unit-convertible points in submission
// order, sorted by time. Non-convertible rows stay in the document but never
// enter the series and never get interpolated.
func collectValidPoints(doc *domain.Document) ([]validPoint, []domain.Diagnostic) {
	var vp []validPoint
	var diags []domain.Diagnostic
	seen := map[int]bool{}
	for _, sp := range doc.Samples {
		bad := len(sp.Invalid) > 0
		if bad {
			for _, d := range sp.Invalid {
				d.Scope = "row"
				diags = append(diags, d)
			}
			continue
		}
		if seen[sp.Row] {
			continue
		}
		seen[sp.Row] = true
		vp = append(vp, validPoint{row: sp.Row, t: sp.TimeS, mass: sp.MassMg, temp: sp.TempC})
	}
	sort.SliceStable(vp, func(i, j int) bool { return vp[i].t < vp[j].t })
	return vp, diags
}

// detectGaps flags intervals wider than the threshold. Nothing bridges them.
func detectGaps(vp []validPoint, thresholdS float64) [][2]float64 {
	var gaps [][2]float64
	for i := 1; i < len(vp); i++ {
		if vp[i].t-vp[i-1].t > thresholdS {
			gaps = append(gaps, [2]float64{vp[i-1].t, vp[i].t})
		}
	}
	return gaps
}

// driftAt evaluates the submitted balance drift correction at time t seconds.
func driftAt(in Inputs, tMin float64, t float64) float64 {
	p := in.Params
	if p.DriftMethod == "none" {
		return 0
	}
	if p.DriftMethod == "linear" {
		// µg/min -> mg/min
		return p.DriftLinearMicrogPerMin / 1000.0 * (t/60.0 - tMin/60.0)
	}
	if in.Drift == nil {
		return 0
	}
	tab := tableFromCurve(in.Drift)
	return interpTable(tab, t)
}

type tableEntry struct{ t, v float64 }

func tableFromCurve(doc *domain.Document) []tableEntry {
	var tab []tableEntry
	for _, cp := range doc.Curve {
		if len(cp.Invalid) > 0 {
			continue
		}
		tab = append(tab, tableEntry{cp.TimeS, cp.Value})
	}
	sort.Slice(tab, func(i, j int) bool { return tab[i].t < tab[j].t })
	return tab
}

// interpTable performs piecewise-linear interpolation strictly INSIDE known
// intervals; outside the table range it clamps (never extrapolates) and marks
// the caller's status partial.
func interpTable(tab []tableEntry, t float64) float64 {
	if len(tab) == 0 {
		return 0
	}
	if t <= tab[0].t {
		return tab[0].v
	}
	if t >= tab[len(tab)-1].t {
		return tab[len(tab)-1].v
	}
	for i := 1; i < len(tab); i++ {
		if t <= tab[i].t {
			a, b := tab[i-1], tab[i]
			fr := (t - a.t) / (b.t - a.t)
			return a.v + fr*(b.v-a.v)
		}
	}
	return tab[len(tab)-1].v
}

func blankAt(in Inputs, t float64) (float64, bool) {
	if !in.Params.BlankCorrection || in.Blank == nil {
		return 0, false
	}
	tab := tableFromCurve(in.Blank)
	if len(tab) == 0 {
		return 0, false
	}
	return interpTable(tab, t), true
}

func buildCorrected(projectID string, in Inputs, g *Graph) *domain.Artifact {
	vp, diags := collectValidPoints(in.Samples)
	status := "ok"
	if len(vp) == 0 {
		status = "unknown"
		diags = append(diags, domain.Diagnostic{Scope: "artifact", Code: "no_valid_points",
			Message: "no numeric sample points available", Severity: domain.SeverityError})
	}
	g.Gaps = detectGaps(vp, in.Params.GapThresholdS)
	for _, gp := range g.Gaps {
		diags = append(diags, domain.Diagnostic{Scope: "artifact", Code: "sampling_gap",
			Message: "sampling gap; series is not interpolated across it",
			Severity: domain.SeverityWarning, Raw: fkv(gp[0]) + ".." + fkv(gp[1])})
	}
	tMin := 0.0
	if len(vp) > 0 {
		tMin = vp[0].t
	}
	var nonMono []float64
	points := make([]domain.Point, 0, len(vp))
	for i, v := range vp {
		corrMass := v.mass - driftAt(in, tMin, v.t)
		if bv, ok := blankAt(in, v.t); ok {
			corrMass -= bv
		}
		if i > 0 && corrMass > points[i-1].MassMg+1e-9 {
			// Non-monotonic corrected mass is physically suspicious; it is
			// retained as first-class data (smoothing only affects DTG).
			nonMono = append(nonMono, v.t)
		}
		points = append(points, domain.Point{T: v.t, TempC: v.temp, MassMg: corrMass})
	}
	for i := range points {
		gapBefore := false
		for _, gp := range g.Gaps {
			if math.Abs(points[i].T-gp[1]) < 1e-9 {
				gapBefore = true
			}
		}
		points[i].GapBefore = gapBefore
	}
	for _, t := range nonMono {
		diags = append(diags, domain.Diagnostic{Scope: "artifact", Code: "non_monotonic_mass",
			Message: "corrected mass increases at this time; retained, treated as noise/artifact",
			Severity: domain.SeverityWarning, Raw: fkv(t)})
	}
	if in.Params.DriftMethod == "table" && in.Drift == nil {
		status = "partial"
		diags = append(diags, domain.Diagnostic{Scope: "artifact", Code: "missing_drift_table",
			Message: "drift method=table but no drift document; no drift applied",
			Severity: domain.SeverityWarning})
	}
	if in.Params.BlankCorrection && in.Blank == nil {
		status = demote(status)
		diags = append(diags, domain.Diagnostic{Scope: "artifact", Code: "missing_blank",
			Message: "blank correction requested but no blank curve submitted; uncorrected",
			Severity: domain.SeverityWarning})
	}
	docs := []*domain.Document{in.Samples, in.Drift, in.Blank}
	ih := inputsHash(docs, in.Params, "corrected")
	id := ArtifactID("corrected", ih)
	data := map[string]any{"points": points, "gaps": g.Gaps}
	a := newArtifact(id, projectID, domain.ArtCorrectedSeries, status, data,
		prov(docs, in.Params, g.ParamsFP, ih), diags)
	return a
}

func demote(s string) string {
	if s == "ok" {
		return "partial"
	}
	return s
}
