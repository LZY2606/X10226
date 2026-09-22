package main

// ReplayStep is one deterministic action in the counter-example replay.
type ReplayStep struct {
	Seq      int    `json:"seq"`
	Kind     string `json:"kind"` // import_baseline | params_revision | source_revision | decision | undo | import_mutation
	Mutation string `json:"mutation,omitempty"`
	Decision string `json:"decision,omitempty"` // accept|reject
	Target   string `json:"target,omitempty"`   // candidate option label, resolved at runtime
	Note     string `json:"note,omitempty"`
}

func writeReplay(outDir string) {
	steps := []ReplayStep{
		{Seq: 1, Kind: "import_baseline", Note: "六分层基线导入，生成当前 artifact 与阶段候选"},
		{Seq: 2, Kind: "params_revision", Mutation: "M1_atmosphere_delay", Note: "一次参数修订：气氛延迟 12->30s"},
		{Seq: 3, Kind: "source_revision", Mutation: "M2_baseline_drift", Note: "只影响局部依赖：漂移率修订，仅依赖它的派生结果过期"},
		{Seq: 4, Kind: "decision", Mutation: "M3_overlapping_peaks", Target: "split_a", Decision: "accept", Note: "采纳重叠峰的一种分解"},
		{Seq: 5, Kind: "decision", Target: "merged", Decision: "accept", Note: "互相冲突的人工决定：同时采纳 merged（互斥），形成冲突记录"},
		{Seq: 6, Kind: "undo", Target: "merged", Note: "撤销冲突决定，撤销写入历史"},
		{Seq: 7, Kind: "import_mutation", Mutation: "M7_import_half_failure", Note: "导入半失败：合法片段保留、非法层拒绝并诊断"},
	}
	writeFile(outDir+"/tga_case_v1/replay_plan.json", map[string]any{
		"format_version": "tga.replay/v1",
		"seed":           20260922,
		"steps":          steps,
	})
}
