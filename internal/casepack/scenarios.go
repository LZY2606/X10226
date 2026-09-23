package casepack

import _ "embed"

//go:embed files/manifest.json
var manifestJSON []byte

// Scenarios lists the committed counterexample scenarios plus the full
// narrative case. The seven acceptance scenarios are all present.
func Scenarios() []Manifest { return scenarioList }

var scenarioList = []Manifest{
	{
		ID: "narrative", Title: "失重谱系：TGA 阶段分解、气氛切换与残留物证据链",
		Narrative: true, Seed: 20260919,
		Description: "完整叙事：分层导入 -> 参数修订 -> 局部源数据改动 -> 互相冲突的人工决定 -> 时间旅行复核。",
		Params: defaultParams(), Files: fileSet("baseline"),
		Triggers: []Trigger{
			{Kind: "import", Files: fileSet("baseline")},
			{Kind: "judgement", Payload: map[string]any{"code": "baseline_stratified",
				"rule": "六个原始分层各自保存，原始表示不可覆盖", "adopted": true}},
			{Kind: "revise_params", Params: ptrParams(ParamsSpec{
				GasDelaySeconds: 40, TempLagSeconds: 60, PeakWindowSeconds: 120,
				OverlapTolSeconds: 60, GapMaxSeconds: 90}),
				Reason: "依据炉段实际传输时间修订气氛延迟与温度滞后"},
			{Kind: "import", Files: []string{"drift_shifted.csv"},
				Reason: "天平基线漂移修订（仅局部依赖）"},
			{Kind: "judgement", Payload: map[string]any{"code": "local_invalidation",
				"rule": "漂移改动只使 corrected/dtg/stages/residue 过期，气体窗口不变化",
				"affected_node_kinds": []string{"corrected.mass", "dtg", "stages", "residue"},
				"spared_node_kinds": []string{"reaction_windows"}, "adopted": true}},
		},
		Expect: Expectation{StagesSumFree: true, ReactionAfterValve: true},
	},
	{
		ID: "gas_delay", Title: "气氛延迟：阀切换时间 != 样品反应时间", Seed: 101,
		Params: defaultParams(), Files: fileSet("gas_delay"),
		Triggers: []Trigger{{Kind: "import", Files: fileSet("gas_delay")}, {
			Kind: "judgement", Payload: map[string]any{
				"code": "gas_delay_lag", "adopted": true,
				"rule": "反应窗口 = 阀时间 + 气氛延迟 + 温度滞后；阶段可重叠，质量损失不强制总和为100%"}}},
		Judgements: []Judgement{{Code: "gas_delay_lag", Rule: "valve != reaction",
			AffectedKinds: []string{"reaction_windows", "stages"}, Adopted: true}},
		Expect: Expectation{StagesSumFree: true, ReactionAfterValve: true},
	},
	{
		ID: "drift", Title: "天平基线漂移：影响仅限校正下游", Seed: 102,
		Params: defaultParams(), Files: fileSet("drift"),
		Triggers: []Trigger{
			{Kind: "import", Files: fileSet("drift")},
			{Kind: "import", Files: []string{"drift_shifted.csv"}},
			{Kind: "judgement", Payload: map[string]any{"code": "drift_local", "adopted": true,
				"affected_node_kinds": []string{"corrected.mass", "dtg", "stages", "residue"},
				"spared_node_kinds": []string{"reaction_windows"}}},
		},
		Judgements: []Judgement{{Code: "drift_local", Rule: "local invalidation", Adopted: true}},
	},
	{
		ID: "overlap", Title: "重叠峰：保留全部合法候选", Seed: 103,
		Params: defaultParams(), Files: fileSet("overlap"),
		Triggers: []Trigger{{Kind: "import", Files: fileSet("overlap")}, {
			Kind: "judgement", Payload: map[string]any{"code": "overlap_unknown", "adopted": true,
			"rule": "重叠峰分解不唯一时保留全部合法候选，阶段可重叠且不强制总和为100%"}}},
		Judgements: []Judgement{{Code: "overlap_unknown", Rule: "ambiguous candidates", Adopted: true}},
		Expect: Expectation{UnknownStages: 1, Overlapping: true, StagesSumFree: true},
	},
	{
		ID: "nonmonotone", Title: "非单调噪声：不作为质量增加", Seed: 104,
		Params: defaultParams(), Files: fileSet("nonmonotone"),
		Triggers: []Trigger{{Kind: "import", Files: fileSet("nonmonotone")}, {
			Kind: "judgement", Payload: map[string]any{"code": "non_monotone", "adopted": true,
			"rule": "质量上冲被标记为噪声并按单调包络处理，保留原始证据"}}},
		Judgements: []Judgement{{Code: "non_monotone", Rule: "noise evidence", Adopted: true}},
		Expect: Expectation{NonMonotone: true},
	},
	{
		ID: "gap", Title: "采样缺口：不跨越插值", Seed: 105,
		Params: defaultParams(), Files: fileSet("gap"),
		Triggers: []Trigger{{Kind: "import", Files: fileSet("gap")}, {
			Kind: "judgement", Payload: map[string]any{"code": "gap_no_interp", "adopted": true,
			"rule": "缺口两侧不插值；跨缺口阶段保持 unknown 与全部合法边界候选"}}},
		Judgements: []Judgement{{Code: "gap_no_interp", Rule: "no interpolation across gaps", Adopted: true}},
		Expect: Expectation{CrossesGap: true, UnknownStages: 1},
	},
	{
		ID: "initrev", Title: "初重修订：新版本与父版本", Seed: 106,
		Params: defaultParams(), Files: fileSet("initrev"),
		Triggers: []Trigger{
			{Kind: "import", Files: fileSet("initrev_base")},
			{Kind: "import", Files: []string{"init_mass.csv"},
				Reason: "样品初重分层修订"},
			{Kind: "judgement", Payload: map[string]any{"code": "init_revision", "adopted": true,
				"rule": "初重以新版本+父版本表达，残留率随之重算，原始初重仍可审计"}}},
		Judgements: []Judgement{{Code: "init_revision", Rule: "versioned revision", Adopted: true}},
		Expect: Expectation{InitRevised: true},
	},
	{
		ID: "halffail", Title: "导入半失败：拒绝分层被保留", Seed: 107,
		Params: defaultParams(), Files: fileSet("halffail"),
		Triggers: []Trigger{{Kind: "import", Files: fileSet("halffail")}, {
			Kind: "judgement", Payload: map[string]any{"code": "half_failure", "adopted": true,
				"rule": "格式错误的分层被拒绝并保留，其他分层照常派生，错误值留在原位置附诊断"}}},
		Judgements: []Judgement{{Code: "half_failure", Rule: "partial batch preserved", Adopted: true}},
		Expect: Expectation{HalfFailure: true},
	},
}

func defaultParams() ParamsSpec {
	return ParamsSpec{GasDelaySeconds: 30, TempLagSeconds: 45,
		PeakWindowSeconds: 120, OverlapTolSeconds: 60, GapMaxSeconds: 90}
}

func ptrParams(p ParamsSpec) *ParamsSpec { return &p }

func fileSet(scenario string) []string {
	return []string{
		scenario + "/samples.csv", scenario + "/drift.csv", scenario + "/program.csv",
		scenario + "/gas.csv", scenario + "/blank.csv", scenario + "/init_mass.csv",
	}
}
