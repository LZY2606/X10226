package domain

import (
	"math"
	"strconv"
	"strings"
)

// SI bases used by the derived layer.
const (
	UnitSeconds = "s"
	UnitCelsius = "degC"
	UnitKelvin  = "K"
	UnitMg      = "mg"
	UnitG       = "g"
	UnitPercent = "%"
)

// ParseRawValue preserves the raw text/unit and, when the unit is known,
// converts into the derived-layer base unit. Unconvertible values stay in
// place with a diagnostic instead of being dropped.
func ParseRawValue(text, unit, want string) RawValue {
	text = strings.TrimSpace(text)
	unit = strings.TrimSpace(unit)
	rv := RawValue{Text: text, Unit: unit}
	v, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil {
		rv.Diagnostic = "not a number: " + err.Error()
		return rv
	}
	factor, ok := conversion(unit, want)
	if !ok {
		rv.Diagnostic = "unknown unit " + unit + " (want " + want + "); kept as-is"
		return rv
	}
	converted := v * factor
	if math.IsNaN(converted) || math.IsInf(converted, 0) {
		rv.Diagnostic = "non-finite value"
		return rv
	}
	rv.Value = converted
	rv.Converted = true
	return rv
}

func conversion(unit, want string) (float64, bool) {
	if unit == want {
		return 1, true
	}
	switch want {
	case UnitSeconds:
		switch unit {
		case "s", "sec":
			return 1, true
		case "min":
			return 60, true
		case "h", "hr":
			return 3600, true
		}
	case UnitCelsius:
		switch unit {
		case "C", "degC", "°C":
			return 1, true
		}
	case UnitMg:
		switch unit {
		case "mg":
			return 1, true
		case "g":
			return 1000, true
		case "ug", "µg":
			return 0.001, true
		}
	}
	return 0, false
}
