package engine

import (
	"fmt"

	"tgaline/internal/domain"
)

type stageResult struct {
	candidates []domain.StageCandidate
}

// massAtOrAfter / massAtOrBefore find the nearest measured corrected mass at a
// boundary. They never interpolate across a gap: a gap inside [a,b] forces an
// unknown mass loss and the candidate stays as a legal-but-unknown result.
func stageCandidates(s *domain.Series, pf peakFindingResult, wins []domain.ReactionWindow, in layerInput) stageResult {
	var out []domain.StageCandidate
	gapSet := map[int]bool{}
	for _, g := range s.GapAfter {
		gapSet[g] = true
	}

	// massLoss returns (loss, known, reasons). loss is mg over [a,b].
	massLoss := func(a, b float64) (float64, bool, []string) {
		var reasons []string
		i0, i1 := -1, -1
		for i := 0; i < len(s.TimeS); i++ {
			if !s.Mask[i] {
				continue
			}
			if i0 == -1 && s.TimeS[i] >= a {
				i0 = i
			}
			if s.TimeS[i] <= b {
				i1 = i
			}
		}
		if i0 == -1 || i1 == -1 || i1 <= i0 {
			return 0, false, []string{"窗口内没有足够的已测点"}
		}
		for g := range gapSet {
			if g >= i0 && g < i1 {
				reasons = append(reasons, fmt.Sprintf("窗口跨越采样缺口（点 %d→%d），缺口不做插值，失重无法唯一决定", g+1, g+2))
			}
		}
		loss := s.MassMG[i0] - s.MassMG[i1]
		if len(reasons) > 0 {
			return loss, false, reasons
		}
		return loss, true, nil
	}

	candidate := func(w domain.Window, peakIDs []string, salt int, reasons []string, ambiguous bool) {
		loss, known, lr := massLoss(w.StartS, w.EndS)
		if lr != nil {
			reasons = append(reasons, lr...)
			ambiguous = ambiguous || !known
		}
		c := domain.StageCandidate{
			ID: stageID(w, loss, salt), Window: w, PeakIDs: peakIDs,
			Ambiguous: ambiguous, Reasons: reasons,
		}
		if known {
			c.MassLossMG = loss
			if in.m0 != nil {
				pct := loss / in.m0.mg * 100.0
				c.MassLossPct = &pct
			}
		}
		c.StartTK = tempAt(s, w.StartS)
		c.EndTK = tempAt(s, w.EndS)
		// atmosphere attribution
		centerT := (w.StartS + w.EndS) / 2
		hits := windowsCovering(wins, centerT)
		switch {
		case len(hits) == 1:
			c.GasWindowID = hits[0].ID
			c.Reasons = append(c.Reasons, fmt.Sprintf("峰中心落在 %s 反应延迟窗口内（阀时间≠反应时间）", hits[0].Gas))
		case len(hits) > 1:
			c.Ambiguous = true
			c.Reasons = append(c.Reasons, fmt.Sprintf("峰中心同时落入 %d 个气氛反应窗口，气氛归属无法唯一决定，保留全部可能", len(hits)))
		}
		out = append(out, c)
	}

	salt := 0
	// deterministic group order
	var gids []string
	for gid := range pf.groups {
		gids = append(gids, gid)
	}
	// order groups by first peak time
	sortGidsByTime(gids, pf.groups, pf.peaks)

	for _, gid := range gids {
		pids := pf.groups[gid]
		var groupPeaks []domain.Peak
		for _, id := range pids {
			for k := range pf.peaks {
				if pf.peaks[k].ID == id {
					groupPeaks = append(groupPeaks, pf.peaks[k])
				}
			}
		}
		// individual peak candidates (stages may overlap)
		for _, pk := range groupPeaks {
			var reasons []string
			amb := false
			if len(groupPeaks) > 1 {
				amb = true
				reasons = append(reasons, "重叠峰组中的单峰候选；与组内其它峰共享质量下降区间，分解不唯一")
			}
			candidate(pk.Window, []string{pk.ID}, salt, reasons, amb)
			salt++
		}
		if len(groupPeaks) > 1 {
			minT := groupPeaks[0].Window.StartS
			maxT := groupPeaks[len(groupPeaks)-1].Window.EndS
			w := domain.Window{StartS: minT, EndS: maxT}
			reasons := []string{
				fmt.Sprintf("重叠峰组合并候选（%d 个峰，组 %s）；阶段可与其它阶段重叠，且不强制各阶段百分比之和为 100%%", len(groupPeaks), gid),
			}
			candidate(w, pids, salt, reasons, true)
			salt++
		}
	}
	return stageResult{candidates: out}
}

// tempAt returns measured temperature at or immediately before t; 0 if none.
func tempAt(s *domain.Series, t float64) float64 {
	best := 0.0
	found := false
	for i := 0; i < len(s.TimeS); i++ {
		if s.TimeS[i] <= t {
			best = s.TK[i]
			found = true
		}
	}
	if found {
		return best
	}
	return 0
}
