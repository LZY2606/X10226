package casepack

func q(value float64, unit, label string) map[string]any {
	return map[string]any{"value": value, "unit": unit, "label": label, "raw_retained": true}
}

func initialImport() map[string]any {
	return map[string]any{
		"schema":   "tga-source-batch/v1",
		"batch_id": "baseline-batch-001",
		"reason":   "热分析团队分层导入：质量、温度、炉时间、气氛事件、空白曲线和样品初重",
		"sources": []map[string]any{
			{
				"id": "src-raw-samples-v1", "layer": "raw_samples", "content_type": "application/json",
				"content": map[string]any{
					"coordinate_basis": "furnace_program_time_seconds_from_run_start; balance_tare_mg; thermocouple_celsius",
					"rows": []map[string]any{
						{"time": q(0, "s", "炉时间"), "temperature": q(30, "°C", "样品温度"), "mass": q(10.5, "mg", "天平原始读数")},
						{"time": q(60, "s", "炉时间"), "temperature": q(70, "°C", "样品温度"), "mass": q(10.5, "mg", "天平原始读数")},
						{"time": q(120, "s", "炉时间"), "temperature": q(110, "°C", "样品温度"), "mass": q(10.4, "mg", "天平原始读数")},
						{"time": q(180, "s", "炉时间"), "temperature": q(150, "°C", "样品温度"), "mass": q(10.1, "mg", "天平原始读数")},
						{"time": q(240, "s", "炉时间"), "temperature": q(190, "°C", "样品温度"), "mass": q(9.6, "mg", "天平原始读数")},
						{"time": q(300, "s", "炉时间"), "temperature": q(230, "°C", "样品温度"), "mass": q(9.2, "mg", "天平原始读数")},
						{"time": q(360, "s", "炉时间"), "temperature": q(270, "°C", "样品温度"), "mass": q(8.9, "mg", "天平原始读数")},
						{"time": q(420, "s", "炉时间"), "temperature": q(310, "°C", "样品温度"), "mass": q(8.4, "mg", "天平原始读数")},
						{"time": q(480, "s", "炉时间"), "temperature": q(350, "°C", "样品温度"), "mass": q(8.2, "mg", "天平原始读数")},
						{"time": q(540, "s", "炉时间"), "temperature": q(390, "°C", "样品温度"), "mass": q(8.1, "mg", "天平原始读数")},
						{"time": q(660, "s", "炉时间"), "temperature": q(470, "°C", "样品温度"), "mass": q(4.7, "mg", "天平原始读数")},
						{"time": q(720, "s", "炉时间"), "temperature": q(510, "°C", "样品温度"), "mass": q(4.6, "mg", "天平原始读数")},
						{"time": q(780, "s", "炉时间"), "temperature": q(550, "°C", "样品温度"), "mass": q(4.62, "mg", "天平原始读数")},
					},
				},
			},
			{
				"id": "src-balance-drift-v1", "layer": "balance_drift_correction", "content_type": "application/json",
				"content": map[string]any{"model": "linear_baseline_drift", "slope": q(0.002, "mg/min", "天平基线斜率"), "intercept": q(0.001, "mg", "零点截距")},
			},
			{
				"id": "src-furnace-v1", "layer": "furnace_program", "content_type": "application/json",
				"content": map[string]any{"coordinate_basis": "furnace_program_time_seconds", "segments": []map[string]any{
					{"start": q(0, "s", "程序开始"), "end": q(780, "s", "升温结束"), "start_temp": q(30, "°C", "开始温度"), "end_temp": q(550, "°C", "结束温度"), "kind": "ramp"},
					{"start": q(780, "s", "保持开始"), "end": q(900, "s", "保持结束"), "start_temp": q(550, "°C", "恒温"), "end_temp": q(550, "°C", "恒温"), "kind": "hold"},
				}},
			},
			{
				"id": "src-gas-events-v1", "layer": "gas_valve_events", "content_type": "application/json",
				"content": map[string]any{"coordinate_basis": "furnace_program_time_seconds", "events": []map[string]any{
					{"id": "valve-air-on", "valve_at": q(480, "s", "切空气阀时间"), "from_atmosphere": "N2", "to_atmosphere": "air", "atmosphere_delay": q(60, "s", "传输延迟"), "temperature_lag": q(60, "s", "温度/反应滞后"), "window_end_valve_at": q(780, "s", "空气阀窗口结束")},
				}},
			},
			{
				"id": "src-blank-v1", "layer": "blank_curve", "content_type": "application/json",
				"content": map[string]any{"coordinate_basis": "furnace_program_time_seconds; empty_pan_mg", "rows": []map[string]any{
					{"time": q(0, "s", "炉时间"), "mass": q(0.010, "mg", "空白读数")},
					{"time": q(120, "s", "炉时间"), "mass": q(0.012, "mg", "空白读数")},
					{"time": q(240, "s", "炉时间"), "mass": q(0.014, "mg", "空白读数")},
					{"time": q(360, "s", "炉时间"), "mass": q(0.016, "mg", "空白读数")},
					{"time": q(540, "s", "炉时间"), "mass": q(0.018, "mg", "空白读数")},
					{"time": q(660, "s", "炉时间"), "mass": q(0.020, "mg", "空白读数")},
					{"time": q(780, "s", "炉时间"), "mass": q(0.022, "mg", "空白读数")},
				}},
			},
			{
				"id": "src-initial-mass-v1", "layer": "sample_initial_mass", "content_type": "application/json",
				"content": map[string]any{"method": "pre_run_balance_layer_reviewed_independently", "mass": q(10.5, "mg", "分层保存的样品初重")},
			},
			{
				"id": "src-parameters-v1", "layer": "analysis_parameters", "content_type": "application/json",
				"content": map[string]any{"coordinate_basis": "furnace_time_seconds", "sample_gap_threshold": q(90, "s", "采样缺口阈值"), "blank_gap_threshold": q(180, "s", "空白插值阈值"), "peak_threshold": q(0.38, "mg/min", "DTG 峰阈值"), "noise_threshold": q(0.005, "mg/min", "非单调噪声阈值"), "overlap_strategy": "retain_narrow_and_broad"},
			},
		},
	}
}
