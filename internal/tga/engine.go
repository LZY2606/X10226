package tga

import (
	"fmt"
	"math"
	"sort"
)

// Inputs bundles the latest parsed version of every raw layer.
type Inputs struct {
	SamplePoints []Point
	Drift        DriftModel
	Furnace      []FurnSeg
	GasEvents    []GasEvent
	Blank        []XYPoint
	InitialMass  InitialMass
}

// Result is the full derived output of one engine run.
type Result struct {
	TG         Curve
	DTG        Curve
	Segments   [][2]int // inclusive index ranges into TG points
	Atmosphere []AtmoInterval
	Candidates []StageCandidate
	Residue    *Residue
	Diags      []Diagnostic
}

// blankLoss interpolates the blank curve at t. It never interpolates
// across a sampling gap wider than gapS: the second return value is
// false when t falls inside such a gap.
func blankLoss(blank []XYPoint, t, gapS float64) (float64, bool) {
	if len(blank) == 0 {
		return 0, true
	}
	if t <= blank[0].X {
		return blank[0].Y, true
	}
	for i := 1; i < len(blank); i++ {
		if t <= blank[i].X {
			x0, y0 := blank[i-1].X, blank[i-1].Y
			x1, y1 := blank[i].X, blank[i].Y
			if x1-x0 > gapS {
				return 0, false // gap: no interpolation across
			}
			if x1 == x0 {
				return y1, true
			}
			return y0 + (y1-y0)*(t-x0)/(x1-x0), true
		}
	}
	return blank[len(blank)-1].Y, true
}

// splitSegments finds continuous runs of sample points separated by
// acquisition gaps. No derived quantity ever interpolates across a gap.
func splitSegments(pts []Point, gapS float64) ([][2]int, []Diagnostic) {
	var segs [][2]int
	var diags []Diagnostic
	start := 0
	for i := 1; i < len(pts); i++ {
		if pts[i].T-pts[i-1].T > gapS {
			segs = append(segs, [2]int{start, i - 1})
			diags = append(diags, Diagnostic{
				Layer:    LayerSamplePoints,
				Location: fmt.Sprintf("t=%.0f..%.0f s", pts[i-1].T, pts[i].T),
				Message:  fmt.Sprintf("sampling gap of %.0f s: no interpolation across", pts[i].T-pts[i-1].T),
			})
			start = i
		}
	}
	segs = append(segs, [2]int{start, len(pts) - 1})
	return segs, diags
}

// atmosphereTimeline converts valve events into effective atmosphere
// intervals at the sample, applying transport delay and temperature lag.
func atmosphereTimeline(events []GasEvent, p ParamSet) []AtmoInterval {
	sorted := append([]GasEvent(nil), events...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].T < sorted[j].T })
	var out []AtmoInterval
	initial := "N2"
	if len(sorted) > 0 && sorted[0].From != "" {
		initial = sorted[0].From
	}
	out = append(out, AtmoInterval{
		Gas:        initial,
		ValveT:     0,
		EffectiveT: 0,
		Note:       "initial purge gas",
	})
	for _, e := range sorted {
		eff := e.T + p.AtmosphereDelayS
		out = append(out, AtmoInterval{
			Gas:        e.To,
			ValveT:     e.T,
			EffectiveT: eff,
			DelayS:     p.AtmosphereDelayS,
			TempLagS:   p.TempLagS,
			Note: fmt.Sprintf("valve switched at %.0f s but gas reaches sample at %.0f s (delay %.0f s, temp lag %.0f s): valve time != sample reaction time",
				e.T, eff, p.AtmosphereDelayS, p.TempLagS),
		})
	}
	return out
}

// gasAt returns the effective atmosphere at sample time t.
func gasAt(atmo []AtmoInterval, t float64) string {
	gas := ""
	for _, a := range atmo {
		if t >= a.EffectiveT {
			gas = a.Gas
		}
	}
	return gas
}

// Run executes the full derivation pipeline.
func Run(in Inputs, p ParamSet) Result {
	var res Result
	pts := in.SamplePoints
	if len(pts) == 0 {
		res.Diags = append(res.Diags, Diagnostic{Layer: LayerSamplePoints, Location: "-", Message: "no sample points"})
		return res
	}

	// 1. drift + blank correction, per point, never across gaps.
	segs, gapDiags := splitSegments(pts, p.GapThresholdS)
	res.Segments = segs
	res.Diags = append(res.Diags, gapDiags...)
	corrected := make([]Point, len(pts))
	blankOK := make([]bool, len(pts))
	for i, pt := range pts {
		m := pt.Mass - in.Drift.Offset(pt.T)
		loss, ok := blankLoss(in.Blank, pt.T, p.GapThresholdS)
		blankOK[i] = ok
		if ok {
			m -= loss
		}
		corrected[i] = Point{T: pt.T, Mass: m, TempC: pt.TempC}
	}
	for i, ok := range blankOK {
		if !ok {
			res.Diags = append(res.Diags, Diagnostic{
				Layer:    LayerBlank,
				Location: fmt.Sprintf("t=%.0f s", pts[i].T),
				Message:  "blank curve has a gap here; blank correction left unknown, value kept uncorrected",
			})
		}
	}
	tg := Curve{XUnit: "s", YUnit: "mg", Label: "TG corrected"}
	for _, c := range corrected {
		tg.Points = append(tg.Points, XYPoint{X: c.T, Y: c.Mass})
	}
	res.TG = tg

	// 2. non-monotonic noise diagnostics (kept, not smoothed away).
	for i := 1; i < len(corrected); i++ {
		if corrected[i].Mass > corrected[i-1].Mass+p.NonMonotonicTolMg {
			res.Diags = append(res.Diags, Diagnostic{
				Layer:    LayerSamplePoints,
				Location: fmt.Sprintf("t=%.0f s", corrected[i].T),
				Message:  fmt.Sprintf("non-monotonic mass increase of %.4f mg (noise); kept as measured", corrected[i].Mass-corrected[i-1].Mass),
			})
		}
	}

	// 3. DTG per segment (central differences, never across gaps).
	dtg := Curve{XUnit: "s", YUnit: "mg/s", Label: "DTG"}
	dtgVals := make([]float64, len(corrected))
	dtgOK := make([]bool, len(corrected))
	for _, s := range segs {
		for i := s[0]; i <= s[1]; i++ {
			var lo, hi int
			switch {
			case i == s[0] && s[1] > s[0]:
				lo, hi = i, i+1
			case i == s[1] && s[1] > s[0]:
				lo, hi = i-1, i
			case i > s[0] && i < s[1]:
				lo, hi = i-1, i+1
			default:
				continue
			}
			dt := corrected[hi].T - corrected[lo].T
			if dt <= 0 {
				continue
			}
			v := (corrected[hi].Mass - corrected[lo].Mass) / dt
			dtgVals[i] = v
			dtgOK[i] = true
			dtg.Points = append(dtg.Points, XYPoint{X: corrected[i].T, Y: v})
		}
	}
	res.DTG = dtg

	// 4. atmosphere timeline with delay / lag.
	res.Atmosphere = atmosphereTimeline(in.GasEvents, p)

	// 5. stage candidates from DTG peaks; overlaps allowed.
	res.Candidates = findCandidates(corrected, dtgVals, dtgOK, segs, res.Atmosphere, in.Furnace, p)

	// 6. residue from the last point of the last segment.
	last := corrected[segs[len(segs)-1][1]]
	r := &Residue{
		FinalMassMg:      last.Mass,
		InitialMassMg:    in.InitialMass.ValueMg,
		InitialMassBasis: in.InitialMass.Basis,
		Pct:              100 * last.Mass / in.InitialMass.ValueMg,
		Note:             "stages may overlap and are not forced to sum to 100%",
	}
	var sum float64
	for _, c := range res.Candidates {
		sum += c.MassLossMg
	}
	r.StageSumPct = 100 * sum / in.InitialMass.ValueMg
	res.Residue = r
	return res
}

// findCandidates detects DTG peaks and emits stage candidates. Where
// peaks overlap, both the individual and the merged hypotheses are kept
// as candidates because the boundary cannot be uniquely decided.
func findCandidates(pts []Point, dtg []float64, dtgOK []bool, segs [][2]int, atmo []AtmoInterval, furn []FurnSeg, p ParamSet) []StageCandidate {
	type peak struct {
		idx int
		t   float64
		val float64
		seg int
		t0i int
		t1i int
	}
	var peaks []peak
	for si, s := range segs {
		for i := s[0] + 1; i < s[1]; i++ {
			if !dtgOK[i] || !dtgOK[i-1] || !dtgOK[i+1] {
				continue
			}
			if dtg[i] <= dtg[i-1] && dtg[i] < dtg[i+1] && dtg[i] < -p.PeakProminenceMgS {
				peaks = append(peaks, peak{idx: i, t: pts[i].T, val: dtg[i], seg: si})
			}
		}
	}
	// window edges: expand while |dm/dt| stays above EdgeFraction*peak.
	edge := func(pk *peak) {
		th := math.Abs(pk.val) * p.EdgeFraction
		s := segs[pk.seg]
		lo := pk.idx
		for lo > s[0] && dtgOK[lo-1] && math.Abs(dtg[lo-1]) > th && dtg[lo-1] < 0 {
			lo--
		}
		hi := pk.idx
		for hi < s[1] && dtgOK[hi+1] && math.Abs(dtg[hi+1]) > th && dtg[hi+1] < 0 {
			hi++
		}
		pk.t0i, pk.t1i = lo, hi
	}
	for i := range peaks {
		edge(&peaks[i])
	}
	var out []StageCandidate
	n := 0
	mkCand := func(t0i, t1i, pki, seg int, ambiguous bool, note string) StageCandidate {
		n++
		loss := pts[t0i].Mass - pts[t1i].Mass
		return StageCandidate{
			Key:        fmt.Sprintf("S%d", n),
			Segment:    seg,
			T0:         pts[t0i].T,
			T1:         pts[t1i].T,
			Temp0C:     TempAt(furn, pts[t0i].T),
			Temp1C:     TempAt(furn, pts[t1i].T),
			MassLossMg: loss,
			PeakT:      pts[pki].T,
			PeakDTGMgS: dtg[pki],
			Atmosphere: gasAt(atmo, pts[pki].T),
			Ambiguous:  ambiguous,
			Note:       note,
		}
	}
	used := make([]bool, len(peaks))
	for i := 0; i < len(peaks); i++ {
		// overlap with the next peak in the same segment?
		if i+1 < len(peaks) && peaks[i+1].seg == peaks[i].seg && peaks[i+1].t0i <= peaks[i].t1i {
			j := i
			for j+1 < len(peaks) && peaks[j+1].seg == peaks[j].seg && peaks[j+1].t0i <= peaks[j].t1i {
				j++
			}
			// individual candidates
			for k := i; k <= j; k++ {
				if math.Abs(dtg[peaks[k].idx]) < p.PeakProminenceMgS {
					continue
				}
				c := mkCand(peaks[k].t0i, peaks[k].t1i, peaks[k].idx, peaks[k].seg, true,
					"overlapping peaks: individual boundary hypothesis, not uniquely decidable")
				if c.MassLossMg >= p.MinStageMassMg {
					out = append(out, c)
					used[k] = true
				}
			}
			// merged hypothesis
			c := mkCand(peaks[i].t0i, peaks[j].t1i, peaks[i].idx, peaks[i].seg, true,
				"overlapping peaks: merged hypothesis kept because split is not uniquely decidable")
			if c.MassLossMg >= p.MinStageMassMg {
				out = append(out, c)
			}
			i = j
			continue
		}
		if used[i] {
			continue
		}
		c := mkCand(peaks[i].t0i, peaks[i].t1i, peaks[i].idx, peaks[i].seg, false, "")
		if c.MassLossMg >= p.MinStageMassMg {
			out = append(out, c)
		}
	}
	return out
}
