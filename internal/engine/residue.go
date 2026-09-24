package engine

import (
	"fmt"

	"tgaline/internal/domain"
)

type residueResult struct {
	residues []domain.Residue
}

// finalResidue computes the residual mass ratio over the last residue_window_s
// of the run (mean corrected mass / initial mass), with a transparent chain.
// It is unknown when m0 is missing or the averaging window crosses a gap.
func finalResidue(s *domain.Series, in layerInput, p map[string]float64,
	corrNodeID, gapNodeID, noiseNodeID, tgNodeID string) residueResult {

	var out []domain.Residue
	if len(s.TimeS) == 0 {
		return residueResult{}
	}
	endT := s.TimeS[len(s.TimeS)-1]
	win := p["residue_window_s"]
	startT := endT - win

	avg, known, reasons := meanMassInWindow(s, startT, endT)
	at := domain.Residue{
		ID:  residueID(round4(endT), m0Tag(in)),
		AtS: endT, AtTK: s.TK[len(s.TK)-1],
		Chain: []domain.ChainStep{
			{Kind: "source", Label: "原始采样点", Detail: "选定运行末尾的样品测量点", SourceRef: layerRef(domain.LayerSample, in)},
			{Kind: "node", NodeID: corrNodeID, Label: "漂移+空白校正后的质量",
				Detail: fmt.Sprintf("在 [%.1f, %.1f] s 窗口内取已测校正质量均值", startT, endT),
				Value:  fmt.Sprintf("m_end = %.4f mg (窗口均值)", avg)},
			{Kind: "node", NodeID: gapNodeID, Label: "采样缺口掩码", Detail: "确认末尾窗口未跨越数据缺口；缺口不插值"},
			{Kind: "node", NodeID: noiseNodeID, Label: "非单调噪声报告", Detail: "非单调跳变仅标记不改写质量"},
		},
	}

	if in.m0 == nil {
		known = false
		reasons = append(reasons, "没有可用的样品初重分层，残留率无分母，保持未知")
	} else {
		at.Chain = append(at.Chain, domain.ChainStep{
			Kind: "source", Label: "样品初重（分层保存）",
			Detail:    fmt.Sprintf("采用初重 %s (source=%s)", fmtQ(in.m0.mg, "mg"), in.m0.source),
			Value:     fmt.Sprintf("m0 = %.4f mg", in.m0.mg),
			SourceRef: fmt.Sprintf("%s#%d", in.m0.layerID, in.m0.rowNo),
		})
	}
	at.Chain = append(at.Chain, domain.ChainStep{
		Kind: "node", NodeID: tgNodeID, Label: "归一化 TG",
		Detail: "R = mean(m_corr 末尾窗口) / m0",
	})

	if known && in.m0 != nil && in.m0.mg > 0 {
		val := avg / in.m0.mg
		pct := val * 100.0
		at.Value = &val
		at.ValuePct = &pct
		at.Status = "ok"
		at.Chain[len(at.Chain)-1].Value = fmt.Sprintf("R = %.4f mg / %.4f mg = %.2f%%", avg, in.m0.mg, pct)
	} else {
		at.Status = "unknown"
		at.UnknownReasons = reasons
	}
	out = append(out, at)
	return residueResult{residues: out}
}

func meanMassInWindow(s *domain.Series, a, b float64) (float64, bool, []string) {
	sum, n := 0.0, 0
	gapSet := map[int]bool{}
	for _, g := range s.GapAfter {
		gapSet[g] = true
	}
	i0, i1 := -1, -1
	for i := range s.TimeS {
		if !s.Mask[i] {
			continue
		}
		if s.TimeS[i] >= a && s.TimeS[i] <= b {
			if i0 == -1 {
				i0 = i
			}
			i1 = i
			sum += s.MassMG[i]
			n++
		}
	}
	var reasons []string
	if i0 >= 0 {
		for g := range gapSet {
			if g >= i0 && g < i1 {
				reasons = append(reasons, "末尾平均窗口跨越采样缺口，缺口不插值")
			}
		}
	}
	if n == 0 {
		return 0, false, append(reasons, "末尾窗口内没有已测质量点")
	}
	return sum / float64(n), len(reasons) == 0, reasons
}

func m0Tag(in layerInput) string {
	if in.m0 == nil {
		return "m0:unknown"
	}
	return fmt.Sprintf("m0:%s@%s#%d", fmtQ(in.m0.mg, "mg"), in.m0.layerID, in.m0.rowNo)
}

func fmtQ(v float64, unit string) string { return fmt.Sprintf("%.4f %s", v, unit) }

func layerRef(k domain.LayerKind, in layerInput) string {
	return string(k)
}
