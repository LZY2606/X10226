package derive

import (
	"fmt"
	"math"
	"sort"

	"tgareview/internal/domain"
)

// BuildCandidates turns DTG peaks + atmosphere events into stage candidates and
// residue candidates. Deliberately, stages MAY overlap and their losses are
// NOT normalized to sum to 100%. When attribution is not unique all legal
// candidates are retained, each with an explicit reject/uncertainty note.
func BuildCandidates(tg TGResult, dtg DTGResult, atmo AtmoResult, p domain.Params) CandidatesResult {
	out := CandidatesResult{}
	pts := tg.Points
	if len(pts) == 0 || tg.InitialMass <= 0 {
		out.Unknowns = append(out.Unknowns, "无法分解阶段：缺少有效 TG 点或初重")
		return out
	}

	// nearest measured point helpers never cross a gap
	nearest := func(t float64, prefer string) (Point, bool) {
		return nearestPoint(pts, t, prefer)
	}

	for pi, pk := range dtg.Peaks {
		// valleys of the TG curve bracket the peak: search left/right until DTG
		// returns toward 0 or a gap boundary is hit
		startT, endT, gapBounded := bracketStage(dtg.Series, pk, p)
		sp, ok1 := nearest(startT, "left")
		ep, ok2 := nearest(endT, "right")
		c := domain.Candidate{
			Kind: "stage",
			ID:   stageID(pk, sp, ep),
			Name: stageName(pi, pk, atmo),
		}
		c.StartT, c.EndT = r3(startT), r3(endT)
		c.StartTemp, c.EndTemp = r3(sp.T), r3(ep.T)
		c.GapBounded = gapBounded
		c.Overlaps = pk.OverlapsWith != 0
		c.Status = domain.CandProposed

		// linkage to the atmosphere event that is active at peak reaction time
		if ev := activeGas(atmo.Events, pk.TS); ev != nil {
			c.Notes = map[string]string{
				"active_gas":        ev.Gas,
				"valve_t_vs_peak_t": fmt.Sprintf("阀 %gs / 峰 %gs（延迟%g、滞后%g）", ev.ValveTS, pk.TS, atmo.DelayS, atmo.LagS),
			}
		}

		switch {
		case !ok1 || !ok2:
			c.Status = domain.CandRejected
			c.RejectReason = "边界落在采样缺口内且不允许跨缺口插值，无法确定质量"
		case gapBounded && sp.Segment != ep.Segment:
			c.Status = domain.CandRejected
			c.RejectReason = "阶段跨越采样缺口，损失量不可由现有数据唯一决定"
		case pk.Prominence < p.PeakProminencePct:
			c.RejectReason = fmt.Sprintf("prominence %g 低于主阈值 %g：保留为重叠/肩部替代候选", pk.Prominence, p.PeakProminencePct)
		}

		if loss, ok := stageLoss(sp, ep, tg.InitialMass); ok {
			c.MassLossPct = floatPtr(r3(loss))
		}

		c.Chain = []domain.ChainNode{
			{Step: "1 峰", Detail: fmt.Sprintf("DTG 峰 @ %g s, %g°C, DTG=%g %%/min, prominence=%g", pk.TS, pk.T, pk.DTG, pk.Prominence)},
			{Step: "2 边界", Detail: fmt.Sprintf("沿同一段 DTG 回到近零处：[%g s, %g s]；缺口边界=%v", startT, endT, gapBounded)},
			{Step: "3 质量损失", Detail: fmt.Sprintf("loss%% = (m_start − m_end)/m0×100，m0=%g mg", tg.InitialMass), Value: pctLabel(c.MassLossPct)},
			{Step: "4 气氛归属", Detail: "按反应时钟归属到实际到达样品的气体；阀时间不作为归属时刻"},
			{Step: "5 重叠/求和", Detail: "阶段允许重叠；各损失独立报告，不重标定、不强制总和为 100%"},
		}
		out.Stages = append(out.Stages, c)

		// when a shoulder overlaps a primary, emit the alternative attribution
		// as an extra legal candidate instead of collapsing them
		if c.Overlaps && c.Status != domain.CandRejected {
			alt := c
			alt.ID = c.ID + ":alt-merged"
			alt.Name = c.Name + "（合并解释）"
			alt.Notes = mergeNote(c.Notes)
			alt.RejectReason = "合并解释与拆分解释均合法：数据无法唯一决定，全部保留待人工裁定"
			out.Stages = append(out.Stages, alt)
		}
	}

	// residues: final and (if available) at last atmosphere switch
	final := pts[len(pts)-1]
	finalRes := domain.Candidate{
		Kind: "residue", ID: residueID(final, "final"), Name: "最终残留率",
		StartT: final.TS, EndT: final.TS, StartTemp: final.T, EndTemp: final.T,
		Status: domain.CandProposed, ResiduePct: floatPtr(r3(final.MassPct)),
		Chain: []domain.ChainNode{
			{Step: "1 终态质量", Detail: "取最后一个有效（同段内）校正点 m_final", Value: f3(final.MassCorr) + " mg"},
			{Step: "2 初重", Detail: tg.M0Source, Value: f3(tg.InitialMass) + " mg"},
			{Step: "3 残留率", Detail: "residue% = m_final_corr / m0 × 100", Value: f3(final.MassPct) + " %"},
			{Step: "4 校正链", Detail: corrSummary(tg)},
			{Step: "5 口径", Detail: "残留率为独立量；不与各阶段损失做强制配平"},
		},
	}
	out.Residues = append(out.Residues, finalRes)

	if len(atmo.Events) > 0 {
		last := atmo.Events[len(atmo.Events)-1]
		if rp, ok := nearest(last.ReactionTS, "right"); ok && rp.TS <= final.TS {
			rr := domain.Candidate{
				Kind: "residue", ID: residueID(rp, "atmo:"+last.Gas),
				Name: fmt.Sprintf("%s 气氛到达时残留率", gasLabel(last.Gas)),
				StartT: rp.TS, EndT: rp.TS, StartTemp: rp.T, EndTemp: rp.T,
				Status: domain.CandProposed, ResiduePct: floatPtr(r3(rp.MassPct)),
				Notes: map[string]string{
					"reaction_t": fmt.Sprintf("%g", last.ReactionTS),
					"valve_t":    fmt.Sprintf("%g", last.ValveTS),
				},
				Chain: []domain.ChainNode{
					{Step: "1 反应时刻", Detail: fmt.Sprintf("最后阀切换 %g s + 延迟 %g s = 反应时钟 %g s", last.ValveTS, atmo.DelayS, last.ReactionTS)},
					{Step: "2 取点", Detail: "在反应时刻右侧取最近同段有效点（不跨缺口）", Value: f3(rp.TS) + " s"},
					{Step: "3 残留率", Detail: "residue% = m_corr(t)/m0×100", Value: f3(rp.MassPct) + " %"},
				},
			}
			out.Residues = append(out.Residues, rr)
		}
	}

	// unknowns: overlapping primary+shoulder both surviving is explicit
	for _, c := range out.Stages {
		if c.Overlaps && c.Status == domain.CandProposed {
			out.Unknowns = append(out.Unknowns, c.Name+" 存在重叠峰，拆分/合并无法由数据唯一决定")
		}
	}
	out.Chain = []domain.ChainNode{
		{Step: "汇总", Detail: fmt.Sprintf("阶段 %d 个（含替代解释）、残留率 %d 个；重叠允许、不强制 100%% 配平", len(out.Stages), len(out.Residues))},
	}
	return out
}

func bracketStage(s []DTGPoint, pk Peak, p domain.Params) (float64, float64, bool) {
	idx := -1
	for i := range s {
		if s[i].Line == pk.Line {
			idx = i
			break
		}
	}
	if idx < 0 {
		return pk.TS, pk.TS, false
	}
	seg := s[idx].Segment
	threshold := p.OverlapPeakPromPct * 0.25 // DTG near zero (in %/min)
	left, right := idx, idx
	gapLeft, gapRight := false, false
	for left > 0 && s[left-1].Defined && s[left-1].Segment == seg {
		if s[left-1].DTGPctMin >= -threshold {
			break
		}
		left--
	}
	if left == 0 || !s[left].Defined || (left > 0 && s[left-1].Segment != seg) {
		gapLeft = true
	}
	for right < len(s)-1 && s[right+1].Defined && s[right+1].Segment == seg {
		if s[right+1].DTGPctMin >= -threshold {
			break
		}
		right++
	}
	if right == len(s)-1 || (right < len(s)-1 && s[right+1].Segment != seg) {
		gapRight = true
	}
	return s[left].TS, s[right].TS, gapLeft || gapRight
}

func nearestPoint(pts []Point, t float64, prefer string) (Point, bool) {
	// exact or nearest on the same side and same segment as neighbors
	var best *Point
	bestD := math.Inf(1)
	for i := range pts {
		cand := &pts[i]
		d := math.Abs(cand.TS - t)
		// forbid crossing a gap: if target is between two points of different
		// segments, accept only the closer point (marked by caller), and set ok
		// false if the target lies inside the gap
		if d < bestD {
			bestD, best = d, cand
		}
	}
	if best == nil {
		return Point{}, false
	}
	// inside-gap check
	for i := 0; i+1 < len(pts); i++ {
		a, b := pts[i], pts[i+1]
		if a.Segment != b.Segment && t > a.TS && t < b.TS {
			return *best, false
		}
	}
	return *best, true
}

func stageLoss(a, b Point, m0 float64) (float64, bool) {
	if m0 <= 0 || a.Segment != b.Segment {
		return 0, false
	}
	return (a.MassCorr - b.MassCorr) / m0 * 100, true
}

func activeGas(evs []AtmoEvent, reactionT float64) *AtmoEvent {
	var active *AtmoEvent
	for i := range evs {
		if evs[i].ReactionTS <= reactionT {
			active = &evs[i]
		}
	}
	return active
}

func stageName(i int, pk Peak, atmo AtmoResult) string {
	base := fmt.Sprintf("阶段 %d（%.0f°C）", i+1, pk.T)
	if ev := activeGas(atmo.Events, pk.TS); ev != nil {
		base += " · " + gasLabel(ev.Gas)
	}
	return base
}

func corrSummary(tg TGResult) string {
	d, b := "关", "关"
	if tg.DriftApplied {
		d = "开"
	}
	if tg.BlankApplied {
		b = "开"
	}
	return fmt.Sprintf("漂移校正=%s，空白扣除=%s，初重来源=%s", d, b, tg.M0Source)
}

func mergeNote(m map[string]string) map[string]string {
	n := map[string]string{}
	for k, v := range m {
		n[k] = v
	}
	n["alternative"] = "merged"
	return n
}

func pctLabel(v *float64) string {
	if v == nil {
		return "未知"
	}
	return f3(*v) + " %"
}

func floatPtr(v float64) *float64 { return &v }

// stable candidate ids
func stageID(pk Peak, sp, ep Point) string {
	return domain.ContentID("cand_stage", domain.CanonicalJSON(map[string]any{
		"line": pk.Line, "t": pk.TS, "prom": pk.Prominence, "s": sp.TS, "e": ep.TS,
	}))
}

func residueID(p Point, tag string) string {
	return domain.ContentID("cand_res", domain.CanonicalJSON(map[string]any{
		"t": p.TS, "m": p.MassCorr, "tag": tag,
	}))
}

// SortedStages returns stages ordered by start time.
func SortedStages(c []domain.Candidate) []domain.Candidate {
	out := append([]domain.Candidate(nil), c...)
	sort.SliceStable(out, func(i, j int) bool { return out[i].StartT < out[j].StartT })
	return out
}
