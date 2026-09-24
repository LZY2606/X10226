package derive

import (
	"fmt"

	"tga-review/internal/domain"
)

// buildResidue computes the final residue percentage and, more importantly, an
// expandable chain that cites every layer version and parameter version used.
func buildResidue(series *Series, st *State, applied []Candidate, atSeq int64) *ResidueReport {
	points := series.Points
	last := points[len(points)-1]

	r := &ResidueReport{
		ID:          "residue-final",
		FinalS:      last.TS,
		FinalTempC:  last.TempC,
		FinalMassMg: last.MassMg,
		Sources:     append([]string{}, series.Sources...),
		ComputedSeq: atSeq,
	}

	m0 := 0.0
	m0Known := false
	if st.InitialMass != nil && st.InitialMass.Known {
		m0 = st.InitialMass.MassMg
		m0Known = true
	}

	chain := []domain.ChainLink{{
		Order: 1, Kind: "initial_mass",
		Description: "样品初重（版本化分层）",
		Value:       fmt.Sprintf("%.4f", m0), Unit: "mg",
		SourceIDs: []string{st.InitialMass.StableID},
		Unknown:   !m0Known,
		Note:      "初重修订以新版本表达，旧版本保留",
	}}
	if st.InitialMass != nil {
		chain[0].Inputs = map[string]any{
			"raw_text": st.InitialMass.Raw.Text,
			"raw_unit": st.InitialMass.Raw.Unit,
			"version":  st.InitialMass.Version,
		}
	}

	blankDesc := "空白曲线未用于校正"
	var blankSrc []string
	if st.Drift != nil && st.Drift.Model == "blank_zeroed" {
		blankDesc = "天平基线漂移：空白曲线零对齐后逐点扣除（仅在空白量程内插值）"
		if v, ok := st.RawVersions[domain.LayerBlank]; ok {
			blankSrc = append(blankSrc, v.StableID)
		}
		if v, ok := st.RawVersions[domain.LayerDrift]; ok {
			blankSrc = append(blankSrc, v.StableID)
		}
	}
	chain = append(chain, domain.ChainLink{
		Order: 2, Kind: "drift_correction",
		Description: blankDesc,
		SourceIDs:   blankSrc,
	})

	gapNote := "无采样缺口"
	if len(series.Gaps) > 0 {
		gapNote = fmt.Sprintf("存在 %d 个采样缺口；缺口两侧不插值，DTG 分段计算", len(series.Gaps))
	}
	chain = append(chain, domain.ChainLink{
		Order: 3, Kind: "gap_policy",
		Description: gapNote,
		Inputs:      map[string]any{"gaps": series.Gaps},
		Note:        "数据缺口不跨越插值",
	})

	chain = append(chain, domain.ChainLink{
		Order:       4,
		Kind:        "final_mass",
		Description: "炉程序末端校正后质量",
		Value:       fmt.Sprintf("%.4f", last.MassMg),
		Unit:        "mg",
		Inputs: map[string]any{
			"t_s":       last.TS,
			"temp_c":    last.TempC,
			"series_id": series.ID,
		},
		SourceIDs: []string{series.ID},
	})

	if !m0Known || m0 == 0 {
		r.Unknown = true
		r.ResiduePct = 0
		r.Reasons = append(r.Reasons, "初重未知，残留率无法唯一决定")
		chain = append(chain, domain.ChainLink{
			Order: 5, Kind: "residue_pct", Unknown: true,
			Description: "残留率 = 末质量 / 初重 × 100%，因初重未知标记为 unknown",
		})
	} else {
		r.ResiduePct = last.MassMg / m0 * 100
		chain = append(chain, domain.ChainLink{
			Order: 5, Kind: "residue_pct",
			Description: "残留率 = 末质量 / 初重 × 100%",
			Value:       fmt.Sprintf("%.3f", r.ResiduePct),
			Unit:        "%",
			Inputs: map[string]any{
				"formula":     "residue_pct = m_final / m0 * 100",
				"algo":        AlgoVersion,
				"fingerprint": series.FP,
			},
		})
	}

	// Accepted/adjusted stages are evidence, but their losses are never forced
	// to add with the residue to exactly 100%.
	var acceptedPct float64
	var acceptedIDs []string
	for _, c := range applied {
		if c.Status == CandStatusAccepted || c.Status == CandStatusAdjusted {
			acceptedPct += c.LossPct
			acceptedIDs = append(acceptedIDs, c.ID)
		}
	}
	chain = append(chain, domain.ChainLink{
		Order: 6, Kind: "stage_sum_check",
		Description: "已接受阶段失重合计（可重叠，不强制与残留率合计为 100%）",
		Value:       fmt.Sprintf("%.3f", acceptedPct),
		Unit:        "%",
		SourceIDs:   acceptedIDs,
		Note:        "阶段可重叠；sum(stage losses)+residue 不必等于 100%",
	})

	r.Chain = chain
	return r
}
