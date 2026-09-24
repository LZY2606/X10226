package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// 单位换算表：派生层统一规范化为 s / mg / °C。
var timeToS = map[string]float64{"s": 1, "sec": 1, "min": 60, "h": 3600}
var massToMg = map[string]float64{"mg": 1, "g": 1000, "ug": 0.001, "µg": 0.001}

func tempToC(v float64, unit string) (float64, bool) {
	switch strings.ToLower(unit) {
	case "c", "°c", "degc":
		return v, true
	case "k":
		return v - 273.15, true
	}
	return 0, false
}

func parseNum(raw string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(raw), 64)
}

// Normalize 把已接受的原始层解析为规范化负载。
// 无法转换的值留在原位置（Valid=false）并附诊断，绝不为了画图丢掉。
func Normalize(layer *Layer) (any, []Diagnostic, error) {
	var diags []Diagnostic
	diag := func(idx int, field, raw, msg string) {
		diags = append(diags, Diagnostic{LayerID: layer.ID, Index: idx, Field: field, Raw: raw, Message: msg})
	}
	switch layer.Kind {
	case LayerRawSamples:
		var in struct {
			UnitTime string     `json:"unit_time"`
			UnitMass string     `json:"unit_mass"`
			UnitTemp string     `json:"unit_temp"`
			Points   [][]string `json:"points"`
		}
		if err := json.Unmarshal([]byte(layer.Raw), &in); err != nil {
			return nil, nil, fmt.Errorf("raw_samples 格式错误: %w", err)
		}
		tf, ok := timeToS[strings.ToLower(in.UnitTime)]
		if !ok {
			return nil, nil, fmt.Errorf("未知时间单位 %q", in.UnitTime)
		}
		mf, ok := massToMg[strings.ToLower(in.UnitMass)]
		if !ok {
			return nil, nil, fmt.Errorf("未知质量单位 %q", in.UnitMass)
		}
		out := SampleSeries{TimeUnit: "s", MassUnit: "mg", TempUnit: "C"}
		for i, row := range in.Points {
			if len(row) != 3 {
				diag(i, "row", strings.Join(row, ","), "采样点需要 3 列 (t, mass, temp)")
				out.Points = append(out.Points, SamplePoint{})
				continue
			}
			p := SamplePoint{RawT: row[0], RawM: row[1], RawTmp: row[2]}
			if v, err := parseNum(row[0]); err != nil {
				diag(i, "t", row[0], "时间无法转换")
			} else {
				p.T, p.ValidT = v*tf, true
			}
			if v, err := parseNum(row[1]); err != nil {
				diag(i, "mass", row[1], "质量无法转换")
			} else {
				p.Mass, p.ValidM = v*mf, true
			}
			if v, err := parseNum(row[2]); err != nil {
				diag(i, "temp", row[2], "温度无法转换")
			} else if c, ok := tempToC(v, in.UnitTemp); !ok {
				diag(i, "temp", row[2], "未知温度单位 "+in.UnitTemp)
			} else {
				p.Temp, p.ValidTmp = c, true
			}
			out.Points = append(out.Points, p)
		}
		return &out, diags, nil

	case LayerDrift, LayerBlankCurve:
		var in struct {
			Unit   string      `json:"unit"`
			Points [][]float64 `json:"points"`
		}
		if err := json.Unmarshal([]byte(layer.Raw), &in); err != nil {
			return nil, nil, fmt.Errorf("%s 格式错误: %w", layer.Kind, err)
		}
		mf, ok := massToMg[strings.ToLower(in.Unit)]
		if !ok {
			return nil, nil, fmt.Errorf("未知质量单位 %q", in.Unit)
		}
		pts := make([]CurvePoint, 0, len(in.Points))
		for i, row := range in.Points {
			if len(row) != 2 {
				diag(i, "row", fmt.Sprint(row), "曲线点需要 2 列 (t_s, mg)")
				continue
			}
			pts = append(pts, CurvePoint{T: row[0], V: row[1] * mf})
		}
		if layer.Kind == LayerDrift {
			return &DriftCurve{Points: pts, Unit: "mg"}, diags, nil
		}
		return &BlankCurve{Points: pts, Unit: "mg"}, diags, nil

	case LayerFurnaceProgram:
		var in FurnaceProgram
		if err := json.Unmarshal([]byte(layer.Raw), &in); err != nil {
			return nil, nil, fmt.Errorf("furnace_program 格式错误: %w", err)
		}
		if len(in.Segments) == 0 {
			return nil, nil, fmt.Errorf("furnace_program 至少需要一个程序段")
		}
		in.Basis = "furnace_setpoint"
		for i := range in.Segments {
			s := &in.Segments[i]
			if s.EndS > s.StartS {
				s.RateCPerMin = (s.EndC - s.StartC) / (s.EndS - s.StartS) * 60
			}
		}
		return &in, diags, nil

	case LayerGasEvents:
		var in GasEventList
		if err := json.Unmarshal([]byte(layer.Raw), &in); err != nil {
			return nil, nil, fmt.Errorf("gas_events 格式错误: %w", err)
		}
		in.Basis = "valve"
		for i := range in.Events {
			if in.Events[i].ID == "" {
				in.Events[i].ID = fmt.Sprintf("gas_%d", i+1)
			}
		}
		return &in, diags, nil

	case LayerInitialMass:
		var in struct {
			Mass  string `json:"mass"`
			Unit  string `json:"unit"`
			Basis string `json:"basis"`
			Note  string `json:"note"`
		}
		if err := json.Unmarshal([]byte(layer.Raw), &in); err != nil {
			return nil, nil, fmt.Errorf("initial_mass 格式错误: %w", err)
		}
		mf, ok := massToMg[strings.ToLower(in.Unit)]
		if !ok {
			return nil, nil, fmt.Errorf("未知质量单位 %q", in.Unit)
		}
		v, err := parseNum(in.Mass)
		if err != nil {
			return nil, nil, fmt.Errorf("初重无法转换: %q", in.Mass)
		}
		if in.Basis == "" {
			in.Basis = "as_weighed"
		}
		return &InitialMass{MassMg: v * mf, Basis: in.Basis, Note: in.Note}, diags, nil
	}
	return nil, nil, fmt.Errorf("未知层类型 %q", layer.Kind)
}
