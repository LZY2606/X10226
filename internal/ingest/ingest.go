// Package ingest parses the stratified TGA source files. Original text is
// always preserved; normalisation happens in the derived layer. Malformed
// rows produce diagnostics rather than aborting (partial import), except
// structurally broken files which fail outright.
package ingest

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"tga-review/internal/domain"
)

// File is one uploaded/imported source file.
type File struct {
	Name    string
	Content []byte
}

// LayerForName maps file names to source strata.
func LayerForName(name string) (domain.Layer, bool) {
	switch strings.TrimSpace(name) {
	case "raw_points.csv":
		return domain.LayerRawPoints, true
	case "balance_drift.csv":
		return domain.LayerDrift, true
	case "furnace_program.csv":
		return domain.LayerFurnace, true
	case "gas_valve.csv":
		return domain.LayerGasValve, true
	case "blank_curve.csv":
		return domain.LayerBlankCurve, true
	case "initial_mass.csv":
		return domain.LayerInitialMass, true
	case "params.json":
		return domain.LayerParams, true
	}
	return "", false
}

// ParseResult is the parsed stratum plus diagnostics.
type ParseResult struct {
	Layer domain.Layer
	Sub   domain.Submission
	Fatal error // structurally unusable file
}

// Parse parses one file into a populated submission (IDs assigned by store).
func Parse(f File) ParseResult {
	layer, ok := LayerForName(f.Name)
	res := ParseResult{Layer: layer}
	if !ok {
		res.Fatal = fmt.Errorf("未知来源文件 %q（期望七个分层文件名之一）", f.Name)
		return res
	}
	sub := domain.Submission{Layer: layer, SourceName: f.Name, Kind: "import"}
	sub.PayloadHash = domain.Fingerprint(string(f.Content))
	if layer == domain.LayerParams {
		parseParams(f.Content, &sub, &res)
		res.Sub = sub
		return res
	}
	r := csv.NewReader(strings.NewReader(string(f.Content)))
	r.FieldsPerRecord = -1
	header, err := r.Read()
	if err != nil {
		res.Fatal = fmt.Errorf("%s: 无法读取表头: %w", f.Name, err)
		return res
	}
	col := mapHeader(header)
	line := 1
	for {
		rec, err := r.Read()
		line++
		if err == io.EOF {
			break
		}
		if err != nil {
			res.Fatal = fmt.Errorf("%s 第 %d 行 CSV 结构损坏: %w", f.Name, line, err)
			return res
		}
		raw := strings.Join(rec, ",")
		get := func(name string) string {
			if i, ok := col[name]; ok && i < len(rec) {
				return strings.TrimSpace(rec[i])
			}
			return ""
		}
		switch layer {
		case domain.LayerRawPoints:
			rp := domain.RawPoint{Line: line, Raw: raw,
				Time: domain.ParseValue(get("time"), "time"),
				Temp: domain.ParseValue(get("temp"), "temp"),
				Mass: domain.ParseValue(get("mass"), "mass")}
			if !rp.Valid() {
				sub.Partial = true
				sub.Diags = append(sub.Diags, domain.Diag{
					Level: "error", Code: "bad_row", Layer: string(layer), Line: line,
					Message: "该行无法完整转换，已保留原始文本且不参与计算", Raw: raw})
			}
			sub.Points = append(sub.Points, rp)
		case domain.LayerBlankCurve:
			bp := domain.BlankPoint{Line: line,
				Time: domain.ParseValue(get("time"), "time"),
				Mass: domain.ParseValue(get("mass"), "mass")}
			if !bp.Time.OK || !bp.Mass.OK {
				sub.Partial = true
				sub.Diags = append(sub.Diags, domain.Diag{
					Level: "error", Code: "bad_row", Layer: string(layer), Line: line,
					Message: "空白点无法完整转换", Raw: raw})
			}
			sub.Blank = append(sub.Blank, bp)
		case domain.LayerDrift:
			ds := &domain.DriftSpec{
				Line: line, Model: get("model"), Note: get("note"),
				SlopeMgPerS: domain.ParseValue(get("slope_mg_per_s"), "mass"),
				InterceptMg: domain.ParseValue(get("intercept_mg"), "mass"),
			}
			if ds.Model != "blank_linear" {
				sub.Diags = append(sub.Diags, domain.Diag{
					Level: "error", Code: "unknown_drift_model", Layer: string(layer), Line: line,
					Message: fmt.Sprintf("未知漂移模型 %q", ds.Model), Raw: raw})
			}
			sub.Drift = ds
		case domain.LayerFurnace:
			fs := domain.FurnaceSegment{
				Line: line, Kind: get("kind"),
				StartS:     domain.ParseValue(get("start_s"), "time"),
				EndS:       domain.ParseValue(get("end_s"), "time"),
				StartTempC: domain.ParseValue(get("start_temp_c"), "temp"),
				EndTempC:   domain.ParseValue(get("end_temp_c"), "temp"),
			}
			if !fs.StartS.OK || !fs.EndS.OK || !fs.StartTempC.OK || !fs.EndTempC.OK {
				sub.Partial = true
				sub.Diags = append(sub.Diags, domain.Diag{
					Level: "error", Code: "bad_row", Layer: string(layer), Line: line,
					Message: "炉程序段无法完整转换", Raw: raw})
			}
			sub.Furnace = append(sub.Furnace, fs)
		case domain.LayerGasValve:
			ve := domain.ValveEvent{
				Line: line, FromGas: get("from_gas"), ToGas: get("to_gas"),
				AtS:      domain.ParseValue(get("at_s"), "time"),
				FlowSCCM: domain.ParseValue(get("flow_sccm"), "flow"),
			}
			if !ve.AtS.OK {
				sub.Partial = true
				sub.Diags = append(sub.Diags, domain.Diag{
					Level: "error", Code: "bad_row", Layer: string(layer), Line: line,
					Message: "阀事件时间无法转换", Raw: raw})
			}
			sub.Valves = append(sub.Valves, ve)
		case domain.LayerInitialMass:
			im := &domain.InitialMass{Line: line, Basis: get("basis"),
				Mass: domain.ParseValue(get("mass"), "mass")}
			if !im.Mass.OK {
				sub.Partial = true
				sub.Diags = append(sub.Diags, domain.Diag{
					Level: "error", Code: "bad_row", Layer: string(layer), Line: line,
					Message: "样品初重无法转换", Raw: raw})
			}
			sub.InitialMass = im
		}
	}
	if sub.Partial {
		sub.Kind = "import_partial"
	}
	res.Sub = sub
	return res
}

func parseParams(content []byte, sub *domain.Submission, res *ParseResult) {
	var p domain.Params
	if err := json.Unmarshal(content, &p); err != nil {
		res.Fatal = fmt.Errorf("params.json 不是合法 JSON: %w", err)
		return
	}
	if p.Algorithm == "" {
		p.Algorithm = "tga-v1"
	}
	if p.Algorithm != "tga-v1" {
		res.Fatal = fmt.Errorf("未知算法版本 %q（仅支持 tga-v1）", p.Algorithm)
		return
	}
	if p.BlankDriftModel == "" {
		p.BlankDriftModel = "blank_linear"
	}
	sub.Params = &p
}

func mapHeader(h []string) map[string]int {
	m := map[string]int{}
	for i, hh := range h {
		m[strings.TrimSpace(hh)] = i
	}
	return m
}
