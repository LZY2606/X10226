package engine

import (
	"fmt"
	"math"

	"tga-lineage/internal/domain"
)

// buildStages emits every legal stage candidate. Overlapping peaks produce both
// a split (two stages) and a merged (one stage) reading — both are kept, with
// the non-adopted reading carrying a rejection reason. Stage windows that meet
// a sampling gap are emitted as gap-ambiguous candidates; loss across a gap is
// reported as unknown rather than interpolated.
func buildStages(ctx *buildCtx, add func(domain.Node) domain.Node) []string {
	var ids []string
	pks := ctx.pks
	mergedSet := map[[2]int]bool{}
	for _, o := range ctx.ovs {
		mergedSet[[2]int{o.a.idx, o.b.idx}] = true
	}

	emit := func(s stageSpec, status string) string {
		id := s.stageID(ctx.stageTags, ctx.fp)
		atm := "UNKNOWN"
		if !math.IsNaN(s.peakT) {
			atm = atmosphereAt(ctx.gasEvs, s.peakT)
		}
		mStart, okS := ctx.massAt(s.startT)
		mEnd, okE := ctx.massAt(s.endT)
		loss := math.NaN()
		pct := math.NaN()
		knownLoss := okS && okE
		if knownLoss {
			loss = mStart - mEnd
			if ctx.initialMass > 0 {
				pct = loss / ctx.initialMass * 100
			}
		}
		n := domain.Node{
			ID: id, Kind: "stage/candidate", Status: status, Group: s.group,
			Label: s.label, Unit: "mg", Basis: "t_since_run_start",
			Extra: map[string]any{
				"stage_key": s.key, "start_t": s.startT, "end_t": s.endT,
				"peak_t": s.peakT, "merged": s.merged,
				"mass_start_mg": optVal(mStart, okS), "mass_end_mg": optVal(mEnd, okE),
				"loss_mg": optVal(loss, knownLoss), "loss_pct_initial": optVal(pct, knownLoss && ctx.initialMass > 0),
				"atmosphere_reaction": atm,
				"valve_at_peak":       func() string { if len(ctx.gasEvs) > 0 { return valveAt(ctx.gasEvs, s.peakT) }; return "" }(),
				"sample_temp_peak_c":  optVal(sampleTempAt(ctx.sp, s.peakT), !math.IsNaN(sampleTempAt(ctx.sp, s.peakT))),
				"furnace_setpoint_peak_c": optVal(furnaceSetpointAt(ctx.fpPts, s.peakT), len(ctx.fpPts) > 0),
				"peak_indices":        s.peaks,
			},
			Direct: []domain.NodeRef{ctx.refs["dtg"], ctx.refs["tg"], ctx.refs["gas"], ctx.refs["tare"], ctx.refs["sampleTemp"], ctx.refs["furnace"]},
			SourceIDs: ctx.stageTags, Rejects: s.rejects,
		}
		if s.gapTouched {
			n.Extra["gap_touched"] = true
			n.Extra["gap_range_s"] = s.gapRange
			n.Rejects = append(n.Rejects, domain.Rejection{Code: "gap_ambiguous", Message: "阶段边界与采样缺口相交，跨缺口不插值；存在多个合法边界候选"})
			if !knownLoss {
				n.Status = "unknown"
			}
		}
		if !knownLoss {
			n.Extra["loss_note"] = "端点质量不可得（缺口/未转换值），失重未知，未做插值"
		}
		add(n)
		ids = append(ids, id)
		return id
	}

	// merged alternatives for overlapping peak pairs
	for k, o := range ctx.ovs {
		startT := ctx.ts[o.a.startI]
		endT := ctx.ts[o.b.endI]
		grp := fmt.Sprintf("overlap@%.1f", math.Min(o.a.t, o.b.t))
		s := stageSpec{key: fmt.Sprintf("merged%d", k), group: grp, merged: true,
			label: fmt.Sprintf("重叠峰合并候选 #%d（t≈%.0f–%.0f s，%s 气氛）", k+1, startT, endT, atmosphereAt(ctx.gasEvs, (o.a.t+o.b.t)/2)),
			startT: startT, endT: endT, peakT: (o.a.t + o.b.t) / 2,
			peaks: []int{o.a.idx, o.b.idx},
			rejects: []domain.Rejection{{Code: "alternative_to_split", Message: "与拆分读法互斥；保留为合法候选，默认不采纳"}}}
		markGapTouched(ctx, &s)
		emit(s, "rejected")
	}

	// primary one-stage-per-peak readings
	for i, p := range pks {
		startT := ctx.ts[p.startI]
		endT := ctx.ts[p.endI]
		if i > 0 {
			startT = (ctx.ts[pks[i-1].endI] + ctx.ts[p.startI]) / 2
		}
		if i+1 < len(pks) {
			endT = (ctx.ts[p.endI] + ctx.ts[pks[i+1].startI]) / 2
		}
		grp := ""
		for _, o := range ctx.ovs {
			if o.a.idx == p.idx || o.b.idx == p.idx {
				grp = fmt.Sprintf("overlap@%.1f", math.Min(o.a.t, o.b.t))
			}
		}
		s := stageSpec{key: fmt.Sprintf("peak%d", i+1), group: grp,
			label: fmt.Sprintf("阶段候选 #%d（峰 t≈%.0f s，%s 气氛）", i+1, p.t, atmosphereAt(ctx.gasEvs, p.t)),
			startT: startT, endT: endT, peakT: p.t, peakI: p.idx,
			peaks: []int{p.idx}}
		markGapTouched(ctx, &s)
		emit(s, "candidate")
	}
	return ids
}

func markGapTouched(ctx *buildCtx, s *stageSpec) {
	for _, g := range ctx.gaps {
		overlap := !(g.endT <= s.startT || g.startT >= s.endT)
		if overlap {
			s.gapTouched = true
			s.gapRange = [2]float64{g.startT, g.endT}
			return
		}
	}
}

func optVal(v float64, ok bool) any {
	if !ok || math.IsNaN(v) {
		return nil
	}
	return v
}

// residueNode emits final residue, per-stage coverage and the unattributed
// remainder. Stage percentages are NOT forced to sum to 100%.
func residueNode(id string, ctx buildCtx, stageIDs []string) domain.Node {
	tEnd := lastValidTime(ctx)
	mEnd, okEnd := ctx.massAt(tEnd)
	n := domain.Node{ID: id, Kind: "derived/residue", Label: "最终残留率（可展开计算链）",
		Basis: "t_since_run_start",
		Extra: map[string]any{"end_t": tEnd},
		Direct: []domain.NodeRef{ctx.refs["tg"], ctx.refs["tare"]},
		SourceIDs: ctx.stageTags}
	for _, sid := range stageIDs {
		n.Direct = append(n.Direct, domain.NodeRef{ID: sid, Kind: "stage/candidate", Role: "stage"})
	}
	resPct := math.NaN()
	if okEnd && ctx.initialMass > 0 {
		resPct = mEnd / ctx.initialMass * 100
		v := resPct
		n.Value = &v
		n.Unit = "%"
		n.Extra["residue_mass_mg"] = mEnd
		n.Extra["initial_mass_mg"] = ctx.initialMass
		n.Extra["formula"] = "residue% = m(end) / m0(revision row) × 100"
		n.Status = "current"
	} else {
		n.Status = "unknown"
		if ctx.initialMass <= 0 {
			n.Rejects = append(n.Rejects, domain.Rejection{Code: "no_initial_mass", Message: "初重缺失：残留率无法计算（原始质量仍保留）"})
		}
		if !okEnd {
			n.Rejects = append(n.Rejects, domain.Rejection{Code: "end_in_gap", Message: "末点落在缺口：残留率未知，未跨缺口外推"})
		}
	}
	return n
}

func lastValidTime(ctx buildCtx) float64 {
	for i := len(ctx.ts) - 1; i >= 0; i-- {
		if ctx.ok[i] && !math.IsNaN(ctx.sm[i]) {
			return ctx.ts[i]
		}
	}
	if len(ctx.ts) > 0 {
		return ctx.ts[len(ctx.ts)-1]
	}
	return 0
}


// ManualStage produces a reviewer-adjusted stage node. Loss is recomputed from
// the supplied TG series at the new window; endpoints meeting a gap stay
// unknown (no interpolation). Provenance points at the parent candidate.
func ManualStage(parent domain.Node, tg *domain.Node, startT, endT float64, decisionID int) domain.Node {
	out := parent
	out.Kind = "stage/manual"
	out.Label = "人工调整阶段：" + parent.Label
	out.Group = parent.Group
	if out.Extra == nil {
		out.Extra = map[string]any{}
	}
	out.Extra["start_t"] = startT
	out.Extra["end_t"] = endT
	out.Extra["manual_decision_id"] = decisionID
	m0, okS := sampleAt(tg, startT)
	m1, okE := sampleAt(tg, endT)
	if okS && okE {
		loss := m0 - m1
		out.Extra["mass_start_mg"] = m0
		out.Extra["mass_end_mg"] = m1
		out.Extra["loss_mg"] = loss
		if f, hasM0 := out.Extra["initial_mass_mg"]; hasM0 {
			if x, ok := f.(float64); ok && x > 0 {
				out.Extra["loss_pct_initial"] = loss / x * 100
			}
		}
		out.Status = "accepted_manual"
	} else {
		out.Extra["loss_note"] = "人工窗口端点落入缺口/未转换值，失重未知"
		out.Status = "unknown"
	}
	tags := append([]string{}, parent.SourceIDs...)
	tags = append(tags, fmt.Sprintf("manual-decision:%d", decisionID))
	out.SourceIDs = tags
	sum := fmt.Sprintf("%s|%.6g|%.6g|%d", parent.ID, startT, endT, decisionID)
	out.ID = "n_manual_" + shortHash(sum)
	out.Direct = append([]domain.NodeRef{{ID: parent.ID, Kind: parent.Kind, Role: "parent_candidate"}}, out.Direct...)
	return out
}

func sampleAt(n *domain.Node, t float64) (float64, bool) {
	if n == nil || len(n.Times) == 0 {
		return 0, false
	}
	best, bestD := -1, math.Inf(1)
	for i, x := range n.Times {
		d := math.Abs(x - t)
		if d < bestD && !math.IsNaN(n.Vals[i]) {
			bestD, best = d, i
		}
	}
	if best < 0 {
		return 0, false
	}
	return n.Vals[best], true
}
