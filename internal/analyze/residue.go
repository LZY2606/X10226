package analyze

import (
	"fmt"

	"tgax/internal/model"
)

// ValID returns the stable identity of a per-point derived/raw display value.
func ValID(sourceKey string, pointIndex int, field string) string {
	return hid("val", sourceKey, "point", pointIndex, field)
}

func mass0ValID(sourceKey string) string { return hid("val", sourceKey, "mass0") }
func paramValID(fp, name string) string  { return hid("val", fp, "param", name) }

func currentMass0(r *Result) *float64 { return r.Mass0Mg }

func computeResidues(r *Result, mass *model.InputVersion) []Residue {
	var out []Residue
	var m0 *float64
	if mass != nil && mass.Mass0 != nil && mass.Mass0.M.Value != nil {
		m0 = mass.Mass0.M.Value
	}
	r.Mass0Mg = m0

	m0Node := model.ValueNode{
		ID: mass0ValID(r.SourceKey), Label: "样品初重 m0", Kind: "raw", Current: true,
		SourceIDs: []string{srcOf("mass", mass)},
	}
	if m0 != nil {
		m0Node.Quantity = qf(*m0, "mg", "sample_initial_mass", "mass0")
	} else {
		m0Node.Quantity = model.Quantity{Status: "unknown", Unit: "mg", Basis: "sample_initial_mass", Diagnostic: "初重缺失或不可转换"}
	}
	r.Values = append(r.Values, m0Node)

	mkChain := func(p Point) []ChainStep {
		steps := []ChainStep{
			{ValueID: mass0ValID(r.SourceKey), Label: "样品初重 m0", Expr: "m0",
				Q: nodeQ(m0, "mg", "sample_initial_mass"), Sources: []string{srcOf("mass", mass)}},
			{ValueID: ValID(r.SourceKey, p.I, "raw"), Label: "天平原始读数 m_raw", Expr: "m_raw(t)",
				Q: nodeQ(p.RawM, "mg", "balance_raw"), Sources: []string{"src:samples"}},
		}
		if p.DriftM != nil {
			steps = append(steps, ChainStep{ValueID: ValID(r.SourceKey, p.I, "drift"), Label: "天平漂移校正量", Expr: "rate·(t−t0)",
				Q: nodeQ(p.DriftM, "mg", "drift_model"), Sources: []string{"src:drift"}})
		}
		if p.BlankM != nil {
			steps = append(steps, ChainStep{ValueID: ValID(r.SourceKey, p.I, "blank"), Label: "空白曲线扣除量", Expr: "b(Ts)−b0",
				Q: nodeQ(p.BlankM, "mg", "blank_curve"), Sources: []string{"src:blank"}})
		}
		steps = append(steps, ChainStep{ValueID: ValID(r.SourceKey, p.I, "corrected"), Label: "校正质量 m_corr",
			Expr: "m_raw − drift − blank_rel", Q: nodeQ(p.Corrected, "mg", "corrected"),
			Sources: []string{"src:samples", "src:drift", "src:blank", paramValID(r.ParamFP, "temperature_lag_s")}},
			ChainStep{ValueID: hid("val", r.SourceKey, "residue-at", p.I), Label: "残留率",
				Expr: "m_corr / m0 × 100%", Q: model.Quantity{Unit: "%", Basis: "residue_vs_initial_mass", Status: "ok"},
				Sources: []string{ValID(r.SourceKey, p.I, "corrected"), mass0ValID(r.SourceKey)}})
		return steps
	}

	// one residue after each accepted-or-pending stage endpoint
	seen := map[float64]bool{}
	for _, c := range r.Candidates {
		if seen[c.EndT] {
			continue
		}
		seen[c.EndT] = true
		pr := pointAt(r, c.EndT)
		if pr == nil {
			continue
		}
		res := Residue{ID: hid("res", r.SourceKey, r.ParamFP, "stage-end", c.EndT),
			Label: fmt.Sprintf("阶段后残留率 (t=%.0fs 峰 t=%.0fs)", c.EndT, c.PeakT),
			AtT:   &pr.T, AtTempC: pr.TempF, MassMg: pr.Corrected, Mass0Mg: m0,
			ChainSteps: mkChain(*pr), Current: true,
			SourceIDs: []string{ValID(r.SourceKey, pr.I, "corrected"), mass0ValID(r.SourceKey), c.ID}}
		if m0 != nil && pr.Corrected != nil {
			pc := *pr.Corrected / *m0 * 100
			res.Percent = &pc
		} else {
			res.Unknown = append(res.Unknown, "初重或终点校正质量缺失，残留率无法唯一计算")
		}
		if c.BoundedByGap {
			res.Unknown = append(res.Unknown, "终点邻接采样缺口")
		}
		out = append(out, res)
	}

	// final residue at last usable point (no interpolation to a later time)
	if len(r.Points) > 0 {
		last := r.Points[len(r.Points)-1]
		if last.Corrected != nil {
			res := Residue{ID: hid("res", r.SourceKey, r.ParamFP, "final"),
				Label: "最终残留率", AtT: &last.T, AtTempC: last.TempF, MassMg: last.Corrected, Mass0Mg: m0,
				ChainSteps: mkChain(last), Current: true,
				SourceIDs: []string{ValID(r.SourceKey, last.I, "corrected"), mass0ValID(r.SourceKey)}}
			if m0 != nil {
				pc := *last.Corrected / *m0 * 100
				res.Percent = &pc
			} else {
				res.Unknown = append(res.Unknown, "初重缺失，最终残留率无法计算")
			}
			if last.GapBefore {
				res.Unknown = append(res.Unknown, "最后一个可用点位于采样缺口之后")
			}
			out = append(out, res)
		}
	}
	return out
}

func nodeQ(v *float64, unit, basis string) model.Quantity {
	if v == nil {
		return model.Quantity{Status: "unknown", Unit: unit, Basis: basis}
	}
	return qf(*v, unit, basis, "")
}

func srcOf(kind string, v *model.InputVersion) string {
	if v == nil {
		return "src:" + kind + ":missing"
	}
	return v.ID
}
