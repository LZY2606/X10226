package core

import "strings"

// Canonical bases: time in seconds, temperature in Celsius, mass in
// milligrams, flow in mL/min. Raw text is preserved untouched at import;
// conversion happens only here in the derived layer. Values that cannot be
// converted stay in place (in the raw record) and carry a diagnostic.

func normUnit(u string) string {
	return strings.ToLower(strings.TrimSpace(u))
}

func ConvertTime(v float64, unit string) (float64, bool) {
	switch normUnit(unit) {
	case "s", "sec", "secs":
		return v, true
	case "min":
		return v * 60, true
	case "h", "hr", "hrs":
		return v * 3600, true
	}
	return 0, false
}

func ConvertTemp(v float64, unit string) (float64, bool) {
	switch normUnit(unit) {
	case "c", "°c", "degc":
		return v, true
	case "k":
		return v - 273.15, true
	}
	return 0, false
}

func ConvertMass(v float64, unit string) (float64, bool) {
	switch normUnit(unit) {
	case "mg":
		return v, true
	case "g":
		return v * 1000, true
	case "ug", "µg":
		return v / 1000, true
	}
	return 0, false
}

func ConvertFlow(v float64, unit string) (float64, bool) {
	switch normUnit(unit) {
	case "ml/min", "ml_min", "mlmin":
		return v, true
	case "l/min":
		return v * 1000, true
	}
	return 0, false
}
