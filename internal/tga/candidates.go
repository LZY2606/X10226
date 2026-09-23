package tga

import (
	"fmt"
	"math"
	"sort"
)

// buildCandidates turns peaks into legal stage candidates. Overlapping peak
// clusters yield multiple non-unique candidates; none of them are forced to
// partition 100% of the mass.
func buildCandidates(rep *Report, p Params) {
	if rep.Initial == nil || rep.Initial.ItemID == "" {
		rep.Warnings = append(rep.Warnings, "初重未知，阶段候选不生成失重百分比")
	}
	groups := map[string][]*Peak{}
	var groupOrder []string
	for i := range rep.Peaks {
		pk := &rep.Peaks[i]
		if pk.GroupID == "" {
			pk.GroupID = HashID("psolo", map[string]any{"t": pk.TimeS})
		}
		if _, ok := groups[pk.GroupID]; !ok {
			groupOrder = append(groupOrder, pk.GroupID)
		}
		groups[pk.GroupID] = append(groups[pk.GroupID], pk)
	}
	for _, gid := range groupOrder {
		pks := groups[gid]
		sort.Slice(pks, func(i, j int) bool { return pks[i].TimeS < pks[j].TimeS })
		for alt, pk := range pks {
			startS, endS, startRef, endRef := stageBounds(rep, pk.TimeS, p.OverlapWindowS/2)
			c := StageCandidate{
				ID: HashID("stage", map[string]any{
					"peak": pk.ID, "alt": alt, "gid": gid,
					"params": rep.ParamFingerprint,
				}),
				GroupID:     gid,
				Alternative: len(pks) > 1 && alt > 0,
				Label:       stageLabel(alt, len(pks), pk.TimeS),
				PeakID:      pk.ID,
				StartS:      startS,
				EndS:        endS,
				Status:      "candidate",
				Attribution: attribute(rep, startS, endS, p),
				Chain:       stageChain(rep, pk, startS, endS, startRef, endRef),
			}
			if len(pks) > 1 {
				c.Reasons = append(c.Reasons, "重叠峰：DTG 峰间距在重叠窗内，存在多个合法分解边界")
			}
			ml, unknown := massLoss(rep, startS, endS)
			if unknown {
				c.Unknowns = append(c.Unknowns, "区间内含非单调噪声点或采样缺口，失重百分比不可唯一决定")
			} else if ml != nil {
				c.MassLossPct = ml
			}
			for j, other := range pks {
				if j != alt {
					c.OverlapsWith = append(c.OverlapsWith,
						HashID("stage", map[string]any{
							"peak": other.ID, "alt": j, "gid": gid, "params": rep.ParamFingerprint,
						}))
				}
			}
			rep.Candidates = append(rep.Candidates, c)
		}
	}
	// keep deterministic order
	sort.SliceStable(rep.Candidates, func(i, j int) bool {
		if rep.Candidates[i].StartS != rep.Candidates[j].StartS {
			return rep.Candidates[i].StartS < rep.Candidates[j].StartS
		}
		return rep.Candidates[i].ID < rep.Candidates[j].ID
	})
	if len(rep.Candidates) > 1 {
		rep.Warnings = append(rep.Warnings, "阶段允许时间重叠，失重百分比不强制总和为 100%")
	}
}

func stageLabel(alt, n int, t float64) string {
	if n > 1 {
		return fmt.Sprintf("重叠候选 %d/%d @ %.0fs", alt+1, n, t)
	}
	return fmt.Sprintf("失重阶段 @ %.0fs", t)
}

// stageBounds picks the nearest clean TG points at +/- half the overlap
// window, snapped inward to the closest DTG-supported points.
func stageBounds(rep *Report, peakT, halfW float64) (float64, float64, ValueRef, ValueRef) {
	clean := []TgPoint{}
	for _, q := range rep.Tg {
		if !q.NoiseFlag {
			clean = append(clean, q)
		}
	}
	if len(clean) == 0 {
		return peakT - halfW, peakT + halfW, ValueRef{}, ValueRef{}
	}
	lo := nearestAtOrBefore(clean, peakT-halfW)
	hi := nearestAtOrAfter(clean, peakT+halfW)
	// respect gap segments: snap to the peak's segment
	segStart, segEnd := segmentRangeOf(rep, peakT)
	if lo.TimeS < segStart {
		lo = nearestAtOrAfter(clean, segStart)
	}
	if hi.TimeS > segEnd && segEnd > 0 {
		hi = nearestAtOrBefore(clean, segEnd)
	}
	return lo.TimeS, hi.TimeS,
		ValueRef{ID: lo.ID, Label: "起点 TG", Value: fmt.Sprintf("%.3f%% @ %.0fs", lo.TgPct, lo.TimeS)},
		ValueRef{ID: hi.ID, Label: "终点 TG", Value: fmt.Sprintf("%.3f%% @ %.0fs", hi.TgPct, hi.TimeS)}
}

func segmentRangeOf(rep *Report, t float64) (float64, float64) {
	bySeg := splitTgByGaps(rep.Tg)
	for _, s := range bySeg {
		if len(s) > 0 && t >= s[0].TimeS && t <= s[len(s)-1].TimeS {
			return s[0].TimeS, s[len(s)-1].TimeS
		}
	}
	if len(rep.Tg) > 0 {
		return rep.Tg[0].TimeS, rep.Tg[len(rep.Tg)-1].TimeS
	}
	return 0, 0
}

func nearestAtOrBefore(tg []TgPoint, t float64) TgPoint {
	best := tg[0]
	for _, q := range tg {
		if q.TimeS <= t {
			best = q
		}
	}
	return best
}

func nearestAtOrAfter(tg []TgPoint, t float64) TgPoint {
	best := tg[len(tg)-1]
	for i := len(tg) - 1; i >= 0; i-- {
		if tg[i].TimeS >= t {
			best = tg[i]
		}
	}
	return best
}

func tgAt(rep *Report, t float64) (TgPoint, bool) {
	for _, q := range rep.Tg {
		if math.Abs(q.TimeS-t) < 1e-9 {
			return q, true
		}
	}
	return TgPoint{}, false
}

func massLoss(rep *Report, startS, endS float64) (*float64, bool) {
	a, okA := tgAt(rep, startS)
	b, okB := tgAt(rep, endS)
	if !okA || !okB {
		return nil, true
	}
	for _, q := range rep.Tg {
		if q.TimeS > startS && q.TimeS < endS && q.NoiseFlag {
			return nil, true
		}
	}
	if a.NoiseFlag || b.NoiseFlag {
		return nil, true
	}
	for _, g := range rep.Gaps {
		if g.FromS >= startS && g.ToS <= endS {
			return nil, true
		}
	}
	ml := a.TgPct - b.TgPct
	return &ml, false
}

// attribute maps a stage to a valve event using atmosphere transport delay and
// furnace lag: valve switching time is not reaction time.
func attribute(rep *Report, startS, endS float64, p Params) Attribution {
	a := Attribution{LagS: p.AtmDelayS + p.FurnaceLagS, Status: "none"}
	var hits []GasSwitch
	for _, sw := range rep.Switches {
		reactS := sw.ReactorS + p.FurnaceLagS
		if reactS >= startS && reactS <= endS {
			hits = append(hits, sw)
		}
	}
	if len(hits) == 0 {
		// nearest switch before stage start
		var nearest *GasSwitch
		for i := range rep.Switches {
			sw := &rep.Switches[i]
			reactS := sw.ReactorS + p.FurnaceLagS
			if reactS <= startS {
				nearest = sw
			}
		}
		if nearest != nil {
			reactS := nearest.ReactorS + p.FurnaceLagS
			v, r := nearest.ValveS, nearest.ReactorS
			a.SwitchID, a.ValveS, a.ReactorS = nearest.ID, &v, &r
			a.Status = "none"
			a.Note = fmt.Sprintf("最近阀事件(%.0fs 阀门 → %.0fs 反应)落在阶段外，不强行归因", nearest.ValveS, reactS)
		}
		return a
	}
	if len(hits) == 1 {
		v, r := hits[0].ValveS, hits[0].ReactorS
		a.SwitchID, a.ValveS, a.ReactorS = hits[0].ID, &v, &r
		a.Status = "attributed"
		a.Note = fmt.Sprintf("阀门切换 %.0fs，经气氛延迟 %.0fs + 温度滞后 %.0fs 后在 %.0fs 作用于样品",
			hits[0].ValveS, p.AtmDelayS, p.FurnaceLagS, r+p.FurnaceLagS)
		return a
	}
	for _, h := range hits[1:] {
		a.Alternatives = append(a.Alternatives, h.ID)
	}
	v, r := hits[0].ValveS, hits[0].ReactorS
	a.SwitchID, a.ValveS, a.ReactorS = hits[0].ID, &v, &r
	a.Status = "ambiguous"
	a.Note = fmt.Sprintf("阶段内有 %d 个延迟后的气氛到达点，归因不唯一，保留全部候选", len(hits))
	return a
}

func stageChain(rep *Report, pk *Peak, startS, endS float64, lo, hi ValueRef) []ChainStep {
	steps := []ChainStep{
		{Step: "dtg", Text: fmt.Sprintf("DTG 局部极小值 %.4f %%/min @ %.0fs（阈值=峰深占比参数）", pk.Dtg, pk.TimeS),
			Refs: []ValueRef{{ID: pk.ID, Label: "DTG 峰", Value: fmt.Sprintf("%.4f @ %.0fs", pk.Dtg, pk.TimeS)}}},
		{Step: "bounds", Text: fmt.Sprintf("以峰 ± 半重叠窗(%.0fs)取最近干净 TG 点作为阶段边界", 0), Refs: []ValueRef{lo, hi}},
	}
	if rep.Initial != nil && rep.Initial.ItemID != "" {
		steps = append(steps, ChainStep{
			Step: "initial", Text: fmt.Sprintf("失重以初重 %.4f mg 为锚点（批次 %s）", rep.Initial.Mg, rep.Initial.BatchID),
			Refs: []ValueRef{{ID: rep.Initial.ItemID, Label: "样品初重", Value: fmt.Sprintf("%.4f mg", rep.Initial.Mg)}},
		})
	}
	return steps
}
