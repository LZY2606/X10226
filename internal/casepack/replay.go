package casepack

func initialMassRevision() map[string]any {
	return map[string]any{
		"schema": "tga-source-batch/v1", "batch_id": "revision-initial-mass-002",
		"parent_batch": "baseline-batch-001", "reason": "样品初重复核后修订；只影响残留率和阶段百分比",
		"scope":   "sample_initial_mass_only",
		"sources": []map[string]any{{"id": "src-initial-mass-v2", "layer": "sample_initial_mass", "parent_id": "src-initial-mass-v1", "content_type": "application/json", "content": map[string]any{"method": "post_import_balance_audit", "mass": q(10.48, "mg", "修订后的样品初重")}}},
	}
}

func driftRevision() map[string]any {
	return map[string]any{
		"schema": "tga-source-batch/v1", "batch_id": "revision-baseline-drift-003",
		"parent_batch": "revision-initial-mass-002", "reason": "天平基线漂移重估；参数规则版本产生新派生指纹",
		"scope":   "balance_drift_only",
		"sources": []map[string]any{{"id": "src-balance-drift-v2", "layer": "balance_drift_correction", "parent_id": "src-balance-drift-v1", "content_type": "application/json", "content": map[string]any{"model": "linear_baseline_drift_reviewed", "slope": q(0.0016, "mg/min", "修订天平基线斜率"), "intercept": q(0.0008, "mg", "修订零点截距")}}},
	}
}

func localRawRevision() map[string]any {
	return map[string]any{
		"schema": "tga-source-batch/v1", "batch_id": "revision-local-raw-004",
		"parent_batch": "revision-baseline-drift-003", "reason": "只修订 420 s 原始采样点；用于检验局部依赖过期",
		"scope":   "raw_samples.rows[7].mass_only",
		"sources": []map[string]any{{"id": "src-raw-samples-v2", "layer": "raw_samples", "parent_id": "src-raw-samples-v1", "content_type": "application/json", "content": copiedRawWith420Mass(8.43)}},
	}
}

func halfFailedImport() map[string]any {
	return map[string]any{
		"schema": "tga-source-batch/v1", "batch_id": "half-failure-format-check-005",
		"parent_batch": "revision-local-raw-004", "reason": "故意提交未知层和不可覆盖源；整批不得落库",
		"sources": []map[string]any{
			{"id": "src-gas-events-v1", "layer": "gas_valve_events", "content_type": "application/json", "content": map[string]any{"not_allowed": "immutable overwrite attempt"}},
			{"id": "src-generic-spreadsheet", "layer": "generic_table", "content_type": "text/csv", "content": map[string]any{"note": "not a TGA source layer"}},
		},
	}
}

func parameterAdjustment() map[string]any {
	return map[string]any{"reason": "审议重叠峰后只保留窄窗口解释", "actor": "lead-reviewer", "parameters": map[string]any{"overlap_strategy": "narrow_only", "peak_threshold": q(0.42, "mg/min", "调整后的 DTG 峰阈值")}}
}
