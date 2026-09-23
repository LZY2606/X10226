package compute

import (
	"math"
	"sort"

	"tga-archive/internal/domain"
)

func (e *engine) buildDTGStages() {
	// DTG per segment using simple adjacent differences; boxcar smoothing
	// limited to a window so it never crosses a gap.
	for segIdx := 0; segIdx <= maxSeg(e.curve); segIdx++ {
		var seg []CurvePoint
		for _, c := range e.curve {
			if c.Segment == segIdx {
				seg = append(seg, c)
			}
		}
		if len(seg) < 2 {
			continue
		}
		raw := make([]float64, len(seg))
		for i := 1; i < len(seg); i++ {
			raw[i] = (seg[i].MCorr - seg[i-1].MCorr) / (seg[i].T - seg[i-1].T)
		}
		raw[0] = raw[1]
		// boxcar smoothing inside the time window
		w := e.p.SmoothWindowS
		smoothed := make([]float64, len(seg))
		for i := range seg {
			var sum, n float64
			for j := range seg {
				if math.Abs(seg[j].T-seg[i].T) <= w/2 {
					sum += raw[j]
					n++
				}
			}
			smoothed[i] = sum / n
			d := DTGPoint{T: seg[i].T, Rate: smoothed[i], Segment: segIdx}
			d.Node = e.node("dtg", "DTG @"+strconvF(seg[i].T)+"s",
				"smoothed dm/dt within segment", smoothed[i], UnitMass+"/"+UnitTime,
				"ok", []Ref{{Kind: "node", ID: seg[i].Node.ID, Role: "tg"}})
			e.dtg = append(e.dtg, d)
		}
		_ = smoothed

		// peak detection: local minima with sufficient prominence
		var segPeaks []Peak
		for i := 1; i < len(seg)-1; i++ {
			if smoothed[i] < smoothed[i-1] && smoothed[i] < smoothed[i+1] {
				prom := math.Min(smoothed[i-1], smoothed[i+1]) - smoothed[i]
				if prom >= e.p.PeakMinProminence {
					pk := Peak{T: seg[i].T, Rate: smoothed[i], Segment: segIdx, Prominence: prom}
					pk.Node = e.node("peak", "DTG peak @"+strconvF(seg[i].T)+"s",
						"local min with prominence above threshold", smoothed[i],
						UnitMass+"/"+UnitTime, "ok",
						[]Ref{{Kind: "node", ID: seg[i].Node.ID, Role: "dtg"}})
					segPeaks = append(segPeaks, pk)
				}
			}
		}
		// reject micro-peaks below threshold as first-class rejected data
		for i := 1; i < len(seg)-1; i++ {
			if smoothed[i] < smoothed[i-1] && smoothed[i] < smoothed[i+1] {
				prom := math.Min(smoothed[i-1], smoothed[i+1]) - smoothed[i]
				if prom < e.p.PeakMinProminence {
					e.judge = append(e.judge, Judgment{
						Code: "micropeak_rejected", Rule: "peak:prominence-threshold",
						Message: "DTG local min at " + strconvF(seg[i].T) + "s kept as rejected micro-peak",
						Detail:  map[string]float64{"prominence": prom, "threshold": e.p.PeakMinProminence},
					})
				}
			}
		}
		e.peaks = append(e.peaks, segPeaks...)

		// stages: each prominent peak owns a stage between two rate-zero
		// crossings in its neighborhood; overlapping peaks share a group.
		groups := groupPeaks(segPeaks, seg, smoothed, e.p.PeakMinProminence)
		for gi, g := range groups {
			if len(g) > 1 {
				e.emitOverlapStages(g, seg, smoothed, segIdx, gi)
			} else if len(g) == 1 {
				e.emitSingleStage(g[0], seg, smoothed, segIdx, gi)
			}
		}
	}
	sort.Slice(e.peaks, func(i, j int) bool { return e.peaks[i].T < e.peaks[j].T })
}

func maxSeg(curve []CurvePoint) int {
	m := 0
	for _, c := range curve {
		if c.Segment > m {
			m = c.Segment
		}
	}
	return m
}

type pgroup []Peak

// groupPeaks clusters peaks whose rate-zero basins overlap.
func groupPeaks(peaks []Peak, seg []CurvePoint, sm []float64, minProm float64) []pgroup {
	var groups []pgroup
	used := make([]bool, len(peaks))
	for i, pk := range peaks {
		if used[i] {
			continue
		}
		gr := pgroup{pk}
		used[i] = true
		b0, b1 := basin(seg, sm, pk.T)
		for j := i + 1; j < len(peaks); j++ {
			if used[j] {
				continue
			}
			c0, c1 := basin(seg, sm, peaks[j].T)
			if c0 < b1 && c1 > b0 {
				gr = append(gr, peaks[j])
				used[j] = true
				b0 = math.Min(b0, c0)
				b1 = math.Max(b1, c1)
			}
		}
		groups = append(groups, gr)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i][0].T < groups[j][0].T })
	return groups
}

func basin(seg []CurvePoint, sm []float64, peakT float64) (float64, float64) {
	idx := 0
	for i, c := range seg {
		if c.T == peakT {
			idx = i
		}
	}
	i0, i1 := idx, idx
	for i0 > 0 && sm[i0-1] < 0 {
		i0--
	}
	for i1 < len(sm)-1 && sm[i1+1] < 0 {
		i1++
	}
	return seg[i0].T, seg[min(i1+1, len(seg)-1)].T
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func (e *engine) stageBase(t0, t1 float64, seg []CurvePoint, sources []Ref, group string) StageCandidate {
	st := StageCandidate{Group: group, TStart: t0, TEnd: t1, Sources: sources}
	gas, gid := e.gasAt((t0 + t1) / 2)
	st.Gas = gas
	if gid != "" {
		st.Sources = append(st.Sources, Ref{Kind: "raw", ID: gid, Role: "gas"})
	}
	m0 := e.lookupM0()
	gap := e.gapWithin(t0, t1)
	mA, okA := massAt(t0, seg)
	mB, okB := massAt(t1, seg)
	switch {
	case gap:
		st.Status = "gap_uncertain"
		st.DeltaRange = [2]float64{0, math.Abs(mA - mB)}
		st.PercentUnknown = true
		st.Rejects = []string{"unique allocation across gap (gap is never interpolated)"}
	case okA && okB:
		st.DeltaMMg = mA - mB
		st.Status = "ok"
		if m0 != nil {
			st.PercentOfM0 = 100 * st.DeltaMMg / m0.Num
		} else {
			st.PercentUnknown = true
		}
	default:
		st.Status = "gap_uncertain"
		st.PercentUnknown = true
	}
	if peakT := (t0 + t1) / 2; peakT > 0 {
		st.PeakT = peakT
	}
	idExtra := st.Status + gas + strconvF(st.DeltaMMg)
	st.ID = e.stageID(group, t0, t1, st.PeakT, gas, st.Sources, idExtra)
	n := e.node("stage", "stage ["+strconvF(t0)+","+strconvF(t1)+")s",
		"delta_m = m(t0)-m(t1); stages may overlap; percentages need not sum to 100",
		st.DeltaMMg, UnitMass, st.Status, st.Sources)
	st.Node = n
	return st
}

func (e *engine) emitSingleStage(pk Peak, seg []CurvePoint, sm []float64, segIdx, gi int) {
	t0, t1 := basin(seg, sm, pk.T)
	sources := []Ref{{Kind: "node", ID: pk.Node.ID, Role: "peak"}}
	st := e.stageBase(t0, t1, seg, sources, "g"+itoa(gi)+".merged")
	st.PeakT = pk.T
	e.stages = append(e.stages, st)
}

func (e *engine) emitOverlapStages(g pgroup, seg []CurvePoint, sm []float64, segIdx, gi int) {
	// Legal alternative set: keep BOTH the merged envelope and each split
	// component. Nothing forces the split allocations to sum to the merge.
	tMin, tMax := 1e30, -1e30
	var peakSrc []Ref
	for _, pk := range g {
		c0, c1 := basin(seg, sm, pk.T)
		tMin = math.Min(tMin, c0)
		tMax = math.Max(tMax, c1)
		peakSrc = append(peakSrc, Ref{Kind: "node", ID: pk.Node.ID, Role: "peak"})
	}
	switch e.p.OverlapOption {
	case "merged":
		st := e.stageBase(tMin, tMax, seg, peakSrc, "g"+itoa(gi))
		st.Status = mergeStatus(st.Status, "overlap_merged")
		e.stages = append(e.stages, st)
	case "split":
		for _, pk := range g {
			c0, c1 := basin(seg, sm, pk.T)
			st := e.stageBase(c0, c1, seg,
				[]Ref{{Kind: "node", ID: pk.Node.ID, Role: "peak"}}, "g"+itoa(gi))
			st.PeakT = pk.T
			st.Status = mergeStatus(st.Status, "overlap_split")
			e.stages = append(e.stages, st)
		}
	default: // "both": every legal candidate is retained
		merged := e.stageBase(tMin, tMax, seg, peakSrc, "g"+itoa(gi)+".merged")
		merged.Status = mergeStatus(merged.Status, "overlap_alternative")
		e.stages = append(e.stages, merged)
		for k, pk := range g {
			c0, c1 := basin(seg, sm, pk.T)
			st := e.stageBase(c0, c1, seg,
				[]Ref{{Kind: "node", ID: pk.Node.ID, Role: "peak"}},
				"g"+itoa(gi)+".split"+itoa(k))
			st.PeakT = pk.T
			st.Status = mergeStatus(st.Status, "overlap_alternative")
			e.stages = append(e.stages, st)
		}
		e.judge = append(e.judge, Judgment{
			Code: "overlapping_peaks", Rule: "stage:retain-all-legal-candidates",
			Message: "overlapping DTG peaks detected; merged and split stages are all retained, overlap allowed, percentages not forced to 100%",
			Refs:    peakSrc,
			Detail:  map[string]float64{"t0": tMin, "t1": tMax, "peaks": float64(len(g))},
		})
	}
}

func mergeStatus(a, b string) string {
	if a == "gap_uncertain" {
		return a
	}
	return b
}

func itoa(i int) string {
	return strconvI(i)
}

func (e *engine) gapWithin(t0, t1 float64) bool {
	for _, g := range e.gaps {
		if g.TBefore >= t0 && g.TAfter <= t1 {
			return true
		}
	}
	return false
}

func massAt(t float64, seg []CurvePoint) (float64, bool) {
	for _, c := range seg {
		if c.T == t {
			return c.MCorr, true
		}
	}
	return 0, false
}

func (e *engine) lookupM0() *domain.Value {
	// newest accepted mass0 row wins within the current state (revisions are
	// new strata rows; parent versions remain in history).
	var best Row
	bestSeq := -1
	for _, r := range e.mass0 {
		if r.Order > bestSeq {
			best, bestSeq = r, r.Order
		}
	}
	if v, ok := best.Fields["m0"]; ok && v.Bad == "" {
		return &v
	}
	return nil
}
