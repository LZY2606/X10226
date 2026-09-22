package engine

import (
	"math"
	"sort"

	"tga-review/internal/domain"
	"tga-review/internal/importer"
)

func buildSegments(snap *Snapshot, dep string, vs *domain.ValidatedSeries) (domain.Artifact, []domain.Segment) {
	var segs []domain.Segment
	usable := []domain.Sample{}
	for _, s := range vs.Samples {
		if !s.Excluded {
			usable = append(usable, s)
		}
	}
	if len(usable) == 0 {
		return mkArtifact(snap, domain.ArtSegments, "segments", "Sampling segments split at gaps", nil, []string{dep}, segs), segs
	}
	// median spacing
	deltas := make([]float64, 0, len(usable)-1)
	for i := 1; i < len(usable); i++ {
		d := usable[i].TimeS - usable[i-1].TimeS
		if d > 0 {
			deltas = append(deltas, d)
		}
	}
	med := median(deltas)
	start := 0
	for i := 1; i < len(usable); i++ {
		d := usable[i].TimeS - usable[i-1].TimeS
		if med > 0 && d > GapFactor*med {
			segs = append(segs, domain.Segment{
				ID: segID(len(segs)), StartIndex: usable[start].Index, EndIndex: usable[i-1].Index,
				StartTimeS: usable[start].TimeS, EndTimeS: usable[i-1].TimeS, GapAfterS: d,
			})
			start = i
		}
	}
	segs = append(segs, domain.Segment{
		ID: segID(len(segs)), StartIndex: usable[start].Index, EndIndex: usable[len(usable)-1].Index,
		StartTimeS: usable[start].TimeS, EndTimeS: usable[len(usable)-1].TimeS,
	})
	art := mkArtifact(snap, domain.ArtSegments, "segments",
		"Sampling segments split at gaps (no interpolation across gaps)", nil, []string{dep}, segs)
	return art, segs
}

func buildCorrected(snap *Snapshot, dep string, vs *domain.ValidatedSeries) (domain.Artifact, *domain.CorrectedSeries) {
	driftKind := snap.Params.DriftKind
	rate := snap.Params.DriftRateMgH // mg per hour
	blanks := blankPoints(snap)
	t0 := 0.0
	if len(vs.Samples) > 0 {
		t0 = vs.Samples[0].TimeS
	}
	cs := &domain.CorrectedSeries{DriftModel: driftKind}
	for _, s := range vs.Samples {
		row := domain.CorrectedSample{
			Index: s.Index, TimeS: s.TimeS, TempC: s.TempC, ObservedMg: s.MassMg,
		}
		if !s.Excluded {
			if driftKind == "linear" {
				row.DriftMg = rate * ((s.TimeS - t0) / 3600.0)
			}
			row.BlankMg = interpBlanks(blanks, s.TimeS)
			row.CorrectedMg = s.MassMg - row.DriftMg - row.BlankMg
		}
		cs.Samples = append(cs.Samples, row)
	}
	src := []string{}
	for _, rs := range snap.Records {
		for _, r := range rs {
			if r.Kind == domain.SrcDrift || r.Kind == domain.SrcBlank {
				src = append(src, r.ID)
			}
		}
	}
	sort.Strings(src)
	art := mkArtifact(snap, domain.ArtCorrected, "corrected-series",
		"Baseline-drift and blank-curve corrected mass", src, []string{dep}, cs)
	return art, cs
}

func buildDTG(snap *Snapshot, dep string, cs *domain.CorrectedSeries, segs []domain.Segment) (domain.Artifact, *domain.DTGSeries) {
	segOf := map[int]int{}
	for si, sg := range segs {
		for i := sg.StartIndex; i <= sg.EndIndex; i++ {
			segOf[i] = si
		}
	}
	out := &domain.DTGSeries{}
	for _, c := range cs.Samples {
		p := domain.DTGPoint{Index: c.Index, TimeS: c.TimeS, TempC: c.TempC}
		out.Points = append(out.Points, p)
	}
	for si, sg := range segs {
		idxs := []int{}
		for i := sg.StartIndex; i <= sg.EndIndex; i++ {
			idxs = append(idxs, i)
		}
		for k, i := range idxs {
			if k == 0 || k == len(idxs)-1 {
				continue
			}
			a, b := cs.Samples[idxs[k-1]], cs.Samples[idxs[k+1]]
			dtMin := (b.TimeS - a.TimeS) / 60.0
			if dtMin <= 0 {
				continue
			}
			rate := (b.CorrectedMg - a.CorrectedMg) / dtMin
			out.Points[i].DTGMgPerMin = rate
			if init := currentInitial(snap); init > 0 {
				out.Points[i].DTGPerMin = rate / init
			}
			_ = si
		}
		// boundary one-sided derivatives
		if len(idxs) >= 2 {
			a, b := cs.Samples[idxs[0]], cs.Samples[idxs[1]]
			if dt := (b.TimeS - a.TimeS) / 60.0; dt > 0 {
				out.Points[idxs[0]].DTGMgPerMin = (b.CorrectedMg - a.CorrectedMg) / dt
			}
			a, b = cs.Samples[idxs[len(idxs)-2]], cs.Samples[idxs[len(idxs)-1]]
			if dt := (b.TimeS - a.TimeS) / 60.0; dt > 0 {
				out.Points[idxs[len(idxs)-1]].DTGMgPerMin = (b.CorrectedMg - a.CorrectedMg) / dt
			}
		}
	}
	// mark points immediately bordering gaps
	for i := range out.Points {
		if segOf[i] != i {
		}
	}
	art := mkArtifact(snap, domain.ArtDTG, "dtg-series",
		"DTG from central differences within segments (gaps not crossed)", nil, []string{dep}, out)
	return art, out
}

func blankPoints(snap *Snapshot) [][2]float64 {
	var pts [][2]float64
	for _, r := range snap.Records[domain.SrcBlank] {
		f := r.Fields
		t, _, _ := importer.NormalizeTime(f[1], f[2])
		m, _, _ := importer.NormalizeMass(f[3], f[4])
		pts = append(pts, [2]float64{t, m})
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i][0] < pts[j][0] })
	return pts
}

func interpBlanks(pts [][2]float64, t float64) float64 {
	if len(pts) == 0 {
		return 0
	}
	if t <= pts[0][0] {
		return pts[0][1]
	}
	last := pts[len(pts)-1]
	if t >= last[0] {
		return last[1]
	}
	for i := 1; i < len(pts); i++ {
		if t <= pts[i][0] {
			a, b := pts[i-1], pts[i]
			frac := (t - a[0]) / (b[0] - a[0])
			return a[1] + frac*(b[1]-a[1])
		}
	}
	return last[1]
}

func median(xs []float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	cp := append([]float64{}, xs...)
	sort.Float64s(cp)
	n := len(cp)
	if n%2 == 1 {
		return cp[n/2]
	}
	return (cp[n/2-1] + cp[n/2]) / 2
}

func currentInitial(snap *Snapshot) float64 {
	recs := snap.Records[domain.SrcInitial]
	if len(recs) == 0 {
		return 0
	}
	r := recs[len(recs)-1]
	m, _, err := importer.NormalizeMass(r.Fields[1], r.Fields[2])
	if err != nil {
		return math.NaN()
	}
	return m
}
