package compute

import (
	"fmt"
)

// buildStages turns each viable DTG peak candidate into an interval candidate.
// Ambiguous (overlapping) peaks each yield a stage; intervals are allowed to
// overlap and the sum of stage losses is deliberately not forced to 100%.
func buildStages(p Params, b Bundle, r *Result) []StageCandidate {
	var stages []StageCandidate
	segByID := map[string]*Segment{}
	for i := range r.Segments {
		segByID[r.Segments[i].ID] = &r.Segments[i]
	}
	basis := r.InitialMassMG
	for _, pk := range r.Peaks {
		if pk.Status == "rejected" {
			continue
		}
		seg := segByID[pk.SegmentID]
		if seg == nil {
			continue
		}
		pi := indexAtT(seg, pk.AtT)
		if pi < 0 {
			continue
		}
		li, ri := shoulders(seg, pi, p)
		startT := seg.Points[li].T - p.StageFootExtension
		endT := seg.Points[ri].T + p.StageFootExtension
		if startT < seg.TStart {
			startT = seg.TStart
		}
		if endT > seg.TEnd {
			endT = seg.TEnd
		}
		ms := massAt(seg, startT)
		me := massAt(seg, endT)
		if basis <= 0 {
			basis = seg.Points[0].MassCorrect
		}
		loss := 0.0
		if basis > 0 {
			loss = (ms - me) / basis * 100
		}
		gs, gws := gasAt(r.Atmospheres, startT)
		ge, gwe := gasAt(r.Atmospheres, endT)
		st := StageCandidate{
			SegmentID: seg.ID,
			StartT:    round(startT, 6), EndT: round(endT, 6),
			StartTk: round(tempAt(seg, startT), 4), EndTk: round(tempAt(seg, endT), 4),
			MassStart: round(ms, 6), MassEnd: round(me, 6),
			LossPct:    round(loss, 4),
			PeakID:     pk.ID,
			GasAtStart: gs, GasAtEnd: ge,
			Status:  pk.Status,
			Sources: appendSource(r, seg.ID, pk.ID, gws, gwe),
		}
		st.ID = ID("stage", seg.ID, pk.ID, ftoa(st.StartT), ftoa(st.EndT))
		st.Chain = []ChainStep{
			{Order: 1, Op: "locate_peak", Detail: fmt.Sprintf("DTG minimum %g %%/min at t=%g min", pk.DTG, round(pk.AtT, 4)), Ref: pk.ID, Value: pk.DTG, Unit: "%/min"},
			{Order: 2, Op: "shoulders", Detail: fmt.Sprintf("foot indices %d..%d within segment, extension %g min", li, ri, p.StageFootExtension)},
			{Order: 3, Op: "interval", Detail: fmt.Sprintf("[%g, %g] min; gases at reaction time: %s -> %s", st.StartT, st.EndT, orUnknown(gs), orUnknown(ge)), Value: st.StartT, Unit: "min"},
			{Order: 4, Op: "mass_delta", Detail: fmt.Sprintf("corrected mass %g - %g = %g mg", ms, me, ms-me), Value: round(ms-me, 6), Unit: "mg"},
			{Order: 5, Op: "percent", Detail: fmt.Sprintf("delta / initial(%g mg) * 100; stages need not sum to 100%%", basis), Value: st.LossPct, Unit: "%"},
		}
		stages = append(stages, st)
	}
	for i := range stages {
		for j := range stages {
			if i == j || stages[i].SegmentID != stages[j].SegmentID {
				continue
			}
			if stages[i].StartT < stages[j].EndT && stages[j].StartT < stages[i].EndT {
				stages[i].OverlapsWith = append(stages[i].OverlapsWith, stages[j].ID)
			}
		}
	}
	return stages
}

func orUnknown(s string) string {
	if s == "" {
		return "unknown"
	}
	return s
}

// shoulders walks outward while DTG is still a loss (negative) and returns the
// indices where the rate first approaches zero on each side.
func shoulders(seg *Segment, pi int, _ Params) (int, int) {
	li, ri := pi, pi
	for li > 0 && seg.Points[li-1].DTG < -1e-9 {
		li--
	}
	for ri < len(seg.Points)-1 && seg.Points[ri+1].DTG < -1e-9 {
		ri++
	}
	return li, ri
}

func indexAtT(seg *Segment, t float64) int {
	for i := range seg.Points {
		if absFloat(seg.Points[i].T-t) < 1e-9 {
			return i
		}
	}
	// nearest within segment-local spacing tolerance
	best, bestD := -1, 1e18
	for i := range seg.Points {
		if d := absFloat(seg.Points[i].T - t); d < bestD {
			best, bestD = i, d
		}
	}
	if bestD < 1e-6 {
		return best
	}
	return -1
}

func absFloat(v float64) float64 {
	if v < 0 {
		return -v
	}
	return v
}

// massAt interpolates only within the segment; the caller guarantees t lies in
// [TStart,TEnd], so no gap is crossed.
func massAt(seg *Segment, t float64) float64 {
	xs := make([]float64, len(seg.Points))
	ys := make([]float64, len(seg.Points))
	for i, pt := range seg.Points {
		xs[i], ys[i] = pt.T, pt.MassCorrect
	}
	return interp1(xs, ys, t)
}

func tempAt(seg *Segment, t float64) float64 {
	xs := make([]float64, len(seg.Points))
	ys := make([]float64, len(seg.Points))
	for i, pt := range seg.Points {
		xs[i], ys[i] = pt.T, pt.Tk
	}
	return interp1(xs, ys, t)
}
