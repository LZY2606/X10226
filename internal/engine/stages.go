package engine

import (
	"math"

	"tgareview/internal/domain"
	"tgareview/internal/ids"
)

// computeDTG differentiates pct against time (s), converts to %/min and smooths
// with a centered moving average. The smoother never reaches outside the
// interval, so no value is interpolated across a data gap.
func computeDTG(a *Artifact, p Params) {
	for _, iv := range a.Intervals {
		n := iv.ToIndex - iv.FromIndex + 1
		if n < 2 {
			continue
		}
		raw := make([]float64, n)
		for j := 0; j < n; j++ {
			pt := &a.Points[iv.FromIndex+j]
			if j == 0 {
				raw[j] = (a.Points[iv.FromIndex+1].Pct - pt.Pct) /
					(a.Points[iv.FromIndex+1].TimeS - pt.TimeS) * 60.0
			} else if j == n-1 {
				raw[j] = (pt.Pct - a.Points[iv.FromIndex+j-1].Pct) /
					(pt.TimeS - a.Points[iv.FromIndex+j-1].TimeS) * 60.0
			} else {
				dt := a.Points[iv.FromIndex+j+1].TimeS - a.Points[iv.FromIndex+j-1].TimeS
				raw[j] = (a.Points[iv.FromIndex+j+1].Pct - a.Points[iv.FromIndex+j-1].Pct) / dt * 60.0
			}
		}
		half := int(p.DtgWindowS / 2.0)
		times := func(j int) float64 { return a.Points[iv.FromIndex+j].TimeS }
		for j := 0; j < n; j++ {
			t0 := times(j)
			sum, cnt := 0.0, 0
			for k := 0; k < n; k++ {
				if math.Abs(times(k)-t0) <= float64(half)+1e-9 {
					sum += raw[k]
					cnt++
				}
			}
			a.Points[iv.FromIndex+j].DTGPctPerMin = sum / float64(cnt)
		}
	}
}

type peakGroup struct {
	startIdx, endIdx int
	indices          []int
	modes            []int
}

// findStages detects negative DTG excursions per interval, splits multimodal
// (overlapping) groups into all legal options, and emits candidates.
func findStages(a *Artifact, p Params) {
	for _, iv := range a.Intervals {
		groups := detectGroups(a, iv, p)
		for gi, g := range groups {
			groupID := ids.Of("grp", a.ID, itoa(iv.FromIndex)+":"+itoa(g.startIdx)+":"+itoa(g.endIdx))
			basePct := a.Points[g.startIdx].Pct
			endPct := a.Points[g.endIdx].Pct
			totalLoss := basePct - endPct

			merged := newCandidate(a, groupID, "merged", g.startIdx, g.endIdx, dominantMode(g.modes, a), p)
			setLoss(&merged, totalLoss, a, g.startIdx, g.endIdx, basePct)

			if p.SplitOverlap && len(g.modes) > 1 {
				// All legal overlaps: every adjacent mode pair defines a split.
				for si := 0; si+1 < len(g.modes); si++ {
					m1, m2 := g.modes[si], g.modes[si+1]
					saddle := saddleBetween(a, m1, m2)
					ca := newCandidate(a, groupID, "split_a", g.startIdx, saddle, m1, p)
					cb := newCandidate(a, groupID, "split_b", saddle, g.endIdx, m2, p)
					setLoss(&ca, a.Points[g.startIdx].Pct-a.Points[saddle].Pct, a, g.startIdx, saddle, basePct)
					setLoss(&cb, a.Points[saddle].Pct-a.Points[g.endIdx].Pct, a, saddle, g.endIdx, a.Points[saddle].Pct)
					ca.OverlapsWith = []string{cb.ID, merged.ID}
					cb.OverlapsWith = []string{ca.ID, merged.ID}
					merged.OverlapsWith = append(merged.OverlapsWith, ca.ID, cb.ID)
					a.Candidates = append(a.Candidates, ca, cb)
				}
			}
			a.Candidates = append(a.Candidates, merged)
			if p.SplitOverlap && len(g.modes) > 1 {
				a.Judgements = append(a.Judgements, RuleJudgement{
					Code:    "R3",
					Status:  "applies",
					Message: "overlapping peaks detected: all legal merged/split candidates retained; no renormalization to 100%",
					Refs:    []string{"group:" + groupID},
				})
			}
			_ = gi
		}
	}
}

func detectGroups(a *Artifact, iv Interval, p Params) []peakGroup {
	var groups []peakGroup
	var g *peakGroup
	threshold := -p.MinPeakProminencePctPerMin
	inGroup := false
	for i := iv.FromIndex; i <= iv.ToIndex; i++ {
		v := a.Points[i].DTGPctPerMin
		if v < threshold {
			if !inGroup {
				inGroup = true
				start := i
				if i > iv.FromIndex {
					start = i - 1
				}
				g = &peakGroup{startIdx: start, indices: []int{}, endIdx: i}
			}
			g.indices = append(g.indices, i)
			g.endIdx = i
		} else if inGroup {
			g.endIdx = i
			finishGroup(a, g)
			groups = append(groups, *g)
			inGroup = false
		}
	}
	if inGroup {
		if g.endIdx < iv.ToIndex {
			g.endIdx++
		}
		finishGroup(a, g)
		groups = append(groups, *g)
	}
	return groups
}

// finishGroup finds significant local modes (most-negative DTG points). A
// secondary mode counts when its depth is at least 35% of the deepest mode and
// it is separated by a distinct saddle (a clear local minimum between them);
// otherwise it is treated as a shoulder of a single (merged) stage.
func finishGroup(a *Artifact, g *peakGroup) {
	deepIdx := dominantMode(g.indices, a)
	deep := a.Points[deepIdx].DTGPctPerMin // negative
	var locals []int
	for _, i := range g.indices {
		v := a.Points[i].DTGPctPerMin
		isMode := true
		if i > 0 && a.Points[i-1].IntervalID == a.Points[i].IntervalID && a.Points[i-1].DTGPctPerMin <= v {
			isMode = false
		}
		if i+1 < len(a.Points) && a.Points[i+1].IntervalID == a.Points[i].IntervalID && a.Points[i+1].DTGPctPerMin <= v {
			isMode = false
		}
		if isMode {
			locals = append(locals, i)
		}
	}
	if len(locals) == 0 {
		locals = []int{deepIdx}
	}
	depth := -deep
	kept := []int{}
	for _, i := range locals {
		if i != deepIdx && -a.Points[i].DTGPctPerMin < 0.35*depth {
			continue // insignificant shoulder
		}
		kept = append(kept, i)
	}
	// order modes by time
	for x := 1; x < len(kept); x++ {
		for y := x; y > 0 && kept[y-1] > kept[y]; y-- {
			kept[y-1], kept[y] = kept[y], kept[y-1]
		}
	}
	// merge adjacent modes that lack a distinct saddle between them
	filtered := []int{kept[0]}
	for x := 1; x < len(kept); x++ {
		prev := filtered[len(filtered)-1]
		saddle := saddleBetween(a, prev, kept[x])
		// distinct saddle must be shallower (closer to zero) than the
		// shallower of the two modes by a clear margin
		shallow := math.Max(a.Points[prev].DTGPctPerMin, a.Points[kept[x]].DTGPctPerMin)
		if a.Points[saddle].DTGPctPerMin < shallow+0.15*depth {
			// saddle too deep: unresolved shoulder -> keep only stronger mode
			if a.Points[kept[x]].DTGPctPerMin < a.Points[prev].DTGPctPerMin {
				filtered[len(filtered)-1] = kept[x]
			}
			continue
		}
		filtered = append(filtered, kept[x])
	}
	g.modes = filtered
}

func dominantMode(idxs []int, a *Artifact) int {
	best, bestV := idxs[0], math.Inf(1)
	for _, i := range idxs {
		if v := a.Points[i].DTGPctPerMin; v < bestV {
			bestV, best = v, i
		}
	}
	return best
}

func minDTGBetween(a *Artifact, i1, i2 int) int {
	best, bestV := i1, math.Inf(1)
	for i := i1; i <= i2; i++ {
		if a.Points[i].IntervalID == a.Points[i1].IntervalID && a.Points[i].DTGPctPerMin < bestV {
			bestV, best = a.Points[i].DTGPctPerMin, i
		}
	}
	return best
}

// saddleBetween returns the point closest to zero DTG strictly between two
// modes (the valley that separates overlapping peaks).
func saddleBetween(a *Artifact, i1, i2 int) int {
	best, bestV := -1, math.Inf(-1)
	for i := i1 + 1; i < i2; i++ {
		if a.Points[i].IntervalID != a.Points[i1].IntervalID {
			continue
		}
		if a.Points[i].DTGPctPerMin > bestV {
			bestV, best = a.Points[i].DTGPctPerMin, i
		}
	}
	if best < 0 {
		return minDTGBetween(a, i1, i2)
	}
	return best
}

func newCandidate(a *Artifact, groupID, option string, si, ei, peakIdx int, p Params) StageCandidate {
	id := ids.Of("cand", a.ID, groupID+"|"+option+"|"+itoa(si)+"|"+itoa(ei))
	c := StageCandidate{
		ID:           id,
		GroupID:      groupID,
		Option:       option,
		StartS:       a.Points[si].TimeS,
		EndS:         a.Points[ei].TimeS,
		PeakS:        a.Points[peakIdx].TimeS,
		PeakTSampleC: a.Points[peakIdx].TSampleC,
		Status:       "proposed",
	}
	return c
}

func setLoss(c *StageCandidate, lossPct float64, a *Artifact, si, ei int, basePct float64) {
	lp := lossPct
	lm := lp
	if a.m0Mg != nil {
		lm = lp / 100.0 * *a.m0Mg
	}
	residue := basePct - lossPct
	c.LossPct = &lp
	c.LossMg = &lm
	c.ResidueEndPct = &residue
}

func attributeAtmosphere(a *Artifact, in Inputs) {
	vl := in.Layers[domain.GasValveEvents]
	type vv struct {
		t   float64
		gas string
	}
	var events []vv
	cur := ""
	if vl != nil {
		ordered := append([]domain.ValveEvent(nil), vl.Valves...)
		// keep source order (index); tolerate unsorted inputs
		for _, e := range ordered {
			if e.TimeS.OK() {
				events = append(events, vv{t: *e.TimeS.Value, gas: e.ToGas})
				a.Valves = append(a.Valves, ValveMark{
					TimeS:     *e.TimeS.Value,
					FromGas:   e.FromGas,
					ToGas:     e.ToGas,
					ReactionS: *e.TimeS.Value + in.Params.AtmosphereDelayS,
				})
			}
		}
	}
	gasAt := func(tSample float64) (string, *float64) {
		reactionAt := tSample // peak time is furnace clock
		var switchT *float64
		gas := cur
		for _, e := range events {
			onset := e.t + in.Params.AtmosphereDelayS
			if onset <= reactionAt+1e-9 {
				g := e.gas
				tt := e.t
				switchT = &tt
				gas = g
			}
		}
		return gas, switchT
	}
	for i := range a.Candidates {
		c := &a.Candidates[i]
		gas, sw := gasAt(c.PeakS)
		c.ActiveGas = gas
		c.ValveSwitchS = sw
		if sw != nil {
			onset := *sw + in.Params.AtmosphereDelayS
			c.ReactionOnsetS = &onset
		}
	}
	if len(events) > 0 {
		e := events[0]
		a.Judgements = append(a.Judgements, RuleJudgement{
			Code:    "R1",
			Status:  "applies",
			Message: "valve switch time shifted by atmosphere transport delay before it is the sample reaction time",
			Refs:    []string{"valve@t=" + ftoa(e.t), "delay_s=" + ftoa(in.Params.AtmosphereDelayS)},
		})
	}
}
