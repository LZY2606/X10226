package analyze

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"

	"tgax/internal/model"
)

func ParamFingerprint(p model.Params) string {
	b, _ := json.Marshal(p)
	h := sha1.Sum(b)
	return "fp-" + hex.EncodeToString(h[:])[:12]
}

func srcKey(vs ...*model.InputVersion) string {
	ids := make([]string, 0, len(vs))
	for _, v := range vs {
		if v != nil {
			ids = append(ids, v.ID)
		}
	}
	sort.Strings(ids)
	b, _ := json.Marshal(ids)
	h := sha1.Sum(b)
	return "src-" + hex.EncodeToString(h[:])[:12]
}

func hid(prefix string, parts ...any) string {
	b, _ := json.Marshal(parts)
	h := sha1.Sum(b)
	return prefix + "-" + hex.EncodeToString(h[:])[:10]
}

func qf(v float64, unit, basis, kind string) model.Quantity {
	return model.Quantity{Value: &v, Unit: unit, Basis: basis, Status: "ok"}
}

type peak struct {
	i       int
	t       float64
	amp     float64
	left    int
	right   int
	cluster int
}

func detectStages(r *Result, p model.Params) []StageCandidate {
	var rawPeaks []peak
	for i := 1; i < len(r.Points)-1; i++ {
		c := r.Points[i]
		if c.DTG == nil || c.GapBefore || r.Points[i+1].GapBefore {
			continue
		}
		if *c.DTG < -p.ProminenceMgPerMin {
			if *c.DTG <= dval(r.Points[i-1].DTG) && *c.DTG <= dval(r.Points[i+1].DTG) {
				rawPeaks = append(rawPeaks, peak{i: i, t: c.T, amp: *c.DTG})
			}
		}
	}

	// cluster peaks within merge window -> overlapping family candidates
	used := make([]bool, len(rawPeaks))
	var clusters [][]int
	for i := range rawPeaks {
		if used[i] {
			continue
		}
		grp := []int{i}
		used[i] = true
		for j := i + 1; j < len(rawPeaks); j++ {
			if used[j] {
				continue
			}
			if rawPeaks[j].t-rawPeaks[grp[len(grp)-1]].t <= p.MergeWindowS {
				grp = append(grp, j)
				used[j] = true
			}
		}
		clusters = append(clusters, grp)
	}

	var cands []StageCandidate
	for ci, grp := range clusters {
		// boundaries: walk to where DTG returns near zero / prominence threshold
		mk := func(pkIdx, role int, members []int) StageCandidate {
			pk := rawPeaks[pkIdx]
			left := pk.i
			right := pk.i
			for j := pk.i - 1; j >= 0; j-- {
				if r.Points[j+1].GapBefore {
					break
				}
				if r.Points[j].DTG == nil || *r.Points[j].DTG >= -p.ProminenceMgPerMin/2 {
					left = j
					break
				}
				left = j
			}
			for j := pk.i + 1; j < len(r.Points); j++ {
				if r.Points[j].GapBefore {
					break
				}
				if r.Points[j].DTG == nil || *r.Points[j].DTG >= -p.ProminenceMgPerMin/2 {
					right = j
					break
				}
				right = j
			}
			// overlap: adjacent members share the valley between them
			if len(members) > 1 {
				if role >= 0 && pkIdx != members[0] {
					// extend left into the overlap valley only; legal alternative kept
				}
			}
			sc := StageCandidate{
				PeakT:     r.Points[pk.i].T,
				StartT:    r.Points[left].T,
				EndT:      r.Points[right].T,
				DTGPeak:   r.Points[pk.i].DTG,
				PeakTempC: r.Points[pk.i].TempF,
				Gas:       gasAt(r.GasIntervals, r.Points[pk.i].T),
				Family:    "primary",
				Current:   true,
			}
			if len(members) > 1 {
				for _, m := range members {
					if m != pkIdx {
						sc.OverlapsWith = append(sc.OverlapsWith, fmt.Sprintf("t=%.0f", rawPeaks[m].t))
					}
				}
				sc.Family = []string{"overlap-left", "primary", "overlap-right"}[role]
				if role < 0 || role > 2 {
					sc.Family = "overlap-alt"
				}
				sc.Unknown = append(sc.Unknown, "重叠峰：失重归属不唯一，簇内每个峰均保留为合法候选")
			}
			if r.Points[pk.i].Noisy {
				sc.Unknown = append(sc.Unknown, "峰位附近存在非单调噪声点")
			}
			sc.BoundedByGap = gapNear(r, left, right)
			if sc.BoundedByGap {
				sc.Unknown = append(sc.Unknown, "候选边界邻接采样缺口，起止质量不做跨缺口插值")
			}
			fillMass(r, &sc)
			sc.ID = hid("stage", r.ParamFP, r.SourceKey, ci, sc.PeakT, sc.Family)
			sc.SourceIDs = []string{
				hid("val", r.SourceKey, "point", left, "corrected"),
				hid("val", r.SourceKey, "point", right, "corrected"),
				hid("val", r.SourceKey, "point", pk.i, "dtg"),
			}
			if mass0Avail(r) {
				sc.SourceIDs = append(sc.SourceIDs, hid("val", r.SourceKey, "mass0"))
			}
			return sc
		}
		if len(grp) == 1 {
			cands = append(cands, mk(grp[0], -1, grp))
		} else {
			for role, pkIdx := range grp {
				cands = append(cands, mk(pkIdx, role, grp))
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].PeakT < cands[j].PeakT })

	if len(cands) > 0 {
		ov := 0
		for _, c := range cands {
			if len(c.OverlapsWith) > 0 {
				ov++
			}
		}
		if ov > 0 {
			r.Assertions = append(r.Assertions, model.Assertion{ID: "ast-overlap", Code: "OVERLAPPING_PEAKS", Severity: "warning",
				Detail: map[string]any{"candidate_count": len(cands), "overlap_candidates": ov},
				Text:   fmt.Sprintf("发现重叠峰：保留 %d 个合法阶段候选（含 %d 个重叠归属候选），不强制合并为单一结论", len(cands), ov)})
		}
	}
	if len(r.GasIntervals) > 0 {
		delayed := false
		for _, g := range r.GasIntervals {
			if g.DelayS > 0 {
				delayed = true
			}
		}
		if delayed {
			r.Assertions = append(r.Assertions, model.Assertion{ID: "ast-atm-delay", Code: "ATMOSPHERE_DELAY", Severity: "info",
				Text: "气氛事件按气氛延迟映射到样品反应时间线；阀切换时间不等于样品反应时间，阶段按反应时间归属气体"})
		}
	}
	return cands
}

func dval(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

func gapNear(r *Result, left, right int) bool {
	for i := left; i <= right && i < len(r.Points); i++ {
		if r.Points[i].GapBefore {
			return true
		}
	}
	return false
}

func fillMass(r *Result, sc *StageCandidate) {
	pl, pr := pointAt(r, sc.StartT), pointAt(r, sc.EndT)
	if pl != nil && pl.Corrected != nil {
		sc.StartMassM = pl.Corrected
	} else {
		sc.Unknown = append(sc.Unknown, "起点校正质量不可得")
	}
	if pr != nil && pr.Corrected != nil {
		sc.EndMassM = pr.Corrected
	} else {
		sc.Unknown = append(sc.Unknown, "终点校正质量不可得")
	}
	if sc.StartMassM != nil && sc.EndMassM != nil {
		ml := *sc.StartMassM - *sc.EndMassM
		sc.MassLossMg = &ml
		if m0 := currentMass0(r); m0 != nil && *m0 != 0 {
			pc := ml / *m0 * 100
			sc.MassLossPct = &pc
		}
	}
}

func pointAt(r *Result, t float64) *Point {
	for i := range r.Points {
		if r.Points[i].T == t {
			return &r.Points[i]
		}
	}
	return nil
}
