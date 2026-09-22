package derive

import (
	"fmt"

	"tga-review/internal/domain"
)

func (e *Engine) runResidue(cn *domain.CorrectNode, dn *domain.DTGNode, sn *domain.StagesNode) *domain.ResidueNode {
	p := e.params
	rn := &domain.ResidueNode{Params: p}
	m0 := e.m0Mg()
	if m0 == nil {
		rn.Diags = append(rn.Diags, domain.Diagnostic{Code: "NO_M0", Msg: "initial mass unknown; residue cannot be computed"})
		return rn
	}
	for si, seg := range cn.Segs {
		last := cn.Points[seg.EndIdx]
		if !last.Valid || last.CorrectedMg == nil {
			continue
		}
		final := *last.CorrectedMg
		r := domain.Residue{
			RID:     fmt.Sprintf("res-%d", si+1),
			TimeS:   round2(last.TimeS),
			TempC:   round2(tempOfPoint(last)),
			FinalMg: final,
			M0Mg:    *m0,
			Status:  "ok",
		}
		pct := round4(final / *m0 * 100)
		r.ResiduePct = &pct
		blank, drift := "", ""
		if last.BlankMg != nil {
			blank = fmt.Sprintf("%.4f", *last.BlankMg)
		} else {
			blank = "none"
		}
		if last.DriftMg != nil {
			drift = fmt.Sprintf("%.4f", *last.DriftMg)
		} else {
			drift = "none"
		}
		rawTok := "raw"
		if last.RawMassMg != nil {
			rawTok = fmt.Sprintf("%.4f", *last.RawMassMg)
		}
		m0Ref := domain.Ref{Kind: "raw", Label: "样品初重"}
		if im := e.optional(domain.KindInitMass); im != nil {
			m0Ref.ID = im.ID
			m0Ref.Ver = im.Version
		}
		r.Chain = []domain.ChainStep{
			{ID: r.RID + "-m0", Label: "样品初重 m0", ValueS: fmt.Sprintf("%.4f mg", *m0), Value: *m0, Unit: "mg", Ref: m0Ref},
			{ID: r.RID + "-raw", Label: "末点原始质量", ValueS: rawTok + " mg", Value: rawVal(last), Unit: "mg", Ref: domain.Ref{Kind: "raw", Label: domain.KindSamples.Label()}},
			{ID: r.RID + "-blank", Label: "空白曲线扣除", ValueS: blank + " mg", Value: optVal(last.BlankMg), Unit: "mg", Ref: domain.Ref{Kind: "raw", Label: domain.KindBlank.Label()}},
			{ID: r.RID + "-drift", Label: "天平漂移校正", ValueS: drift + " mg", Value: optVal(last.DriftMg), Unit: "mg", Ref: domain.Ref{Kind: "raw", Label: domain.KindDrift.Label()}},
			{ID: r.RID + "-final", Label: "校正末点质量 m_f", ValueS: fmt.Sprintf("%.4f mg", final), Value: final, Unit: "mg", Ref: domain.Ref{Kind: "node", Label: domain.AlgoCorrect}},
			{ID: r.RID + "-pct", Label: "残留率 = m_f / m0 × 100", ValueS: fmt.Sprintf("%.4f %%", pct), Value: pct, Unit: "%", Ref: domain.Ref{Kind: "node", Label: domain.AlgoResidue}},
		}
		if last.GapBefore {
			r.Status = "unknown"
			r.UnknownReason = "TAIL_AFTER_GAP: final point is on the far side of an un-interpolated gap"
		}
		rn.Residues = append(rn.Residues, r)
	}
	if len(rn.Residues) > 0 && m0 != nil {
		tot := 100 - *rn.Residues[len(rn.Residues)-1].ResiduePct
		rn.TotalLossPct = &tot
	}
	return rn
}

func tempOfPoint(cp domain.CorrectPoint) float64 {
	if cp.SampleC != nil {
		return *cp.SampleC
	}
	if cp.FurnaceC != nil {
		return *cp.FurnaceC
	}
	return 0
}
func optVal(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}
func rawVal(cp domain.CorrectPoint) float64 {
	if cp.RawMassMg == nil {
		return 0
	}
	return *cp.RawMassMg
}
