package tga

import (
	"math"
	"sort"
)

// defaultParams is the version-0 analysis parameter set.
func defaultParams() Params {
	return Params{
		Version: 0, GapMin: 2.0, AtmosDelayMin: 0.5, TempLagC: 3.0,
		DriftCorrect: true, BlankCorrect: true, SmoothWindow: 5,
		ProminenceMg: 0.05,
	}
}

// buildSeries normalises accepted raw sample points into canonical units,
// sorts by furnace time, flags non-monotone noise and splits segments at
// sampling gaps. It never interpolates across a gap.
func buildSeries(samples []RawSamplePoint, drift *NormDrift, blank *NormBlank, p Params, srcs []SourceVersion, initial *NormInitialMass) *NormSeries {
	art := &NormSeries{Algorithm: AlgoNormalise, ParamFP: p.fingerprint(), Sources: srcs}
	type pt struct{ t, T, m float64; row int }
	pts := make([]pt, 0, len(samples))
	for _, s := range samples {
		tv, ok1 := s.Time.NormFloat()
		tp, ok2 := s.Temp.NormFloat()
		mv, ok3 := s.Mass.NormFloat()
		if ok1 && ok2 && ok3 {
			pts = append(pts, pt{tv, tp, mv, s.Row})
		}
	}
	sort.Slice(pts, func(i, j int) bool { return pts[i].t < pts[j].t })

	// Balance baseline drift correction: m_corr = m - rate*t.
	var driftRate float64
	if p.DriftCorrect && drift != nil && drift.Drift != nil && drift.Drift.Rate.OK() {
		driftRate, _ = drift.Drift.Rate.NormFloat()
	}
	// Blank-curve correction: subtract nearest blank reading (no crossing of
	// gaps: nearest only within a safe window, otherwise leave unchanged).
	for i := range pts {
		m := pts[i].m
		if driftRate != 0 {
			m -= driftRate * pts[i].t
		}
		if p.BlankCorrect && blank != nil {
			if b, ok := blankAt(blank, pts[i].t); ok {
				m -= b
			}
		}
		pts[i].m = m
	}

	prevT := math.NaN()
	mono := true
	norm := make([]NormSample, 0, len(pts))
	for i, x := range pts {
		okMono := true
		if i > 0 && x.m > pts[i-1].m+1e-9 {
			okMono = false
			mono = false
			art.Diags = append(art.Diags, Diagnostic{Severity: "warning", Section: SecSamples, Row: x.row, Message: "non-monotonic mass increase retained (noise), not smoothed away"})
		}
		_ = prevT
		norm = append(norm, NormSample{Index: i, Row: x.row, TimeMin: x.t, TempC: x.T, MassMg: x.m, MonotonicOK: okMono})
	}
	if !mono {
		art.Diags = append(art.Diags, Diagnostic{Severity: "warning", Section: SecSamples, Message: "series contains non-monotonic noise; points retained"})
	}

	// Split segments at sampling gaps wider than p.GapMin.
	segs := []Segment{}
	start := 0
	gapBefore := false
	for i := 1; i < len(norm); i++ {
		dt := norm[i].TimeMin - norm[i-1].TimeMin
		if dt > p.GapMin+1e-9 {
			segs = append(segs, Segment{ID: segID(srcs, start, i-1), Start: start, End: i - 1, GapBefore: gapBefore})
			art.Diags = append(art.Diags, Diagnostic{Severity: "warning", Section: SecSamples, Row: norm[i].Row, Message: "sampling gap: no interpolation across gap", Field: "t", Cell: fnum(dt) + "min"})
			start = i
			gapBefore = true
		}
	}
	if len(norm) > 0 {
		segs = append(segs, Segment{ID: segID(srcs, start, len(norm)-1), Start: start, End: len(norm) - 1, GapBefore: gapBefore})
	}
	art.Samples = norm
	art.Segments = segs
	art.ArtifactID = artifactID(AlgoNormalise, p.fingerprint(), srcs, map[string]any{
		"n": len(norm), "gaps": len(segs), "drift": driftRate,
	})
	return art
}

func segID(srcs []SourceVersion, a, b int) string {
	return "seg_" + short(hashCanonical([]any{srcs, a, b}))
}

func fnum(v float64) string {
	return trimNum(v)
}

// blankAt returns the blank mass nearest time t, only if it lies within the
// same observed window (no extrapolation).
func blankAt(b *NormBlank, t float64) (float64, bool) {
	best := math.MaxFloat64
	var bv float64
	found := false
	for _, q := range b.Points {
		if d := math.Abs(q.TimeMin - t); d < best {
			best = d
			bv = q.MassMg
			found = true
		}
	}
	if !found {
		return 0, false
	}
	return bv, true
}
