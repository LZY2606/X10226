package tga

import (
	"fmt"
	"strconv"
	"strings"
)

// 列的物理坐标基准 → 规范化单位。
var colBase = map[string]string{
	"time_s":         "s",
	"temp_c":         "c",
	"target_temp_c":  "c",
	"mass_mg":        "mg",
	"blank_mass_mg":  "mg",
	"a_mg":           "mg",
	"b_mg_per_s":     "mg/s",
}

var unitAlias = map[string]string{
	"s": "s", "sec": "s", "seconds": "s",
	"c": "c", "°c": "c", "degc": "c", "k": "k",
	"mg": "mg", "g": "g", "kg": "kg", "ug": "ug", "µg": "ug",
	"mg/min": "mg/min", "mg/s": "mg/s", "mg_per_s": "mg/s",
}

// ParseCell 解析一个原始单元格。列名决定坐标基准；无法转换时 Normalized 留在 nil 并附诊断。
func ParseCell(layer, col, raw string, rowIdx int) Cell {
	c := Cell{Q: Q{Original: raw}}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		c.Diags = append(c.Diags, Diagnostic{SevError, layer, rowIdx, col, "empty_cell", "单元格为空", raw})
		return c
	}
	num, unit, err := splitNumUnit(raw)
	if err != nil {
		c.Diags = append(c.Diags, Diagnostic{SevError, layer, rowIdx, col, "unparseable_quantity", err.Error(), raw})
		return c
	}
	base := colBase[col]
	want, hasBase := canonicalUnit(base)
	c.Q.Unit = unit
	if !hasBase {
		c.Diags = append(c.Diags, Diagnostic{SevWarning, layer, rowIdx, col, "unknown_column_basis", "未知列基准，无法规范化", raw})
		return c
	}
	norm, perr := convertTo(num, unit, want)
	if perr != nil {
		c.Diags = append(c.Diags, Diagnostic{SevError, layer, rowIdx, col, perr.(*convErr).Code, perr.Error(), raw})
		return c
	}
	c.Q.Normalized = &norm
	return c
}

type convErr struct{ Code, Msg string }

func (e *convErr) Error() string { return e.Msg }

func splitNumUnit(raw string) (float64, string, error) {
	i := 0
	for i < len(raw) {
		r := raw[i]
		if (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '+' || r == 'e' || r == 'E' {
			i++
			continue
		}
		break
	}
	numPart := strings.TrimSpace(raw[:i])
	unitPart := strings.ToLower(strings.TrimSpace(raw[i:]))
	if numPart == "" {
		return 0, "", fmt.Errorf("缺少数值")
	}
	v, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, "", fmt.Errorf("数值无法解析: %q", numPart)
	}
	return v, unitPart, nil
}

func canonicalUnit(base string) (string, bool) {
	switch base {
	case "s", "c", "mg", "mg/s":
		return base, true
	}
	return "", false
}

func convertTo(v float64, unit, want string) (float64, error) {
	if unit == "" {
		return v, nil // 无单位标注 → 按列基准（导入时保留原始表示）
	}
	u, ok := unitAlias[unit]
	if !ok {
		return 0, &convErr{"unknown_unit", fmt.Sprintf("未知单位 %q", unit)}
	}
	switch want {
	case "s":
		if u == "s" {
			return v, nil
		}
	case "c":
		switch u {
		case "c":
			return v, nil
		case "k":
			return v - 273.15, nil
		}
	case "mg":
		switch u {
		case "mg":
			return v, nil
		case "ug":
			return v / 1000, nil
		case "g":
			return v * 1000, nil
		case "kg":
			return v * 1e6, nil
		}
	case "mg/s":
		if u == "mg/s" {
			return v, nil
		}
	}
	return 0, &convErr{"incompatible_unit", fmt.Sprintf("单位 %q 与列基准 %q 不兼容", unit, want)}
}

// HasDiagLevel 判断诊断列表是否含某等级。
func HasDiagLevel(ds []Diagnostic, sev string) bool {
	for _, d := range ds {
		if d.Severity == sev {
			return true
		}
	}
	return false
}
