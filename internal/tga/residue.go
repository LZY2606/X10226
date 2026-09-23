package tga

import "fmt"

// buildResidues emits one final-residue result per gap-delimited segment plus,
// when the furnace program extends beyond the last observed segment, an
// explicit unknown residue across the trailing gap.
func buildResidues(rep *Report) {
	bySeg := splitTgByGaps(rep.Tg)
	for si, seg := range bySeg {
		last := seg[len(seg)-1]
		rid := HashID("res", map[string]any{"tg": last.ID, "seg": si})
		r := Residue{
			ID: rid, AtS: last.TimeS, AtTempC: last.TempC,
			InitialID: initialRef(rep), Status: "ok",
			Chain: []ChainStep{
				{Step: "pick", Text: fmt.Sprintf("取段 %d 最后一个干净采样点 @ %.0fs", si+1, last.TimeS),
					Refs: []ValueRef{{ID: last.ID, Label: "末端 TG", Value: fmt.Sprintf("%.3f%% @ %.0fs", last.TgPct, last.TimeS)}}},
			},
		}
		if rep.Initial != nil && rep.Initial.ItemID != "" {
			pct := last.TgPct
			mg := last.TgPct / 100 * rep.Initial.Mg
			r.Pct = &pct
			r.Mg = &mg
			r.Chain = append(r.Chain,
				ChainStep{Step: "drift", Text: "质量=样品原始质量-空白/漂移校正，TG%=校正质量/初重×100",
					Refs: []ValueRef{{ID: rep.Initial.ItemID, Label: "初重", Value: fmt.Sprintf("%.4f mg", rep.Initial.Mg)}}},
				ChainStep{Step: "result", Text: fmt.Sprintf("残留率=%.3f%%，残留质量=%.4f mg（不与各阶段失重做 100%% 闭合约束）", pct, mg),
					Refs: []ValueRef{{ID: rid, Label: "残留率", Value: fmt.Sprintf("%.3f%%", pct)}}},
			)
		} else {
			r.Status = "unknown"
			r.Reasons = append(r.Reasons, "初重缺失或不可转换，残留率不可计算")
		}
		if last.NoiseFlag {
			r.Status = "unknown"
			r.Reasons = append(r.Reasons, "末端点被标记为非单调噪声")
		}
		rep.Residues = append(rep.Residues, r)
	}

	// Trailing unknown residue across a final sampling gap / missing tail.
	progEnd := 0.0
	haveProg := len(rep.Program) > 0
	if haveProg {
		progEnd = rep.Program[len(rep.Program)-1].EndS
	}
	if len(rep.Tg) > 0 {
		lastObserved := rep.Tg[len(rep.Tg)-1].TimeS
		if haveProg && progEnd > lastObserved+1e-9 {
			rep.Residues = append(rep.Residues, Residue{
				ID: HashID("res_gap", map[string]any{"after": lastObserved, "end": progEnd}),
				AtS: progEnd, AtTempC: progTempAt(rep, progEnd),
				InitialID: initialRef(rep), Status: "gap",
				Reasons: []string{"炉程序结束晚于最后观测点，缺口处不做插值，最终残留率未知"},
				Chain: []ChainStep{{Step: "gap", Text: fmt.Sprintf("最后观测 %.0fs → 程序结束 %.0fs 之间为采样缺口，禁止跨越插值", lastObserved, progEnd)}},
			})
		}
	}
}

func initialRef(rep *Report) string {
	if rep.Initial != nil {
		return rep.Initial.ItemID
	}
	return ""
}

func progTempAt(rep *Report, t float64) float64 {
	for _, s := range rep.Program {
		if t >= s.StartS && t <= s.EndS {
			if s.EndS == s.StartS {
				return s.EndC
			}
			f := (t - s.StartS) / (s.EndS - s.StartS)
			return s.StartC + (s.EndC-s.StartC)*f
		}
	}
	if len(rep.Program) > 0 {
		return rep.Program[len(rep.Program)-1].EndC
	}
	return 0
}
