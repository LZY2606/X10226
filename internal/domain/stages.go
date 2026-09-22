package domain

import (
	"fmt"
	"sort"
)

// 阶段候选状态。
const (
	StageProposed = "proposed"
	StageAccepted = "accepted"
	StageRejected = "rejected"
	StageUnknown  = "unknown"
)

// StageCandidate 是一段失重阶段候选。阶段可重叠，不强制总和为 100%。
type StageCandidate struct {
	ID          string   `json:"id"`
	OnsetS      float64  `json:"onset_s"`
	EndS        float64  `json:"end_s"`
	PeakS       float64  `json:"peak_s"`
	PeakDTG     float64  `json:"peak_dtg_mg_per_s"`
	PeakTempC   float64  `json:"peak_temp_c"`
	MassLossPct float64  `json:"mass_loss_pct"` // 相对初重
	Overlaps    []string `json:"overlaps,omitempty"`
	Status      string   `json:"status"`
	Reasons     []string `json:"reasons,omitempty"` // 拒绝原因 / 未知原因，一等数据
	Segment     int      `json:"segment"`
}

// StageSet 是阶段候选集合及判定。
type StageSet struct {
	Candidates []StageCandidate `json:"candidates"`
	SumPct     float64          `json:"sum_pct"`
	InitialMg  float64          `json:"initial_mg"`
}

// FindStageCandidates 在 DTG 上找失重峰并生成阶段候选。
// 规则：非单调噪声用平滑+显著度容忍；重叠峰各自保留；
// 被缺口或数据末端截断的峰状态为 unknown 并保留全部合法候选。
func FindStageCandidates(dtg *DTGCurve, cc *CorrectedCurve, segs []Segment, initialMg float64, p Params) (*StageSet, []Judgment) {
	set := &StageSet{InitialMg: initialMg}
	var judgments []Judgment
	if len(dtg.Points) == 0 || initialMg <= 0 {
		return set, nil
	}
	pts := usablePoints(cc)
	massAt := func(t float64) (float64, bool) {
		// 段内二分查找；不跨缺口插值。
		seg := -1
		for _, s := range segs {
			if t >= s.StartT && t <= s.EndT {
				seg = s.Index
				break
			}
		}
		if seg < 0 {
			return 0, false
		}
		s := segs[seg]
		lo, hi := s.StartIdx, s.EndIdx
		for lo < hi {
			mid := (lo + hi) / 2
			if pts[mid].T < t {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo > s.StartIdx && pts[lo].T > t {
			a, b := pts[lo-1], pts[lo]
			f := (t - a.T) / (b.T - a.T)
			return a.Mass + f*(b.Mass-a.Mass), true
		}
		return pts[lo].Mass, true
	}
	tempAt := func(t float64) float64 {
		best, bd := 0.0, -1.0
		for _, pt := range pts {
			d := pt.T - t
			if d < 0 {
				d = -d
			}
			if bd < 0 || d < bd {
				bd, best = d, pt.Temp
			}
		}
		return best
	}
	// 阈值：显著度换算 mg/s。
	thr := p.ProminencePctMin / 100 * initialMg / 60
	// 按段扫描峰。
	bySeg := map[int][]DTGPoint{}
	for _, dp := range dtg.Points {
		bySeg[dp.Segment] = append(bySeg[dp.Segment], dp)
	}
	noiseNote := false
	for _, seg := range segs {
		sp := bySeg[seg.Index]
		if len(sp) < 5 {
			continue
		}
		// 非单调噪声检查：原始差分序列的符号翻转次数。
		flips := 0
		for i := 2; i < len(sp); i++ {
			a := sp[i-1].DTG - sp[i-2].DTG
			b := sp[i].DTG - sp[i-1].DTG
			if a*b < 0 {
				flips++
			}
		}
		if flips > len(sp)/4 && !noiseNote {
			noiseNote = true
			judgments = append(judgments, Judgment{
				Code:    JNoiseTolerated,
				Subject: fmt.Sprintf("segment-%d", seg.Index),
				Detail: map[string]any{
					"sign_flips": flips,
					"rule":       "non-monotonic noise tolerated via smoothing window and prominence threshold; naive extrema not adopted",
				},
			})
		}
		// 找显著峰：局部最大且高于阈值。
		for i := 1; i < len(sp)-1; i++ {
			if sp[i].DTG < thr || sp[i].DTG < sp[i-1].DTG || sp[i].DTG < sp[i+1].DTG {
				continue
			}
			// 峰窗口：向两侧扩展到 DTG 回落到峰值的 10% 或阈值以下。
			floor := sp[i].DTG * 0.1
			if floor < thr*0.5 {
				floor = thr * 0.5
			}
			lo, hi := i, i
			truncLo, truncHi := false, false
			for lo > 0 && sp[lo].DTG > floor {
				lo--
			}
			if lo == 0 && sp[0].DTG > floor {
				truncLo = true
			}
			for hi < len(sp)-1 && sp[hi].DTG > floor {
				hi++
			}
			if hi == len(sp)-1 && sp[len(sp)-1].DTG > floor {
				truncHi = true
			}
			onset, end := sp[lo].T, sp[hi].T
			if end-onset < p.MinStageWidthS && !truncLo && !truncHi {
				continue // 太窄，视为噪声
			}
			m0, ok0 := massAt(onset)
			m1, ok1 := massAt(end)
			cand := StageCandidate{
				ID:         fmt.Sprintf("stage-%d-p%02d", seg.Index, i),
				OnsetS:     onset,
				EndS:       end,
				PeakS:      sp[i].T,
				PeakDTG:    sp[i].DTG,
				PeakTempC:  tempAt(sp[i].T),
				Status:     StageProposed,
				Segment:    seg.Index,
			}
			if ok0 && ok1 {
				cand.MassLossPct = (m0 - m1) / initialMg * 100
			}
			if truncLo || truncHi {
				cand.Status = StageUnknown
				side := "onset"
				if truncHi {
					side = "end"
				}
				cand.Reasons = append(cand.Reasons,
					"peak truncated by data gap or run end at "+side+"; boundary cannot be uniquely determined",
					"all legal candidate boundaries retained; no interpolation across the gap")
				judgments = append(judgments, Judgment{
					Code:    JUnknownCandidate,
					Subject: cand.ID,
					Detail: map[string]any{
						"truncated_side": side,
						"rule":           "when a stage cannot be uniquely decided, all legal candidates are kept as unknown",
					},
				})
			}
			set.Candidates = append(set.Candidates, cand)
		}
	}
	// 重叠检测：窗口相交即重叠，各自保留。
	for i := range set.Candidates {
		for j := i + 1; j < len(set.Candidates); j++ {
			a, b := set.Candidates[i], set.Candidates[j]
			if a.OnsetS < b.EndS && b.OnsetS < a.EndS {
				set.Candidates[i].Overlaps = append(set.Candidates[i].Overlaps, b.ID)
				set.Candidates[j].Overlaps = append(set.Candidates[j].Overlaps, a.ID)
			}
		}
	}
	for _, c := range set.Candidates {
		if len(c.Overlaps) > 0 {
			judgments = append(judgments, Judgment{
				Code:    JStageOverlap,
				Subject: c.ID,
				Detail: map[string]any{
					"overlaps_with": c.Overlaps,
					"rule":          "stages may overlap; overlapping candidates are kept individually, not merged",
				},
			})
		}
	}
	sum := 0.0
	for _, c := range set.Candidates {
		sum += c.MassLossPct
	}
	set.SumPct = sum
	judgments = append(judgments, Judgment{
		Code:    JSumNotForced,
		Subject: "stage-set",
		Detail: map[string]any{
			"sum_pct": sum,
			"rule":    "stage mass losses are not forced to sum to 100%; overlap and residue make the sum informational only",
		},
	})
	sort.Slice(set.Candidates, func(i, j int) bool { return set.Candidates[i].OnsetS < set.Candidates[j].OnsetS })
	return set, judgments
}

// Residue 是最终残留率结果，计算链可展开。
type Residue struct {
	Name        string  `json:"name"` // "overall" 或 "segment-N"
	FinalMassMg float64 `json:"final_mass_mg"`
	InitialMg   float64 `json:"initial_mg"`
	ResiduePct  float64 `json:"residue_pct"`
	AtS         float64 `json:"at_s"`
	Basis       string  `json:"basis"`
}

// ResidueSet 保存全部残留率。
type ResidueSet struct {
	Residues []Residue `json:"residues"`
}

// ComputeResidues 计算整体与逐段残留率。
func ComputeResidues(cc *CorrectedCurve, segs []Segment, im *InitialMass) (*ResidueSet, []Diagnostic) {
	rs := &ResidueSet{}
	var diags []Diagnostic
	if im == nil {
		return rs, []Diagnostic{{Code: "initial_mass_missing", Message: "initial mass input missing; residue not computable"}}
	}
	m0, d := MustCanonical(im.Mass, DimMass, "initial_mass")
	if d != nil {
		diags = append(diags, *d)
		return rs, diags
	}
	pts := usablePoints(cc)
	if len(pts) == 0 || m0 <= 0 {
		return rs, diags
	}
	last := pts[len(pts)-1]
	rs.Residues = append(rs.Residues, Residue{
		Name: "overall", FinalMassMg: last.Mass, InitialMg: m0,
		ResiduePct: last.Mass / m0 * 100, AtS: last.T, Basis: im.Basis,
	})
	for _, s := range segs {
		end := pts[s.EndIdx]
		rs.Residues = append(rs.Residues, Residue{
			Name: fmt.Sprintf("segment-%d", s.Index), FinalMassMg: end.Mass, InitialMg: m0,
			ResiduePct: end.Mass / m0 * 100, AtS: end.T, Basis: im.Basis,
		})
	}
	return rs, diags
}
