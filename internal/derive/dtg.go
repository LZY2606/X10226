package derive

import "tgaline/internal/domain"

// segments splits points at gaps so no derivative ever crosses a gap.
func segments(pts []domain.Point) [][]domain.Point {
	var segs [][]domain.Point
	start := 0
	for i := 1; i < len(pts); i++ {
		if pts[i].GapBefore {
			segs = append(segs, pts[start:i])
			start = i
		}
	}
	if start < len(pts) {
		segs = append(segs, pts[start:])
	}
	return segs
}

func buildDTG(projectID string, in Inputs, g *Graph, tg *domain.Artifact) *domain.Artifact {
	pts := asPoints(tg)
	w := in.Params.SmoothWindow
	if w < 0 {
		w = 0
	}
	status := tg.Status
	diags := append([]domain.Diagnostic(nil), tg.Diagnostics...)
	out := make([]domain.Point, len(pts))
	copy(out, pts)
	for _, seg := range segments(pts) {
		raw := make([]float64, len(seg))
		// central difference on time (irregular spacing respected)
		for i := range seg {
			switch {
			case i == 0:
				if len(seg) > 1 && seg[1].T > seg[0].T {
					raw[i] = (seg[1].MassMg-seg[0].MassMg)/(seg[1].T-seg[0].T)*60
				}
			case i == len(seg)-1:
				if seg[i].T > seg[i-1].T {
					raw[i] = (seg[i].MassMg-seg[i-1].MassMg)/(seg[i].T-seg[i-1].T)*60
				}
			default:
				if seg[i+1].T > seg[i-1].T {
					raw[i] = (seg[i+1].MassMg-seg[i-1].MassMg)/(seg[i+1].T-seg[i-1].T)*60
				}
			}
		}
		// boxcar smoothing, clamped inside the segment: noise on a monotone
		// trend is reduced but the raw points remain in tg/corrected artifacts.
		sm := make([]float64, len(seg))
		for i := range seg {
			var sum float64
			n := 0
			for k := i - w; k <= i+w; k++ {
				if k >= 0 && k < len(raw) {
					sum += raw[k]
					n++
				}
			}
			sm[i] = sum / float64(n)
		}
		// write back at matching indices
		base := indexOf(pts, seg[0].T)
		for i := range seg {
			out[base+i].DTGMgPerMin = sm[i]
		}
	}
	docs := []*domain.Document{in.Samples, in.Drift, in.Blank, in.InitialMass}
	ih := inputsHash(docs, in.Params, "dtg")
	id := ArtifactID("dtg", ih)
	data := map[string]any{"points": out, "smooth_window": w, "segments": len(segments(pts))}
	return newArtifact(id, projectID, domain.ArtDTGSeries, status, data,
		prov(docs, in.Params, g.ParamsFP, ih, tg.ID), diags)
}

func indexOf(pts []domain.Point, t float64) int {
	for i := range pts {
		if pts[i].T == t {
			return i
		}
	}
	return 0
}
