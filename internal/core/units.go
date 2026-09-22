package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ParseQuantity preserves the original representation and only normalises in
// the derived layer. Unconvertible values stay in place with a diagnostic.
//
// dim: "time" -> s, "temp" -> degC, "mass" -> mg, "flow" -> mL/min, "pct" -> %.
func ParseQuantity(raw, unitRaw, dim string) Quantity {
	q := Quantity{Raw: strings.TrimSpace(raw), UnitRaw: strings.TrimSpace(unitRaw)}
	v, err := strconv.ParseFloat(q.Raw, 64)
	if err != nil {
		q.Diag = fmt.Sprintf("无法解析数值 %q", q.Raw)
		return q
	}
	u := strings.ToLower(q.UnitRaw)
	switch dim {
	case "time":
		switch u {
		case "", "s", "sec", "secs", "second", "seconds":
			q.Value, q.Unit = v, "s"
		case "min", "minute", "minutes":
			q.Value, q.Unit = v*60, "s"
		case "h", "hour", "hours":
			q.Value, q.Unit = v*3600, "s"
		case "ms":
			q.Value, q.Unit = v/1000, "s"
		default:
			q.Diag = fmt.Sprintf("未知时间单位 %q", q.UnitRaw)
			return q
		}
	case "temp":
		switch u {
		case "", "c", "°c", "degc", "deg c", "celsius":
			q.Value, q.Unit = v, "degC"
		case "k", "kelvin":
			q.Value, q.Unit = v-273.15, "degC"
		case "f", "degf", "fahrenheit":
			q.Value, q.Unit = (v-32)*5/9, "degC"
		default:
			q.Diag = fmt.Sprintf("未知温度单位 %q", q.UnitRaw)
			return q
		}
	case "mass":
		switch u {
		case "", "mg":
			q.Value, q.Unit = v, "mg"
		case "g":
			q.Value, q.Unit = v*1000, "mg"
		case "ug", "µg":
			q.Value, q.Unit = v/1000, "mg"
		default:
			q.Diag = fmt.Sprintf("未知质量单位 %q", q.UnitRaw)
			return q
		}
	case "flow":
		switch u {
		case "", "ml/min", "mlm", "ml min-1":
			q.Value, q.Unit = v, "mL/min"
		default:
			q.Diag = fmt.Sprintf("未知流量单位 %q", q.UnitRaw)
			return q
		}
	case "pct":
		switch u {
		case "", "%":
			q.Value, q.Unit = v, "%"
		case "fraction":
			q.Value, q.Unit = v*100, "%"
		default:
			q.Diag = fmt.Sprintf("未知百分比单位 %q", q.UnitRaw)
			return q
		}
	case "rate_mg_s":
		switch u {
		case "", "mg/s":
			q.Value, q.Unit = v, "mg/s"
		case "mg/min":
			q.Value, q.Unit = v/60, "mg/s"
		case "ug/s":
			q.Value, q.Unit = v/1000, "mg/s"
		default:
			q.Diag = fmt.Sprintf("未知漂移率单位 %q", q.UnitRaw)
			return q
		}
	case "rate_c_min":
		switch u {
		case "", "c/min", "degc/min", "°c/min":
			q.Value, q.Unit = v, "degC/min"
		case "c/s", "degc/s":
			q.Value, q.Unit = v*60, "degC/min"
		default:
			q.Diag = fmt.Sprintf("未知升温率单位 %q", q.UnitRaw)
			return q
		}
	default:
		q.Diag = "未知量纲 " + dim
		return q
	}
	q.OK = true
	return q
}

func (q Quantity) String() string {
	if !q.OK {
		return q.Raw + " " + q.UnitRaw + "(未转换:" + q.Diag + ")"
	}
	return strconv.FormatFloat(q.Value, 'g', -1, 64) + " " + q.Unit
}

// UnmarshalJSON accepts the import form {"raw":"12.3","unit":"s"} and only
// stores the raw representation; normalisation is an explicit later step.
func (q *Quantity) UnmarshalJSON(b []byte) error {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	q.Raw = getS(m, "raw")
	q.UnitRaw = getS(m, "unit")
	if q.UnitRaw == "" {
		q.UnitRaw = getS(m, "unit_raw")
	}
	if v, ok := m["value"]; ok {
		q.Value = toF(v)
		q.OK, _ = m["ok"].(bool)
	}
	if u, ok := m["unit_norm"].(string); ok {
		q.Unit = u
	}
	if dg, ok := m["diag"].(string); ok {
		q.Diag = dg
	}
	return nil
}

func (q Quantity) MarshalJSON() ([]byte, error) {
	type alias Quantity
	return json.Marshal(alias(q))
}

func getS(m map[string]any, k string) string {
	if v, ok := m[k]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
