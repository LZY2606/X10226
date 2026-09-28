package engine

import (
	"fmt"
	"sort"

	"tga/internal/domain"
)

// buildStages turns the DTG trace, effective atmosphere windows and the
// furnace program into stage candidates. Candidates may overlap, losses are
// never renormalized to sum to 100%, and ambiguous regions keep every legal
// alternative rather than a single forced answer.
func buildStages(series *domain.SeriesResult, dtg *domain.DTGResult, gasSrc, progSrc *domain.Source, initSrc *domain.Source, p domain.Params) *domain.StagesResult {
	res := &domain.StagesResult{}
	res.GasTimeline = effectiveGasWindows(gasSrc, p)
	res.ProgramCurve = programCurve(progSrc, p)

	pts := series.Points
	// Active-loss intervals: contiguous index spans where DTG is negative,
	// never crossing a gap and never spanning a zero crossing.
	type span struct{ lo, hi int }
	var spans []span
	i := 0
	for i < len(dtg.Points) {
		d := dtg.Points[i]
		if d.InGap || d.DTGmgPerMin > -p.MinProminence || pts[i].MassMg == nil {
			i++
			continue
		}
		lo := i
		for i < len(dtg.Points) && !dtg.Points[i].InGap &&
			dtg.Points[i].DTGmgPerMin <= -p.MinProminence && pts[i].MassMg != nil {
			i++
		}
		hi := i - 1
		if hi > lo {
			spans = append(spans, span{lo, hi})
		}
	}

	var kept []domain.Stage
	stageOrd := 0
	for _, spn := range spans {
		// Peaks inside this negative span. A single peak is one stage; multiple
		// unresolved minima are overlapping lobes.
		var peakIdx []int
		for k := spn.lo; k <= spn.hi; k++ {
			for _, pk := range dtg.Peaks {
				if pk.Index == k {
					peakIdx = append(peakIdx, k)
				}
			}
		}
		if len(peakIdx) == 0 {
			peakIdx = []int{strongestPeakIn(dtg, spn.lo, spn.hi)}
		}

		if len(peakIdx) == 1 {
			stageOrd++
			st := stageFromSpan(pts, dtg, spn.lo, spn.hi, peakIdx[0],
				res.GasTimeline, stageOrd, p, initialMg(initSrc))
			if rejectBelowMin(&st, p) {
				res.Stages = append(res.Stages, st)
				stageOrd--
				continue
			}
			kept = append(kept, st)
			continue
		}

		// Multiple lobes: split at the local maximum (least-negative point)
		// between consecutive peaks, keep each lobe and one merged candidate.
		bounds := []int{spn.lo}
		for q := 0; q+1 < len(peakIdx); q++ {
			bounds = append(bounds, valleyBetween(dtg, peakIdx[q], peakIdx[q+1]))
		}
		bounds = append(bounds, spn.hi)
		var lobes []domain.Stage
		for q := 0; q < len(peakIdx); q++ {
			stageOrd++
			st := stageFromSpan(pts, dtg, bounds[q], bounds[q+1], peakIdx[q],
				res.GasTimeline, stageOrd, p, initialMg(initSrc))
			if st.LossPct >= 0 && st.LossPct < p.MinStageLossPct {
				st.RejectReason = fmt.Sprintf("loss %.2f%% below minimum stage loss %.2f%%", st.LossPct, p.MinStageLossPct)
				res.Stages = append(res.Stages, st)
				stageOrd--
				continue
			}
			lobes = append(lobes, st)
		}
		stageOrd++
		merged := stageFromSpan(pts, dtg, spn.lo, spn.hi, strongestPeakIn(dtg, spn.lo, spn.hi),
			res.GasTimeline, stageOrd, p, initialMg(initSrc))
		merged.Label = fmt.Sprintf("Stage %d-%d merged", lobes[0].Number, lobes[len(lobes)-1].Number)
		for li := range lobes {
			lobes[li].Ambiguous = true
			lobes[li].Overlaps = append(lobes[li].Overlaps, merged.ID)
			merged.Overlaps = append(merged.Overlaps, lobes[li].ID)
			addLobeAlternatives(&lobes[li], &merged)
		}
		merged.Ambiguous = true
		kept = append(kept, merged)
		kept = append(kept, lobes...)
	}

	// Record pairwise overlap between any time-overlapping retained stages.
	for a := 0; a < len(kept); a++ {
		for b := a + 1; b < len(kept); b++ {
			if kept[a].EndS > kept[b].StartS && kept[b].EndS > kept[a].StartS {
				kept[a].Overlaps = appendUnique(kept[a].Overlaps, kept[b].ID)
				kept[b].Overlaps = appendUnique(kept[b].Overlaps, kept[a].ID)
			}
		}
	}
	res.Stages = append(res.Stages, kept...)

	sort.SliceStable(res.Stages, func(a, b int) bool { return res.Stages[a].StartS < res.Stages[b].StartS })
	res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
		Code: "stages_overlap_allowed", Severity: "info",
		Message: "stage losses are independent integrals; stages may overlap and are not forced to total 100%"})
	return res
}

func stageFromSpan(pts []domain.SeriesPoint, dtg *domain.DTGResult, lo, hi, peakIdx int, gas []domain.GasWindow, ord int, p domain.Params, initMg float64) domain.Stage {
	startS, endS := pts[lo].TimeS, pts[hi].TimeS
	startM, endM := *pts[lo].MassMg, *pts[hi].MassMg
	st := domain.Stage{
		ID:          domain.StableID("stage", []any{"v1", startS, endS, round4(startM), round4(endM)}),
		Label:       fmt.Sprintf("Stage %d", ord),
		Number:      ord,
		StartS:      startS, EndS: endS,
		StartMassMg: startM, EndMassMg: endM,
		LossMg:      startM - endM,
		StartTempC:  tempAt(pts, lo),
		EndTempC:    tempAt(pts, hi),
	}
	if initMg > 0 {
		st.LossPct = round4((startM - endM) / initMg * 100)
	} else {
		st.LossPct = -1 // percentage not determinable without recorded initial mass
	}
	st.PeakS = pts[peakIdx].TimeS
	st.PeakMgPerMin = round4(dtg.Points[peakIdx].DTGmgPerMin)
	st.PeakIndices = []int{pts[peakIdx].Index}
	st.TempAtPeakC = tempAt(pts, peakIdx)
	st.GasAtStart, st.GasAtEnd = gasAt(gas, startS), gasAt(gas, endS)
	if v, vs := valveEventNear(gas, st.PeakS); v >= 0 {
		vv := v
		st.ValveTimeS = &vv
		ev := vs
		st.EffectiveGasS = &ev
	}
	return st
}

func strongestPeakIn(dtg *domain.DTGResult, lo, hi int) int {
	best := lo
	for k := lo; k <= hi; k++ {
		if dtg.Points[k].InGap {
			continue
		}
		if dtg.Points[k].DTGmgPerMin < dtg.Points[best].DTGmgPerMin {
			best = k
		}
	}
	return best
}

func valleyBetween(dtg *domain.DTGResult, a, b int) int {
	v := a + 1
	for k := a + 1; k < b; k++ {
		if dtg.Points[k].DTGmgPerMin > dtg.Points[v].DTGmgPerMin {
			v = k
		}
	}
	return v
}

func rejectBelowMin(st *domain.Stage, p domain.Params) bool {
	return st.LossPct >= 0 && st.LossPct < p.MinStageLossPct
}

func appendUnique(xs []string, s string) []string {
	for _, x := range xs {
		if x == s {
			return xs
		}
	}
	return append(xs, s)
}

func addLobeAlternatives(lobe, merged *domain.Stage) {
	alt := func(label string, loS, hiS, startM, endM float64, reason string) domain.StageAlternative {
		loss := 0.0
		if startM != 0 {
			loss = round4((startM - endM) / startM * 100)
		}
		return domain.StageAlternative{
			ID: domain.StableID("alt", []any{"v1", label, loS, hiS}),
			Label: label, StartS: loS, EndS: hiS, LossPct: loss, Reason: reason,
		}
	}
	lobe.Alternatives = append(lobe.Alternatives,
		alt(lobe.Label+" lobe only", lobe.StartS, lobe.EndS, lobe.StartMassMg, lobe.EndMassMg, "resolve overlap as a single lobe"),
		alt(merged.Label, merged.StartS, merged.EndS, merged.StartMassMg, merged.EndMassMg, "merge unresolved overlapping peaks into one stage"),
	)
}

// effectiveGasWindows shifts valve events onto the sample reaction clock.
// Valve switch time != sample reaction time because of atmosphere delay.
func effectiveGasWindows(gasSrc *domain.Source, p domain.Params) []domain.GasWindow {
	if gasSrc == nil {
		return nil
	}
	evs := gasSrc.Gas
	sort.SliceStable(evs, func(a, b int) bool { return evs[a].ValveTimeS < evs[b].ValveTimeS })
	var wins []domain.GasWindow
	for i, ev := range evs {
		from := ev.ValveTimeS + p.GasDelayS
		to := 1e30
		if i+1 < len(evs) {
			to = evs[i+1].ValveTimeS + p.GasDelayS
		}
		wins = append(wins, domain.GasWindow{
			FromS: round4(from), ToS: to, Gas: ev.ToGas, ValveFromS: round4(ev.ValveTimeS),
		})
	}
	return wins
}

// programCurve returns the furnace envelope and the lagged expected sample temp.
func programCurve(progSrc *domain.Source, p domain.Params) []domain.ProgramPoint {
	if progSrc == nil {
		return nil
	}
	var out []domain.ProgramPoint
	for _, st := range progSrc.Program {
		steps := 4
		for k := 0; k <= steps; k++ {
			fr := float64(k) / float64(steps)
			t := st.FromTime + (st.ToTime-st.FromTime)*fr
			temp := st.StartTempC + (st.TargetTempC-st.StartTempC)*fr
			out = append(out, domain.ProgramPoint{
				TimeS: round4(t), ProgramTempC: round4(temp),
				ExpectedSampleC: round4(temp - p.TempLagC),
			})
		}
	}
	return out
}

func gasAt(wins []domain.GasWindow, t float64) string {
	cur := "?"
	for _, w := range wins {
		if t >= w.FromS && t < w.ToS {
			cur = w.Gas
		}
	}
	return cur
}

// valveEventNear returns (valve time, effective time) for the window active at t.
func valveEventNear(wins []domain.GasWindow, t float64) (float64, float64) {
	for _, w := range wins {
		if t >= w.FromS && (w.ToS == 1e30 || t < w.ToS) {
			return w.ValveFromS, w.FromS
		}
	}
	return -1, 0
}

func tempAt(pts []domain.SeriesPoint, idx int) float64 {
	if pts[idx].TempC != nil {
		return round4(*pts[idx].TempC)
	}
	return 0
}

func round4(f float64) float64 { return roundToP(f, 4) }

func roundToP(f float64, digits int) float64 {
	return domain.Round(domain.MustConvert(f, domain.KindDimensionless), digits).Value
}
