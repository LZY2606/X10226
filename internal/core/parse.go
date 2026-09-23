package core

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Point is a normalized series point: T is seconds, V is normalized units
// (mg for mass, degC for temperature). Raw representation stays in the object.
type Point struct {
	T float64 `json:"t_s"`
	V float64 `json:"v"`
}

type ValveEvent struct {
	T   float64 `json:"t_s"`
	Gas string  `json:"gas"`
}

// Diagnostic records a value that could not be converted. The raw text is
// retained verbatim; the row is excluded from normalized series, never
// silently dropped or invented (no interpolation) for charting.
type Diagnostic struct {
	Line   int    `json:"line"`
	Column string `json:"column,omitempty"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

func toSeconds(num float64, unit string) (float64, bool) {
	switch unit {
	case "s", "sec", "secs", "second", "seconds":
		return num, true
	case "min", "mins", "minute", "minutes":
		return num * 60, true
	}
	return 0, false
}

func toMg(num float64, unit string) (float64, bool) {
	switch unit {
	case "mg", "milligram", "milligrams":
		return num, true
	case "g", "gram", "grams":
		return num * 1000, true
	case "ug", "µg", "microgram", "micrograms":
		return num / 1000, true
	}
	return 0, false
}

func toCelsius(num float64, unit string) (float64, bool) {
	switch unit {
	case "c", "°c", "degc", "celsius":
		return num, true
	case "k", "kelvin":
		return num - 273.15, true
	}
	return 0, false
}

// splitHeader splits "time_min" into ("time","min"), "mass_mg" etc.
func splitHeader(h string) (name, unit string) {
	h = strings.ToLower(strings.TrimSpace(h))
	if i := strings.LastIndex(h, "_"); i > 0 {
		return h[:i], h[i+1:]
	}
	return h, ""
}

func conv(num float64, unit, want string) (float64, bool) {
	switch want {
	case "time":
		return toSeconds(num, unit)
	case "mass":
		return toMg(num, unit)
	case "temp":
		return toCelsius(num, unit)
	}
	return 0, false
}

// ParseSeries parses a two-column CSV with unit-bearing headers.
// want selects the physical type of the value column: "mass" or "temp".
func ParseSeries(content string, want string) ([]Point, []Diagnostic, error) {
	r := csv.NewReader(strings.NewReader(content))
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("CSV 无法解析: %v", err)
	}
	if len(recs) < 2 {
		return nil, nil, fmt.Errorf("至少需要表头和一行数据")
	}
	tIdx, vIdx := -1, -1
	var tUnit, vUnit string
	for i, h := range recs[0] {
		name, unit := splitHeader(h)
		switch name {
		case "t", "time", "furnace_time", "炉时间":
			tIdx = i
			tUnit = unit
		case "mass", "weight", "m", "温度", "temp", "temperature":
			if (want == "mass") == (name == "mass" || name == "weight" || name == "m") {
				vIdx = i
				vUnit = unit
			}
		}
	}
	// Simpler deterministic fallback: header prefix matching.
	if tIdx < 0 || vIdx < 0 {
		for i, h := range recs[0] {
			name, unit := splitHeader(strings.ToLower(strings.TrimSpace(h)))
			if tIdx < 0 && (strings.HasPrefix(name, "time") || name == "t") {
				tIdx, tUnit = i, unit
			}
			if vIdx < 0 {
				switch want {
				case "mass":
					if strings.Contains(name, "mass") || strings.Contains(name, "weight") {
						vIdx, vUnit = i, unit
					}
				case "temp":
					if strings.Contains(name, "temp") {
						vIdx, vUnit = i, unit
					}
				}
			}
		}
	}
	if tIdx < 0 {
		return nil, nil, fmt.Errorf("缺少时间列（期望表头形如 time_s）")
	}
	if vIdx < 0 {
		return nil, nil, fmt.Errorf("缺少%s列（期望表头携带单位）", want)
	}
	if _, ok := conv(0, tUnit, "time"); tUnit != "" && !ok {
		return nil, nil, fmt.Errorf("无法识别的时间单位 %q，原始表示已保留", tUnit)
	}
	if _, ok := conv(0, vUnit, want); vUnit != "" && !ok {
		return nil, nil, fmt.Errorf("无法识别的%s单位 %q，原始表示已保留", want, vUnit)
	}
	var pts []Point
	var diags []Diagnostic
	for ri := 1; ri < len(recs); ri++ {
		rec := recs[ri]
		if len(recs) <= max2(tIdx, vIdx) || strings.TrimSpace(strings.Join(rec, "")) == "" {
			continue
		}
		lineNo := ri + 1
		tn, et := strconv.ParseFloat(strings.TrimSpace(rec[tIdx]), 64)
		vn, ev := strconv.ParseFloat(strings.TrimSpace(rec[vIdx]), 64)
		if et != nil || ev != nil {
			diags = append(diags, Diagnostic{Line: lineNo, Raw: strings.Join(rec, ","),
				Reason: "数值无法解析，保留原始行，不参与规范化"})
			continue
		}
		if tUnit != "" {
			if c, ok := conv(tn, tUnit, "time"); ok {
				tn = c
			}
		}
		if vUnit != "" {
			if c, ok := conv(vn, vUnit, want); ok {
				vn = c
			}
		}
		pts = append(pts, Point{T: tn, V: vn})
	}
	if len(pts) == 0 {
		return nil, diags, fmt.Errorf("没有可规范化的数据行")
	}
	return pts, diags, nil
}

func max2(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// ParseValves parses "time_s,gas" rows.
func ParseValves(content string) ([]ValveEvent, []Diagnostic, error) {
	r := csv.NewReader(strings.NewReader(content))
	r.FieldsPerRecord = -1
	recs, err := r.ReadAll()
	if err != nil {
		return nil, nil, fmt.Errorf("CSV 无法解析: %v", err)
	}
	if len(recs) < 2 {
		return nil, nil, fmt.Errorf("至少需要表头和一行数据")
	}
	tIdx, gIdx, tUnit := -1, -1, ""
	for i, h := range recs[0] {
		name, unit := splitHeader(h)
		if strings.Contains(name, "time") || name == "t" {
			tIdx, tUnit = i, unit
		}
		if name == "gas" || strings.Contains(name, "气体") {
			gIdx = i
		}
	}
	if tIdx < 0 || gIdx < 0 {
		return nil, nil, fmt.Errorf("阀事件需要 time_s,gas 两列")
	}
	var out []ValveEvent
	var diags []Diagnostic
	for ri := 1; ri < len(recs); ri++ {
		rec := recs[ri]
		if len(recs) <= max2(tIdx, gIdx) || strings.TrimSpace(strings.Join(rec, "")) == "" {
			continue
		}
		tn, et := strconv.ParseFloat(strings.TrimSpace(rec[tIdx]), 64)
		if et != nil {
			diags = append(diags, Diagnostic{Line: ri + 1, Raw: strings.Join(rec, ","),
				Reason: "时间无法解析，保留原始行"})
			continue
		}
		gas := strings.TrimSpace(rec[gIdx])
		if gas == "" {
			diags = append(diags, Diagnostic{Line: ri + 1, Raw: strings.Join(rec, ","),
				Reason: "气体名称为空，保留原始行"})
			continue
		}
		if tUnit != "" {
			if c, ok := toSeconds(tn, tUnit); ok {
				tn = c
			}
		}
		out = append(out, ValveEvent{T: tn, Gas: gas})
	}
	if len(out) == 0 {
		return nil, diags, fmt.Errorf("没有可规范化的阀事件行")
	}
	return out, diags, nil
}

type DriftSpec struct {
	RateUgPerMin float64 `json:"rate_ug_per_min"`
	Note         string  `json:"note,omitempty"`
}

func ParseDrift(content string) (DriftSpec, []Diagnostic, error) {
	var spec DriftSpec
	if err := json.Unmarshal([]byte(strings.TrimSpace(content)), &spec); err != nil {
		return spec, nil, fmt.Errorf("漂移校正需要 JSON（如 {\"rate_ug_per_min\":6.0}）: %v", err)
	}
	return spec, nil, nil
}

// ParseInitialMass accepts "5.000 mg" with an explicit unit.
func ParseInitialMass(content string) (float64, string, error) {
	fields := strings.Fields(strings.TrimSpace(content))
	if len(fields) < 2 {
		return 0, content, fmt.Errorf("初重需要数值与单位（如 5.000 mg），原始表示已保留")
	}
	n, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, content, fmt.Errorf("初重数值无法解析: %v", err)
	}
	unit := strings.ToLower(fields[1])
	mg, ok := toMg(n, unit)
	if !ok {
		return 0, content, fmt.Errorf("无法识别的质量单位 %q，原始表示已保留", unit)
	}
	return mg, strings.TrimSpace(content), nil
}
