package calc

import (
	"fmt"
	"math"
	"sort"

	"gotga/internal/domain"
)

// Compute runs the deterministic TGA pipeline version in params.Version.
// Results are content-addressed; local input change expires only dependents.
func Compute(in *Inputs, params domain.Params, eventSeq int64, extraDiags []string) *domain.ResultBundle {
	b := &domain.ResultBundle{EventSeq: eventSeq, ParamFP: domain.ParamFingerprint(params)}
	b.Diags = append(b.Diags, extraDiags...)

	valid := make([]domain.SamplePoint, 0, len(in.Samples))
	var dropped []domain.SamplePoint
	for _, p := range in.Samples {
		if p.T != nil && p.M != nil {
			valid = append(valid, p)
		} else {
			dropped = append(dropped, p)
		}
	}
	if len(dropped) > 0 {
		b.Diags = append(b.Diags, fmt.Sprintf("%d unconvertible sample point(s) kept in raw layer, excluded from derived curve", len(dropped)))
	}
	sort.SliceStable(valid, func(i, j int) bool { return *valid[i].T < *valid[j].T })
	if len(valid) < 2 {
		b.Diags = append(b.Diags, "fewer than 2 convertible points; TG/DTG cannot be derived")
		b.Residues = unknownResidues(in, params, eventSeq)
		b.Judgments = append(b.Judgments, mkJudgment(domain.AxiomUnconvertibleKept, "series", "series", "unconvertible points retained in raw documents", eventSeq))
		return b
	}

	times := make([]float64, len(valid))
	for i, p := range valid {
		times[i] = *p.T
	}
	gapThresh := medianGap(times) * params.GapFactor
	segments, gaps := splitByGaps(valid, gapThresh, in, params, eventSeq)
	b.Diags = append(b.Diags, gapDiags(gaps)...)

	temp := buildTemperature(valid, in)
	corrected, driftNote := applyDrift(valid, in, params)

	var seriesSegs [][]domain.Point
	var noises []domain.NoiseRun
	for si, seg := range segments {
		pts := make([]domain.Point, 0, len(seg))
		for _, idx := range seg {
			pts = append(pts, domain.Point{
				T: times[idx], M: corrected[idx], D: 0, Tmp: temp[idx], Seg: fmt.Sprintf("seg%d", si+1),
			})
		}
		if len(pts) >= 2 {
			dtg := smooth(centralDTG(pts), params.SmoothWindow)
			for i := range pts {
				pts[i].D = dtg[i]
			}
			noises = append(noises, detectNoise(pts, params)...)
		}
		seriesSegs = append(seriesSegs, pts)
	}

	docIDs := []string{in.SampleDoc.ID}
	if in.DriftDoc != nil {
		docIDs = append(docIDs, in.DriftDoc.ID)
	}
	if in.BlankDoc != nil {
		docIDs = append(docIDs, in.BlankDoc.ID)
	}
	seriesID := domain.HashID("series", params, len(seriesSegs), firstLast(seriesSegs), driftNote)
	b.Series = &domain.Series{
		ID: seriesID, Basis: "t[s] furnace time; m[mg] corrected; d[mg/min]; T[C]",
		Segments: seriesSegs, Gaps: gaps,
		Prov: domain.Provenance{
			Algorithm: params.Version + ":tg-dtg", ParamFP: b.ParamFP,
			DocIDs: docIDs, EventSeq: eventSeq,
			Formula: "D_i=(m_{i+1}-m_{i-1})/(t_{i+1}-t_{i-1})*60, gaps never spanned",
		},
	}
	b.Noises = noises
	b.DriftNote = driftNote

	peaks := detectPeaks(seriesSegs, params, b.ParamFP, eventSeq, in)
	b.Peaks = peaks
	b.Stages = buildStages(seriesSegs, peaks, gaps, in, params, b.ParamFP, eventSeq)
	b.Residues = buildResidues(seriesSegs, in, params, b.ParamFP, eventSeq, gaps)
	b.Judgments = buildJudgments(b, in, params, eventSeq, gaps)
	return b
}

func firstLast(segs [][]domain.Point) [4]float64 {
	var out [4]float64
	if len(segs) > 0 && len(segs[0]) > 0 {
		out[0], out[1] = segs[0][0].T, segs[0][0].M
	}
	if n := len(segs); n > 0 && len(segs[n-1]) > 0 {
		q := segs[n-1]
		out[2], out[3] = q[len(q)-1].T, q[len(q)-1].M
	}
	return out
}

func medianGap(ts []float64) float64 {
	if len(ts) < 2 {
		return math.MaxFloat64
	}
	g := make([]float64, 0, len(ts)-1)
	for i := 1; i < len(ts); i++ {
		g = append(g, ts[i]-ts[i-1])
	}
	sort.Float64s(g)
	return g[len(g)/2]
}

// splitByGaps breaks the point index list; no interpolation ever crosses.
func splitByGaps(pts []domain.SamplePoint, thresh float64, in *Inputs, p domain.Params, seq int64) ([][]int, []domain.Gap) {
	var segs [][]int
	var gaps []domain.Gap
	cur := []int{0}
	for i := 1; i < len(pts); i++ {
		dt := *pts[i].T - *pts[i-1].T
		if dt > thresh {
			gap := domain.Gap{
				FromT: *pts[i-1].T, ToT: *pts[i].T, Spanned: false,
				FromTmp: math.NaN(), ToTmp: math.NaN(),
			}
			gaps = append(gaps, gap)
			segs = append(segs, cur)
			cur = []int{i}
			continue
		}
		cur = append(cur, i)
	}
	segs = append(segs, cur)
	return segs, gaps
}

func gapDiags(gs []domain.Gap) []string {
	out := make([]string, 0, len(gs))
	for _, g := range gs {
		out = append(out, fmt.Sprintf("sampling gap %.0fs-%.0fs: no interpolation across it (%.0fs missing)", g.FromT, g.ToT, g.ToT-g.FromT))
	}
	return out
}

func centralDTG(pts []domain.Point) []float64 {
	d := make([]float64, len(pts))
	for i := range pts {
		switch {
		case i == 0:
			d[i] = (pts[1].M - pts[0].M) / (pts[1].T - pts[0].T) * 60
		case i == len(pts)-1:
			d[i] = (pts[i].M - pts[i-1].M) / (pts[i].T - pts[i-1].T) * 60
		default:
			d[i] = (pts[i+1].M - pts[i-1].M) / (pts[i+1].T - pts[i-1].T) * 60
		}
	}
	return d
}

func smooth(v []float64, w int) []float64 {
	if w < 2 || w%2 == 0 {
		w = 3
	}
	half := w / 2
	out := make([]float64, len(v))
	for i := range v {
		var sum float64
		var n int
		for k := i - half; k <= i+half; k++ {
			if k >= 0 && k < len(v) {
				sum += v[k]
				n++
			}
		}
		out[i] = sum / float64(n)
	}
	return out
}
