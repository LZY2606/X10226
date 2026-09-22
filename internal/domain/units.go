package domain

import "fmt"

// Quantity 是携带单位与坐标基准的领域量。
// 导入层保留原始表示；只有派生层才做规范化。
type Quantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`  // 例如 "s","min","mg","g","C","K","%","mL/min"
	Basis string  `json:"basis"` // 坐标基准，例如 "elapsed","furnace-setpoint","as-received"
}

// Diagnostic 描述无法转换或格式异常的值；原值留在原位置，不丢弃。
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Context string `json:"context,omitempty"`
}

// 规范维度。
const (
	DimTime  = "time"       // 规范单位 s
	DimMass  = "mass"       // 规范单位 mg
	DimTemp  = "temperature"// 规范单位 C
	DimFlow  = "flow"       // 规范单位 mL/min
	DimPct   = "percent"    // 规范单位 %
)

// Canonical 把 Quantity 转换到指定维度的规范单位。
// 无法转换时返回 ok=false，调用方必须保留原值并附诊断。
func Canonical(q Quantity, dim string) (float64, bool) {
	switch dim {
	case DimTime:
		switch q.Unit {
		case "s", "sec", "second", "seconds":
			return q.Value, true
		case "min", "minute", "minutes":
			return q.Value * 60, true
		case "h", "hr", "hour", "hours":
			return q.Value * 3600, true
		}
	case DimMass:
		switch q.Unit {
		case "mg":
			return q.Value, true
		case "g":
			return q.Value * 1000, true
		case "ug", "µg":
			return q.Value / 1000, true
		case "kg":
			return q.Value * 1e6, true
		}
	case DimTemp:
		switch q.Unit {
		case "C", "degC", "°C":
			return q.Value, true
		case "K":
			return q.Value - 273.15, true
		case "F":
			return (q.Value - 32) * 5 / 9, true
		}
	case DimFlow:
		switch q.Unit {
		case "mL/min", "ml/min":
			return q.Value, true
		case "L/min":
			return q.Value * 1000, true
		case "sccm":
			return q.Value, true
		}
	case DimPct:
		switch q.Unit {
		case "%", "pct", "percent":
			return q.Value, true
		case "fraction":
			return q.Value * 100, true
		}
	}
	return 0, false
}

// MustCanonical 是 Canonical 的诊断包装：失败时生成诊断并返回原值。
func MustCanonical(q Quantity, dim, context string) (float64, *Diagnostic) {
	if v, ok := Canonical(q, dim); ok {
		return v, nil
	}
	return q.Value, &Diagnostic{
		Code:    "unit_unconvertible",
		Message: fmt.Sprintf("unit %q cannot be normalized to dimension %s; raw value kept in place", q.Unit, dim),
		Context: context,
	}
}
