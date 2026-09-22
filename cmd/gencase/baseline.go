package main

import (
	"encoding/json"
	"strconv"

	"tgareview/internal/bundle"
	"tgareview/internal/domain"
)

func fnum(x float64, nd int) string { return strconv.FormatFloat(x, 'f', nd, 64) }

func writeBaseline(outDir string, points []pt) {
	sp := make([]map[string]any, 0, len(points))
	for i, p := range points {
		sp = append(sp, map[string]any{
			"time_s":  json.Number(fnum(p.time, 0)),
			"temp_c":  json.Number(fnum(p.temp, 2)),
			"mass_mg": json.Number(fnum(p.mass, 5)),
			"_row":    i,
		})
	}
	b := bundle.Bundle{
		FormatVersion: bundle.FormatVersion,
		Name:          "TGA 失重谱系案例：阶段分解 / 气氛切换 / 残留物证据链",
		Description:   "离线领域案例基线：原始采样点、天平漂移校正、炉程序、气体阀事件、空白曲线与样品初重分层保存。",
		Layers: []bundle.LayerInput{
			{Kind: domain.SamplePoints, SourceFile: "samples/tga_run_01.csv", Data: rawMap(sp)},
			{Kind: domain.DriftLayer, SourceFile: "balance/drift.json", Data: rawMap(map[string]any{
				"basis":            domain.BasisBalanceMassMg,
				"rate_mg_per_min":  json.Number("0.0120"),
				"reference_time_s": json.Number("0"),
				"note":             "空载基线斜率（导出层校正，原始点不变）",
			})},
			{Kind: domain.FurnaceLayer, SourceFile: "furnace/program.json", Data: rawMap(map[string]any{
				"temp_unit": "C",
				"segments": []map[string]any{
					{"kind": "ramp", "start_s": json.Number("0"), "end_s": json.Number("300"), "start_c": json.Number("50"), "end_c": json.Number("300")},
					{"kind": "hold", "start_s": json.Number("300"), "end_s": json.Number("420"), "start_c": json.Number("300"), "end_c": json.Number("300")},
					{"kind": "ramp", "start_s": json.Number("420"), "end_s": json.Number("840"), "start_c": json.Number("300"), "end_c": json.Number("1000")},
					{"kind": "hold", "start_s": json.Number("840"), "end_s": json.Number("960"), "start_c": json.Number("1000"), "end_c": json.Number("1000")},
				},
			})},
			{Kind: domain.GasValveEvents, SourceFile: "gas/valves.csv", Data: rawArr([]map[string]any{
				{"time_s": json.Number("180"), "from_gas": "N2", "to_gas": "N2", "flow": json.Number("50"), "flow_unit": "mL/min"},
				{"time_s": json.Number("560"), "from_gas": "N2", "to_gas": "air", "flow": json.Number("50"), "flow_unit": "mL/min", "note": "切空气（存在气氛传输延迟）"},
			})},
			{Kind: domain.BlankCurve, SourceFile: "blank/blank.csv", Data: rawArr(blankRows())},
			{Kind: domain.InitialMass, SourceFile: "samples/m0.json", Data: rawMap(map[string]any{
				"mass_mg":          json.Number("10.00000"),
				"method":           "tare_baseline_average",
				"reference_time_s": json.Number("0"),
				"note":             "样品初重独立分层；后续修订以新版本保存",
			})},
		},
	}
	writeFile(outDir+"/tga_case_v1/baseline.json", b)
}

func blankRows() []map[string]any {
	var rows []map[string]any
	for t := 0.0; t <= 960.0; t += 60 {
		rows = append(rows, map[string]any{
			"time_s":   json.Number(fnum(t, 0)),
			"blank_mg": json.Number(fnum(blankBase*mathSin(t/300.0), 5)),
		})
	}
	return rows
}

func rawMap(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
func rawArr(v any) json.RawMessage { return rawMap(v) }
