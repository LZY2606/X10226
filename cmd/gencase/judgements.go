package main

// JudgementSpec is the machine-readable expectation for a rule statement.
type JudgementSpec struct {
	Code            string   `json:"code"`
	Statement       string   `json:"statement"`
	MustHold        bool     `json:"must_hold"`
	AssertPresent   bool     `json:"assert_present"`
	AssertStatusAny []string `json:"assert_status_any,omitempty"`
}

func writeJudgements(outDir string) {
	specs := []JudgementSpec{
		{Code: "R1", Statement: "气氛延迟和温度滞后使阀切换时间不等于样品反应时间", MustHold: true, AssertPresent: true, AssertStatusAny: []string{"applies"}},
		{Code: "R2", Statement: "炉温与样品温度采用不同坐标基准（一阶滞后）", MustHold: true, AssertPresent: true},
		{Code: "R3", Statement: "重叠阶段保留全部合法候选，且不强制阶段百分比总和为 100%", MustHold: true, AssertPresent: true},
		{Code: "R4", Statement: "数据缺口不跨越插值；平滑与阶段窗口在区间边界停止", MustHold: true, AssertPresent: true},
		{Code: "R5", Statement: "非单调噪声原位保留并标记，不为画图丢弃", MustHold: true, AssertPresent: true},
		{Code: "R6", Statement: "阶段可重叠，单独报告失重，不重归一化到 100%", MustHold: true, AssertPresent: true},
		{Code: "R7", Statement: "天平基线漂移仅作用于派生层，原始采样点不可变", MustHold: true, AssertPresent: true},
	}
	writeFile(outDir+"/judgements/rule_specs.json", map[string]any{
		"format_version": "tga.judgements/v1",
		"judgements":     specs,
	})
}
