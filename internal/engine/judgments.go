package engine

import (
	"fmt"
	"sort"

	"tga/internal/model"
)

// buildJudgments emits the machine-readable domain findings for this state.
// These are the enforceable judgments reviewers rely on:
//
//	VALVE_TIME_NEQ_REACTION_TIME, REACTION_WINDOW_UNCERTAIN,
//	OVERLAPPING_STAGES_POSSIBLE, STAGES_NEED_NOT_SUM_100,
//	GAP_DO_NOT_BRIDGE, NONMONOTONE_PRESENT, PARTIAL_IMPORT,
//	M0_REVISED, DRIFT_MODEL_ACTIVE, MULTIPLE_CANDIDATES_KEPT.
func (e *Engine) buildJudgments(st *State) {
	var js []model.Judgment
	params := st.ActiveParams()
	add := func(code, sev, msg string, refs []string, data map[string]any) {
		js = append(js, model.Judgment{Code: code, Severity: sev, Message: msg, AtSeq: st.AtSeq, Refs: refs, Data: data})
	}

	idsByKind := map[string][]*model.Artifact{}
	for _, id := range st.ArtifactIDs {
		a := st.Artifacts[id]
		if a.Status == "current" {
			idsByKind[a.Kind] = append(idsByKind[a.Kind], a)
		}
	}

	if len(idsByKind["atmosphere_window"]) > 0 {
		delay := params.Values["gas_delay_s"]
		add("VALVE_TIME_NEQ_REACTION_TIME", model.SevInfo,
			fmt.Sprintf("气氛延迟 %s s + 温度滞后：阀切换时间不等于样品反应时间，关联以时间窗表达", delay),
			refIDs(idsByKind["atmosphere_window"]), map[string]any{"delay_s": num(delay, 60)})
		add("REACTION_WINDOW_UNCERTAIN", model.SevWarn,
			"反应时间为区间而非点值；无法唯一归属时保留全部合法候选",
			refIDs(idsByKind["atmosphere_window"]), nil)
	}

	if len(idsByKind["data_gap"]) > 0 {
		add("GAP_DO_NOT_BRIDGE", model.SevWarn,
			"采样缺口不得用插值跨越；DTG 在缺口两侧断开，跨越缺口的失重状态为 unknown",
			refIDs(idsByKind["data_gap"]), map[string]any{"count": len(idsByKind["data_gap"])})
	}
	if len(idsByKind["nonmonotone_segment"]) > 0 {
		add("NONMONOTONE_PRESENT", model.SevWarn,
			"检测到非单调质量段（浮力/噪声），仅标注不做强制单调化",
			refIDs(idsByKind["nonmonotone_segment"]), nil)
	}

	overlapCount := 0
	candidates := map[string]int{}
	for _, a := range idsByKind["stage_candidate"] {
		if a.Data != nil {
			if n, ok := a.Data["combo_count"].(int); ok && n > 1 {
				overlapCount++
				candidates[a.Title[0:0]] = n
			}
		}
	}
	if overlapCount > 0 {
		maxN := 0
		for _, n := range candidates {
			if n > maxN {
				maxN = n
			}
		}
		add("OVERLAPPING_STAGES_POSSIBLE", model.SevInfo,
			fmt.Sprintf("重叠 DTG 峰导致阶段边界不唯一，已保留 %d 套合法候选供裁定", maxN),
			refIDs(idsByKind["stage_candidate"]), map[string]any{"candidate_combos": maxN})
		add("STAGES_NEED_NOT_SUM_100", model.SevInfo,
			"阶段允许重叠，阶段失重之和不强制等于 100%；残留率由终点质量/初重独立计算",
			refIDs(idsByKind["stage_candidate"]), nil)
	}

	if cs := idsByKind["corrected_series"]; len(cs) > 0 && cs[0].Status == "partial" {
		add("PARTIAL_IMPORT", model.SevWarn,
			"导入半失败：部分行不可转换，保留在原位置并附诊断；仅有效行参与派生计算",
			[]string{cs[0].ID}, map[string]any{"artifact": cs[0].ID})
	}

	if len(st.Submissions[model.LayerM0]) > 1 {
		add("M0_REVISED", model.SevInfo,
			"初重存在修订版本：新版本与父版本并存，旧版本不被覆盖，依赖旧版本的结果标记过期",
			subIDs(st.Submissions[model.LayerM0]), nil)
	}
	if len(st.Params) > 0 {
		add("DRIFT_MODEL_ACTIVE", model.SevInfo,
			fmt.Sprintf("当前算法 %s 参数指纹 %s（漂移模型=%s，延迟=%s s）",
				AlgorithmVersion, params.Fingerprint, params.Values["drift_model"], params.Values["gas_delay_s"]),
			nil, map[string]any{"param_fp": params.Fingerprint})
	}

	// conflicting decisions
	active := st.ActiveDecisions()
	rejected := map[string]bool{}
	for _, d := range active {
		if d.Action == "reject" {
			rejected[d.Target] = true
		}
	}
	if len(active) > 1 {
		add("DECISION_HISTORY_PRESENT", model.SevInfo,
			fmt.Sprintf("当前有 %d 条生效人工决定；冲突解决与撤销均已写入历史", len(active)),
			nil, nil)
	}
	for t := range rejected {
		if a := st.Artifacts[t]; a != nil && a.Kind == "stage_candidate" {
			add("CANDIDATE_REJECTED", model.SevInfo,
				fmt.Sprintf("阶段候选 %s 已被人工否定（仍保留在历史中，不参与当前结论）", t),
				[]string{t}, nil)
		}
	}

	sort.SliceStable(js, func(i, j int) bool { return js[i].Code < js[j].Code })
	st.Judgments = js
}

func refIDs(as []*model.Artifact) []string {
	out := make([]string, 0, len(as))
	for _, a := range as {
		out = append(out, a.ID)
	}
	return out
}

func subIDs(ss []*model.Submission) []string {
	out := make([]string, 0, len(ss))
	for _, s := range ss {
		out = append(out, s.ID)
	}
	return out
}
