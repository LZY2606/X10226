package engine

import (
	"fmt"

	"tga/internal/model"
)

func (e *Engine) initialMassArtifact(m0, samples *model.Submission) *model.Artifact {
	a := &model.Artifact{
		Kind: "initial_mass", Title: "样品初重（独立称量层）",
		Algorithm: AlgorithmVersion, ParamFP: "none",
		Sources:    []model.SourceRef{e.srcRef(m0, "initial_mass")},
		CreatedSeq: m0.EventSeq, CreatedAt: m0.CreatedAt,
	}
	a.ID = e.hashID("initial_mass", fmt.Sprint(m0.EventSeq))
	state := "ok"
	diags := []model.Diag{}
	if m0.M0 == nil || !m0.M0.ValueOK {
		state = "unknown"
		diags = append(diags, model.Diag{Code: "M0_UNCONVERTIBLE", Severity: model.SevError, Message: "初重值/单位无法转换，保留原始表示"})
	}
	val := 0.0
	raw := ""
	if m0.M0 != nil {
		val, raw = m0.M0.ValueMg, m0.M0.ValueRaw
	}
	a.Quantities = []model.Quantity{
		{Name: "m0", Label: "称量初重", Value: val, Unit: "mg", State: state,
			Display: fmt.Sprintf("%s（规范化 %.4f mg）", raw, val)},
	}
	a.Steps = []model.Step{
		{Label: "原始表示", Result: fmt.Sprintf("%q 单位=%q", raw, unitOrNone(m0))},
		{Label: "规范化", Formula: "mg", Result: fmt.Sprintf("%.4f mg", val)},
	}
	a.Diags = diags
	if m0.ParentID != "" {
		a.Diags = append(a.Diags, model.Diag{Code: "M0_REVISED", Severity: model.SevInfo, Message: "初重已修订：新版本以父版本关系表达，旧值不可覆盖"})
	}
	return a
}

func unitOrNone(m0 *model.Submission) string {
	if m0.M0 == nil {
		return ""
	}
	return m0.M0.Unit
}

func (e *Engine) residueArtifacts(cs *model.Artifact, stages []*model.Artifact, m0a *model.Artifact, params *model.Params) []*model.Artifact {
	rows := cs.Data["rows"].([]CorrectedPoint)
	var lastValid *CorrectedPoint
	for i := range rows {
		if rows[i].Valid {
			r := rows[i]
			lastValid = &r
		}
	}
	if lastValid == nil || m0a.Quantities[0].State == "unknown" {
		return nil
	}
	m0 := m0a.Quantities[0].Value
	finalMg := lastValid.CorrMg
	pct := 100 * finalMg / m0
	stagesCombo := comboSet(stages)
	var out []*model.Artifact
	for _, combo := range stagesCombo {
		a := &model.Artifact{
			Kind: "residue", Title: fmt.Sprintf("最终残留率 · 阶段组合 %s", combo),
			Algorithm: AlgorithmVersion, ParamFP: params.Fingerprint,
			Sources: []model.SourceRef{
				artRef(cs, "corrected_series"),
				artRef(m0a, "initial_mass"),
			},
			CreatedSeq: cs.CreatedSeq, CreatedAt: cs.CreatedAt,
		}
		for _, stg := range stages {
			if fmt.Sprintf("%d/%d", int(stg.Data["combo_index"].(int))+1, int(stg.Data["combo_count"].(int))) == combo ||
				(combo == "唯一" && int(stg.Data["combo_count"].(int)) == 1) {
				a.Sources = append(a.Sources, artRef(stg, "stage_candidate"))
			}
		}
		a.ID = e.hashID("residue", cs.ID, m0a.ID, combo)
		sumLoss := 0.0
		sumState := "ok"
		for _, stg := range stages {
			inCombo := fmt.Sprintf("%d/%d", int(stg.Data["combo_index"].(int))+1, int(stg.Data["combo_count"].(int))) == combo
			if combo == "唯一" && int(stg.Data["combo_count"].(int)) == 1 {
				inCombo = true
			}
			if inCombo {
				if stg.Data["loss_state"] == "unknown" {
					sumState = "unknown"
				} else {
					sumLoss += stg.Data["loss_mg"].(float64)
				}
			}
		}
		state := "ok"
		lossDisplay := fmt.Sprintf("%.4f mg", sumLoss)
		if sumState == "unknown" {
			state = "unknown"
			lossDisplay = "部分阶段跨越缺口，失重未知"
		}
		a.Quantities = []model.Quantity{
			{Name: "final_mass", Label: "终点校正质量", Value: finalMg, Unit: "mg", State: "ok", Display: fmt.Sprintf("%.4f mg @ %.0f s", finalMg, lastValid.TimeSec)},
			{Name: "total_loss", Label: "各阶段失重合计", Value: sumLoss, Unit: "mg", State: state, Display: lossDisplay},
			{Name: "residue_pct", Label: "最终残留率", Value: pct, Unit: "%", State: state, Display: fmt.Sprintf("%.2f %%", pct)},
		}
		a.Steps = []model.Step{
			{Label: "1. 原始", Inputs: []model.StepInput{{Label: "终点原始质量", Display: fmt.Sprintf("%.4f mg", lastValid.RawMg)}}, Result: "来自最后一个有效采样点"},
			{Label: "2. 校正", Formula: "m_corr = m_raw - b(t)", Inputs: []model.StepInput{{Label: "校正序列", ArtifactID: cs.ID, Display: cs.ID}}, Result: fmt.Sprintf("%.4f mg", finalMg)},
			{Label: "3. 初重", Inputs: []model.StepInput{{Label: "独立称量层", ArtifactID: m0a.ID, Display: m0a.ID}}, Result: fmt.Sprintf("m0 = %.4f mg", m0)},
			{Label: "4. 残留率", Formula: "100 × m_corr(end) / m0", Result: fmt.Sprintf("%.2f %%（可与 100%% - Σ阶段失重 交叉核对，阶段可重叠故两者不强制相等）", pct)},
		}
		a.Data = map[string]any{
			"final_mass_mg": finalMg, "end_time_sec": lastValid.TimeSec,
			"residue_pct": pct, "m0_mg": m0, "combo": combo,
			"sum_stage_loss_mg": sumLoss,
		}
		out = append(out, a)
	}
	return out
}

func comboSet(stages []*model.Artifact) []string {
	seen := map[string]bool{}
	var out []string
	for _, stg := range stages {
		n := int(stg.Data["combo_count"].(int))
		var label string
		if n == 1 {
			label = "唯一"
		} else {
			label = fmt.Sprintf("%d/%d", int(stg.Data["combo_index"].(int))+1, n)
		}
		if !seen[label] {
			seen[label] = true
			out = append(out, label)
		}
	}
	return out
}
