package domain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// ParseQuantity keeps the original representation and normalizes into the
// canonical unit when possible. Unconvertible values stay in place and carry
// a diagnostic; they are never dropped to make plotting possible.
func ParseQuantity(rawText string, v any, unit string, canonicalUnit string) Quantity {
	q := Quantity{Raw: rawText, Unit: strings.TrimSpace(unit)}
	f, err := toFloat(v)
	if err != nil {
		q.Diagnostic = fmt.Sprintf("unparseable numeric value %q: %v", rawText, err)
		return q
	}
	q.Value = f
	conv, cu, err := convertUnit(f, unit, canonicalUnit)
	if err != nil {
		q.Diagnostic = err.Error()
		q.Basis = "original-unit"
		return q
	}
	q.Value = conv
	q.Unit = cu
	q.Converted = true
	q.Basis = "canonical"
	return q
}

func toFloat(v any) (float64, error) {
	switch x := v.(type) {
	case float64:
		return x, nil
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, fmt.Errorf("empty")
		}
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			// tolerate a trailing unit, e.g. "10.2 mg"
			fields := strings.Fields(s)
			if len(fields) > 1 {
				if f2, e2 := strconv.ParseFloat(fields[0], 64); e2 == nil {
					return f2, nil
				}
			}
			return 0, err
		}
		return f, nil
	case int:
		return float64(x), nil
	case int64:
		return float64(x), nil
	case bool:
		return 0, fmt.Errorf("boolean is not a number")
	case nil:
		return 0, fmt.Errorf("missing")
	default:
		return 0, fmt.Errorf("unsupported type %T", v)
	}
}

func convertUnit(v float64, from, canonical string) (float64, string, error) {
	from = normUnit(from)
	canonical = normUnit(canonical)
	if from == canonical {
		return v, canonical, nil
	}
	// mass -> mg
	if canonical == "mg" {
		switch from {
		case "mg":
			return v, "mg", nil
		case "ug":
			return v / 1000, "mg", nil
		case "g":
			return v * 1000, "mg", nil
		case "kg":
			return v * 1e6, "mg", nil
		}
	}
	// temperature -> degC
	if canonical == "degc" {
		switch from {
		case "degc", "c":
			return v, "degC", nil
		case "k":
			return v - 273.15, "degC", nil
		case "degf", "f":
			return (v - 32) * 5 / 9, "degC", nil
		}
	}
	// time -> s
	if canonical == "s" {
		switch from {
		case "s", "sec":
			return v, "s", nil
		case "min":
			return v * 60, "s", nil
		case "h":
			return v * 3600, "s", nil
		}
	}
	// flow -> mL/min
	if canonical == "ml/min" {
		switch from {
		case "ml/min":
			return v, "mL/min", nil
		case "l/min":
			return v * 1000, "mL/min", nil
		}
	}
	// rate -> K/min
	if canonical == "k/min" {
		switch from {
		case "k/min", "c/min":
			return v, "K/min", nil
		case "k/s":
			return v * 60, "K/min", nil
		}
	}
	// mass rate -> mg/min
	if canonical == "mg/min" {
		switch from {
		case "mg/min":
			return v, "mg/min", nil
		case "mg/s":
			return v * 60, "mg/min", nil
		case "g/min":
			return v * 1000, "mg/min", nil
		}
	}
	return 0, canonical, fmt.Errorf("cannot convert %q into canonical %q", from, canonical)
}

func normUnit(u string) string {
	return strings.ToLower(strings.TrimSpace(u))
}

// TempC returns canonical degC; ok=false if the point's temperature could not
// be converted (the raw value remains on the point with a diagnostic).
func TempC(q Quantity) (float64, bool) {
	if q.Diagnostic != "" || (q.Unit != "degC" && q.Unit != "") {
		return 0, false
	}
	return q.Value, true
}

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) }
