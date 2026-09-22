package analysis

import (
	"fmt"
	"math"

	"tga-archive/internal/model"
)

// segmentGaps assigns segment ids. A missing time interval (invalid sample or
// a time jump wider than GapMaxS) opens a gap: it is recorded explicitly and
// is NEVER bridged by interpolation.
func segmentGaps(pts []workPoint, p model.Params, d *model.Derived) []model.Gap {
	var gaps []model.Gap
	seg := 0
	var prevValid *workPoint
	segStarted := false
	for i := range pts {
		if !pts[i].massOk {
			if segStarted {
				gaps = append(gaps, model.Gap{StartS: prevValid.t, EndS: pts[i].t, Reason: "invalid sample mass: " + pts[i].rawMass})
				segStarted = false
			}
			continue
		}
		if !segStarted {
			seg++
			pts[i].seg = seg
			segStarted = true
		} else {
			dt := pts[i].t - prevValid.t
			if dt > p.GapMaxS {
				gaps = append(gaps, model.Gap{StartS: prevValid.t, EndS: pts[i].t, Reason: fmt.Sprintf("sampling gap %.1fs > %.1fs; no interpolation across gap", dt, p.GapMaxS)})
				seg++
			}
			pts[i].seg = seg
		}
		prevValid = &pts[i]
	}
	for _, g := range gaps {
		g := g
		d.Findings = append(d.Findings, model.Finding{
			Code: "data_gap_no_interpolation", Severity: model.SevWarning,
			Message: g.Reason, StartS: &g.StartS, EndS: &g.EndS,
		})
	}
	return gaps
}

// markNoise flags non-monotonic up-bounces larger than NoiseAbsPct within an
// otherwise descending region. Suspect points remain plotted but the DTG there
// is distrusted; they never silently delete data.
func markNoise(pts []workPoint, p model.Params, d *model.Derived) {
	n := len(pts)
	for i := 1; i < n-1; i++ {
		if !pts[i].massOk || !pts[i-1].massOk || !pts[i+1].massOk {
			continue
		}
		if pts[i].seg != pts[i-1].seg || pts[i].seg != pts[i+1].seg {
			continue
		}
		up := pts[i].pct - pts[i-1].pct
		down := pts[i].pct - pts[i+1].pct
		if up > p.NoiseAbsPct && down > p.NoiseAbsPct {
			pts[i].suspect = true
			d.Findings = append(d.Findings, model.Finding{
				Code: "nonmonotonic_noise", Severity: model.SevWarning,
				Message: fmt.Sprintf("non-monotonic bounce +%.3f%% at t=%.1fs treated as suspect, not as a mass gain stage", qround(up), pts[i].t),
			})
		}
	}
}

// smoothAndDTG computes a boxcar-smoothed mass % within each segment and then
// DTG = dm%/dt in %/min via central differences. Differentiation never
// crosses segment (gap) boundaries.
func smoothAndDTG(pts []workPoint, p model.Params) {
	sm := make([]float64, len(pts))
	for i := range pts {
		if !pts[i].massOk {
			continue
		}
		var sum, w float64
		for j := range pts {
			if !pts[j].massOk || pts[j].seg != pts[i].seg {
				continue
			}
			if math.Abs(pts[j].t-pts[i].t) <= p.SmoothWinS/2 {
				sum += pts[j].pct
				w++
			}
		}
		if w > 0 {
			sm[i] = sum / w
		}
	}
	for i := range pts {
		if !pts[i].massOk || pts[i].suspect {
			continue
		}
		l, r := i-1, i+1
		if l < 0 || !pts[l].massOk || pts[l].seg != pts[i].seg {
			continue
		}
		if r >= len(pts) || !pts[r].massOk || pts[r].seg != pts[i].seg {
			continue
		}
		dt := pts[r].t - pts[l].t
		if dt <= 0 {
			continue
		}
		v := (sm[r] - sm[l]) / dt * 60.0
		pts[i].dtg = &v
	}
}

func makeSeries(pts []workPoint) []model.SeriesPoint {
	out := make([]model.SeriesPoint, 0, len(pts))
	for _, q := range pts {
		if !q.massOk {
			continue
		}
		tk := q.tk
		out = append(out, model.SeriesPoint{
			TSec: qround(q.t), TK: qround(tk), TempValid: q.tempValid,
			Pct: qround(q.pct), DTG: q.dtg, Suspect: q.suspect, Seg: q.seg,
		})
	}
	return out
}
