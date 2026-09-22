package main

import "encoding/json"

// Mutation is a single-source change applied to the baseline. Each mutation
// touches exactly one direct source so tests can assert the blast radius is
// limited to results that depend on it.
type Mutation struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Scenario       string     `json:"scenario"`
	ChangesDirect  string     `json:"changes_direct_source"`
	ExpectedStatus string     `json:"expected_status"`
	ExpectRule     string     `json:"expect_rule_code"`
	Layer          layerPatch `json:"layer"`
}

type layerPatch struct {
	Kind       string          `json:"kind"`
	ParentFrom string          `json:"parent_kind"` // replace that layer's head
	Data       json.RawMessage `json:"data"`
	// bad envelope toggle for the half-failure case
	FatalStructure bool        `json:"fatal_structure,omitempty"`
	ExtraLayer     *extraPatch `json:"extra_layer,omitempty"`
}

type extraPatch struct {
	Kind       string          `json:"kind"`
	ParentFrom string          `json:"parent_kind"`
	Data       json.RawMessage `json:"data"`
}

func writeMutations(outDir string) {
	muts := []Mutation{
		{
			ID: "M1_atmosphere_delay", Title: "气氛延迟",
			Scenario:       "只修订参数：气氛传输延迟与热滞后使阀切换时刻不等于样品反应时刻",
			ChangesDirect:  "params.atmosphere_delay_s",
			ExpectedStatus: "committed", ExpectRule: "R1",
			Layer: layerPatch{Kind: "__params__", Data: rawMap(map[string]any{
				"atmosphere_delay_s": json.Number("30"),
				"reason":             "复核气体输送管长度后修订延迟（不改原始阀事件）",
			})},
		},
		{
			ID: "M2_baseline_drift", Title: "天平基线漂移",
			Scenario:       "只改漂移校正直接来源：速率翻倍，只影响 m_corr/DTG/残留链，原始采样点不变",
			ChangesDirect:  "drift_correction.rate_mg_per_min",
			ExpectedStatus: "committed", ExpectRule: "R7",
			Layer: layerPatch{Kind: "drift_correction", ParentFrom: "drift_correction",
				Data: rawMap(map[string]any{
					"basis":            "m_balance_mg",
					"rate_mg_per_min":  json.Number("0.0240"),
					"reference_time_s": json.Number("0"),
					"note":             "基线斜率修订（单一直接来源）",
				})},
		},
		{
			ID: "M3_overlapping_peaks", Title: "重叠峰",
			Scenario:       "只改峰识别参数：提高窗宽使中段重叠为多峰，保留全部合法 merged/split 候选",
			ChangesDirect:  "params.dtg_window_s",
			ExpectedStatus: "committed", ExpectRule: "R3",
			Layer: layerPatch{Kind: "__params__", Data: rawMap(map[string]any{
				"dtg_window_s": json.Number("34"),
				"reason":       "重叠峰复核：保留多个合法分解，不强制总和 100%",
			})},
		},
		{
			ID: "M4_nonmonotonic_noise", Title: "非单调噪声",
			Scenario:       "只改一个采样点：注入小的反向跳动，原始点保留并在派生层标记，不删除不平滑",
			ChangesDirect:  "sample_points[row@300s].mass_mg",
			ExpectedStatus: "committed", ExpectRule: "R5",
			Layer: layerPatch{Kind: "sample_points", ParentFrom: "sample_points",
				Data: rawArr(pointsWithBlip(outDir))},
		},
		{
			ID: "M5_sampling_gap", Title: "采样缺口扩大",
			Scenario:       "只改采样点：新增更长缺口；区间断裂、不跨缺口插值，跨缺口处给出 unknown 状态",
			ChangesDirect:  "sample_points[removed rows]",
			ExpectedStatus: "committed", ExpectRule: "R4",
			Layer: layerPatch{Kind: "sample_points", ParentFrom: "sample_points",
				Data: rawArr(pointsWithWiderGap())},
		},
		{
			ID: "M6_initial_mass_revision", Title: "初重修订",
			Scenario:       "只改初重直接来源：m0 10.0 -> 9.98mg；所有百分比与残留率依赖它而重算，mg 量不变",
			ChangesDirect:  "initial_mass.mass_mg",
			ExpectedStatus: "committed", ExpectRule: "R6",
			Layer: layerPatch{Kind: "initial_mass", ParentFrom: "initial_mass",
				Data: rawMap(map[string]any{
					"mass_mg":          json.Number("9.98000"),
					"method":           "tare_baseline_average_recalibrated",
					"reference_time_s": json.Number("0"),
					"note":             "初重复核修订（新版本，父版本保留）",
				})},
		},
		{
			ID: "M7_import_half_failure", Title: "导入半失败",
			Scenario:       "一次提交中含结构非法层与合法层：合法片段(initial_mass)保留、非法层(gas_valve_events)拒绝并留诊断，整体 partial",
			ChangesDirect:  "gas_valve_events.data(structure)",
			ExpectedStatus: "partial", ExpectRule: "",
			Layer: layerPatch{Kind: "gas_valve_events", ParentFrom: "gas_valve_events", FatalStructure: true,
				Data: json.RawMessage(`{"not_an_array": true}`),
				ExtraLayer: &extraPatch{Kind: "initial_mass", ParentFrom: "initial_mass",
					Data: rawMap(map[string]any{
						"mass_mg":          json.Number("9.99500"),
						"method":           "tare_baseline_average_halfcheck",
						"reference_time_s": json.Number("0"),
						"note":             "与非法层同批提交的合法片段",
					})}},
		},
	}
	for _, m := range muts {
		writeFile(outDir+"/mutations/"+m.ID+".json", m)
	}
}

func regenerate(tweak func(p *pt, i int)) []map[string]any {
	pts := generatePoints()
	rows := make([]map[string]any, 0, len(pts))
	for i, p := range pts {
		tweak(&p, i)
		rows = append(rows, map[string]any{
			"time_s": json.Number(fnum(p.time, 0)), "temp_c": json.Number(fnum(p.temp, 2)),
			"mass_mg": json.Number(fnum(p.mass, 5)), "_row": i,
		})
	}
	return rows
}

func pointsWithBlip(outDir string) []map[string]any {
	// single-source change: copy the baseline sample rows verbatim and alter
	// exactly one point (t=306s); every other raw value is identical.
	raw, errR := osReadBaselineSamples(outDir)
	if errR != nil {
		panic(errR)
	}
	rows := make([]map[string]any, len(raw))
	for i, r := range raw {
		cp := map[string]any{}
		for k, v := range r {
			cp[k] = v
		}
		if asInt(r["time_s"]) == 306 {
			cp["mass_mg"] = json.Number(fnum(asFloat(r["mass_mg"])+0.0035, 5))
			cp["mutated"] = "M4 single-point non-monotonic injection"
		}
		rows[i] = cp
	}
	return rows
}

func pointsWithWiderGap() []map[string]any {
	pts := generatePoints()
	rows := make([]map[string]any, 0)
	for i, p := range pts {
		if p.time > 360 && p.time < 480 { // extend gap upward
			continue
		}
		rows = append(rows, map[string]any{
			"time_s": json.Number(fnum(p.time, 0)), "temp_c": json.Number(fnum(p.temp, 2)),
			"mass_mg": json.Number(fnum(p.mass, 5)), "_row": i,
		})
	}
	return rows
}
