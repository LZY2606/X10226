package analyze

import (
	"fmt"

	"tgax/internal/model"
)

func (r *Result) finalize(p model.Params, _ map[string]string) *Result {
	// per-point value nodes: raw mass, corrected mass, DTG, sample temperature
	for i := range r.Points {
		pt := &r.Points[i]
		add := func(field, label, kind, unit, basis string, v *float64) {
			node := model.ValueNode{
				ID: ValID(r.SourceKey, pt.I, field), Label: fmt.Sprintf("%s @t=%.0fs", label, pt.T),
				Kind: kind, Current: true,
				SourceIDs: []string{"src:samples", "src:drift", "src:blank", r.ParamFP},
			}
			node.Quantity = nodeQ(v, unit, basis)
			r.Values = append(r.Values, node)
		}
		add("raw", "原始质量", "raw", "mg", "balance_raw", pt.RawM)
		add("corrected", "校正质量", "corrected", "mg", "corrected", pt.Corrected)
		add("dtg", "DTG", "derived", "mg/min", "dtg_central_diff", pt.DTG)
		if pt.TempF != nil {
			tv := *pt.TempF
			r.Values = append(r.Values, model.ValueNode{
				ID: ValID(r.SourceKey, pt.I, "temp"), Label: fmt.Sprintf("样品温度 @t=%.0fs", pt.T),
				Kind: "derived", Current: true, Quantity: qf(tv, "°C", "sample_temp_with_lag"),
				SourceIDs: []string{"src:furnace", paramValID(r.ParamFP, "temperature_lag_s")}})
		}
	}
	// parameter nodes
	pn := func(name, label, unit string, v float64) {
		r.Values = append(r.Values, model.ValueNode{ID: paramValID(r.ParamFP, name), Label: label, Kind: "parameter",
			Current: true, Quantity: qf(v, unit, "algorithm_param")})
	}
	pn("temperature_lag_s", "温度滞后", "s", p.TempLagS)
	for gas, d := range p.AtmosphereDelayS {
		pn("atmosphere_delay_"+gas, "气氛延迟:"+gas, "s", d)
	}
	pn("gap_factor", "缺口判定系数", "", p.GapFactor)
	pn("prominence_mg_per_min", "DTG峰显著性阈值", "mg/min", p.ProminenceMgPerMin)

	if len(r.Gaps) > 0 {
		// gap assertions already added; ensure gap id stable values
	}
	// residue chain values registry
	for _, res := range r.Residues {
		for _, s := range res.ChainSteps {
			found := false
			for _, v := range r.Values {
				if v.ID == s.ValueID {
					found = true
					break
				}
			}
			if !found {
				r.Values = append(r.Values, model.ValueNode{ID: s.ValueID, Label: s.Label, Kind: "result",
					Current: true, Quantity: s.Q, SourceIDs: s.Sources})
			}
		}
	}
	return r
}
