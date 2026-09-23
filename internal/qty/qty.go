// Package qty provides unit-aware quantities. Raw values keep their original
// textual representation; conversion to canonical units happens only in the
// derived layer. Values that cannot be converted stay in place and carry a
// diagnostic instead of being silently dropped.
package qty

import (
	"fmt"
	"strconv"
	"strings"
)

// Canonical units used by the derived layer.
const (
	UnitS     = "s"
	UnitC     = "C"
	UnitMg    = "mg"
	UnitPct   = "%"
	UnitMlMin = "mL/min"
	UnitCMin  = "C/min"
)

// Quantity is a domain value with a canonical unit and an optional coordinate
// basis (e.g. "valve-time" vs "sample-time", "furnace-thermocouple").
type Quantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Basis string  `json:"basis,omitempty"`
}

// Raw is the original representation of a value exactly as submitted.
type Raw struct {
	Raw  string `json:"raw"`
	Unit string `json:"unit"`
}

// Parsed is the outcome of interpreting a Raw value.
type Parsed struct {
	Q          Quantity
	OK         bool
	Diagnostic string
}

// Parse converts a raw value into canonical units. On failure the returned
// Diagnostic explains why; the caller must keep the raw value in place.
func Parse(raw Raw, basis string) Parsed {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw.Raw), 64)
	if err != nil {
		return Parsed{Diagnostic: fmt.Sprintf("无法解析数值 %q", raw.Raw)}
	}
	cv, cu, ok := Convert(v, raw.Unit)
	if !ok {
		return Parsed{Diagnostic: fmt.Sprintf("无法转换单位 %q，值保留在原位置", raw.Unit)}
	}
	return Parsed{Q: Quantity{Value: cv, Unit: cu, Basis: basis}, OK: true}
}

// Convert converts v expressed in unit into a canonical unit.
func Convert(v float64, unit string) (float64, string, bool) {
	switch strings.TrimSpace(unit) {
	case "s", "sec", "second", "seconds":
		return v, UnitS, true
	case "min":
		return v * 60, UnitS, true
	case "h", "hr":
		return v * 3600, UnitS, true
	case "C", "°C", "degC", "celsius":
		return v, UnitC, true
	case "K", "kelvin":
		return v - 273.15, UnitC, true
	case "F", "°F":
		return (v - 32) * 5 / 9, UnitC, true
	case "mg":
		return v, UnitMg, true
	case "g":
		return v * 1000, UnitMg, true
	case "ug", "µg":
		return v / 1000, UnitMg, true
	case "%", "pct", "percent":
		return v, UnitPct, true
	case "mL/min", "ml/min", "sccm":
		return v, UnitMlMin, true
	case "C/min", "K/min":
		return v, UnitCMin, true
	}
	return 0, "", false
}
