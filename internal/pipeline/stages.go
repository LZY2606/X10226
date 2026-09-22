package pipeline

import "tgaaudit/internal/domain"

// buildStages turns every peak of every hypothesis into a stage candidate.
// Stages may overlap, their losses are never forced to sum to 100%, and any
// stage whose interval touches a gap is retained as "unknown" rather than
// interpolated away.
func (d *Derivation) buildStages(p domain.Params, switches []gasSwitch) {
	pts := d.Corrected.Points
	tempAt := func(t float64) *float64 {
		// nearest same-region point; only from actual sample measurements
		best := -1
		bestDt := 1e18
		for i := range pts {
			if pts[i].Missing {
				continue
			}
			dt := absF(pts[i].TS - t)
			if dt < bestDt {
				bestDt, best = dt, i
			}
		}
		if best < 0 {
			return nil
		}
		if pts[best].SampleTempK != nil {
			v := *pts[best].SampleTempK
			return &v
		}
		if pts[best].FurnaceTempK != nil {
			v := *pts[best].FurnaceTempK
			return &v
		}
		return nil
	}

	for _, set := range d.PeakSets {
		stages := make([]Stage, 0, len(set.Peaks))
		for k, pk := range set.Peaks {
			st := Stage{Hypothesis: set.Hypothesis, Index: k + 1, PeakIndex: pk.Index,
				OnsetT: pk.LeftT, EndT: pk.RightT, PeakT: pk.TS,
				OnsetTempK: tempAt(pk.LeftT), EndTempK: tempAt(pk.RightT)}
			for gi := range d.Gaps {
				g := d.Gaps[gi]
				if intervalsTouch(st.OnsetT, st.EndT, g.FromT, g.ToT) {
					gp := d.Gaps[gi]
					st.CrossesGap = &gp
					break
				}
			}
			frac := massFractionBetween(pts, st.OnsetT, st.EndT)
			if frac == nil {
				st.Status = domain.StatusUnknown
				st.RejectReasons = append(st.RejectReasons,
					"mass loss cannot be evaluated without crossing a data gap")
			} else {
				f := *frac
				st.MassLossFrac = &f
				pct := f * 100
				st.MassLossPct = &pct
				st.Status = domain.StatusRetained
			}
			if st.CrossesGap != nil {
				st.Status = domain.StatusUnknown
			}
			stages = append(stages, st)
		}
		for i := range stages {
			for j := range stages {
				if i != j && intervalsOverlap(stages[i].OnsetT, stages[i].EndT,
					stages[j].OnsetT, stages[j].EndT, p.OverlapTolS) {
					stages[i].OverlapsWith = append(stages[i].OverlapsWith, stages[j].Index)
				}
			}
		}
		d.Stages[set.Hypothesis] = stages
	}

	// Reaction windows: valve switch time + transport delay, widened by the
	// thermal-lag uncertainty. Observed peak time is independent evidence.
	var rx []ReactionWindow
	for _, set := range d.PeakSets {
		stages := d.Stages[set.Hypothesis]
		for si, st := range stages {
			for _, sw := range switches {
				lo := sw.t + p.AtmosphereDelayS
				hi := lo + p.LagTauS
				r := ReactionWindow{
					StageKey: stageKey(set.Hypothesis, st.Index),
					StageHyp: set.Hypothesis, StageIndex: st.Index,
					ValveT: sw.t, GasFrom: sw.from, GasTo: sw.to,
					DelayS: p.AtmosphereDelayS, LagTauS: p.LagTauS,
					ReactionTLo: lo, ReactionTHi: hi, ObservedPeakT: st.PeakT,
				}
				r.PeakMatches = st.PeakT >= lo-p.OverlapTolS && st.PeakT <= hi+p.OverlapTolS
				_ = si
				rx = append(rx, r)
			}
		}
	}
	d.Reactions = rx
}

func stageKey(hyp string, idx int) string {
	return "stage-" + hyp + "-" + itoa(idx)
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func intervalsTouch(a, b, c, d float64) bool {
	return b > c && a < d
}
func intervalsOverlap(a, b, c, d, tol float64) bool {
	return b > c+tol && a < d-tol
}

// massFractionBetween returns f(onset)-f(end) only when both endpoints can be
// anchored to real measured points in the same segment. Otherwise nil.
func massFractionBetween(pts []Point, onset, end float64) *float64 {
	anchor := func(t float64, pickRight bool) *float64 {
		var best *Point
		bestDt := 1e18
		for i := range pts {
			if pts[i].Missing || pts[i].MassFraction == nil {
				continue
			}
			if pickRight && pts[i].TS < t {
				continue
			}
			if !pickRight && pts[i].TS > t {
				continue
			}
			dt := absF(pts[i].TS - t)
			if dt < bestDt {
				bestDt = dt
				q := pts[i]
				best = &q
			}
		}
		if best == nil {
			return nil
		}
		v := *best.MassFraction
		return &v
	}
	a := anchor(onset, false)
	b := anchor(end, true)
	if a == nil || b == nil {
		return nil
	}
	f := *a - *b
	return &f
}
