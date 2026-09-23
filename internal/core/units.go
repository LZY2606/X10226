package core

import (
	"fmt"
	"strconv"
	"strings"
)

// Canonical units used by the derived layer.
const (
	CanonMass = "mg"
	CanonTemp = "C"
	CanonTime = "s"
	CanonFlow = "mL/min"
	CanonRate = "mg/min"
)

// massFactors converts a unit to milligrams.
var massFactors = map[string]float64{
	"mg": 1, "g": 1000, "ug": 0.001, "µg": 0.001, "kg": 1e6,
}

// timeFactors converts a unit to seconds.
var timeFactors = map[string]float64{
	"s": 1, "sec": 1, "min": 60, "h": 3600, "ms": 0.001,
}

// flowFactors converts a unit to mL/min.
var flowFactors = map[string]float64{
	"mL/min": 1, "ml/min": 1, "sccm": 1, "L/min": 1000,
}

// rateFactors converts a mass-rate unit to mg/min.
var rateFactors = map[string]float64{
	"mg/min": 1, "mg/s": 60, "ug/min": 0.001, "g/min": 1000,
}

// ConvertMass converts v expressed in unit to milligrams.
func ConvertMass(v float64, unit string) (float64, bool) {
	f, ok := massFactors[strings.TrimSpace(unit)]
	if !ok {
		return 0, false
	}
	return v * f, true
}

// ConvertTime converts v expressed in unit to seconds.
func ConvertTime(v float64, unit string) (float64, bool) {
	f, ok := timeFactors[strings.TrimSpace(unit)]
	if !ok {
		return 0, false
	}
	return v * f, true
}

// ConvertTemp converts v expressed in unit to Celsius.
func ConvertTemp(v float64, unit string) (float64, bool) {
	switch strings.TrimSpace(unit) {
	case "C", "°C", "degC":
		return v, true
	case "K":
		return v - 273.15, true
	case "F":
		return (v - 32) * 5 / 9, true
	}
	return 0, false
}

// ConvertFlow converts v expressed in unit to mL/min.
func ConvertFlow(v float64, unit string) (float64, bool) {
	f, ok := flowFactors[strings.TrimSpace(unit)]
	if !ok {
		return 0, false
	}
	return v * f, true
}

// ConvertRate converts v expressed in unit to mg/min.
func ConvertRate(v float64, unit string) (float64, bool) {
	f, ok := rateFactors[strings.TrimSpace(unit)]
	if !ok {
		return 0, false
	}
	return v * f, true
}

// ParseScalar parses a raw string like "100.5" or "100.5 mg" into a RawValue.
// The expected dimension ("mass", "time", "temp", "flow", "rate") selects the
// conversion table. On failure the raw text is preserved with a diagnostic.
func ParseScalar(raw, dimension, basis string) RawValue {
	rv := RawValue{Raw: raw}
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		rv.Diagnostic = &Diagnostic{Code: "empty_value", Message: "no value present"}
		return rv
	}
	num, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		rv.Diagnostic = &Diagnostic{Code: "unparseable_number", Message: fmt.Sprintf("cannot parse %q as number", fields[0])}
		return rv
	}
	unit := ""
	if len(fields) > 1 {
		unit = fields[1]
	}
	var conv float64
	var ok bool
	var canon string
	switch dimension {
	case "mass":
		conv, ok = ConvertMass(num, orDefault(unit, "mg"))
		canon = CanonMass
	case "time":
		conv, ok = ConvertTime(num, orDefault(unit, "s"))
		canon = CanonTime
	case "temp":
		conv, ok = ConvertTemp(num, orDefault(unit, "C"))
		canon = CanonTemp
	case "flow":
		conv, ok = ConvertFlow(num, orDefault(unit, "mL/min"))
		canon = CanonFlow
	case "rate":
		conv, ok = ConvertRate(num, orDefault(unit, "mg/min"))
		canon = CanonRate
	default:
		rv.Diagnostic = &Diagnostic{Code: "unknown_dimension", Message: dimension}
		return rv
	}
	if !ok {
		rv.Diagnostic = &Diagnostic{
			Code:    "unconvertible_unit",
			Message: fmt.Sprintf("unit %q cannot be converted to %s; value kept in raw form", unit, canon),
		}
		return rv
	}
	rv.Parsed = &Quantity{Value: conv, Unit: canon, Basis: basis}
	return rv
}

func orDefault(s, d string) string {
	if strings.TrimSpace(s) == "" {
		return d
	}
	return s
}
