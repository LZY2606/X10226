package domain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Q is a measured quantity. The original textual representation is always
// preserved; normalization into the canonical unit happens only in the
// derivation layer. A value that cannot be converted stays in place with a
// diagnostic instead of being dropped for the sake of a chart.
type Q struct {
	Raw        string  `json:"raw"`
	Value      float64 `json:"value,omitempty"`
	Unit       string  `json:"unit,omitempty"`
	Valid      bool    `json:"valid"`
	Diagnostic string  `json:"diagnostic,omitempty"`
}

// Canonical units and their coordinate bases.
const (
	UnitMass   = "mg"
	UnitTemp   = "degC"
	UnitTime   = "s"
	UnitRate   = "degC/min"
	UnitPct    = "%"
	UnitFlow   = "mL/min"
	UnitBasis  = "furnace_time_s" // sample & furnace share the furnace clock
	ReactBasis = "reaction_time_s"
)

// mass factors into mg
var massFactors = map[string]float64{"mg": 1, "g": 1000, "kg": 1e6}
var timeFactors = map[string]float64{"s": 1, "sec": 1, "min": 60, "mins": 60, "minute": 60, "minutes": 60}
var flowFactors = map[string]float64{"ml/min": 1, "mL/min": 1, "sccm": 1}

// ParseQ parses raw against a declared unit (from the column header).
// kind selects the conversion family: "mass","temp","time","rate","pct","flow".
func ParseQ(raw, declared, kind string) Q {
	q := Q{Raw: raw}
	declared = strings.TrimSpace(declared)
	text := strings.TrimSpace(raw)
	num, unit := splitNumberUnit(text, declared)
	if num == "" {
		q.Diagnostic = fmt.Sprintf("empty or non-numeric value %q", raw)
		return q
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		q.Diagnostic = fmt.Sprintf("cannot parse number %q: %v", num, err)
		return q
	}
	switch kind {
	case "mass":
		conv, ok := massFactors[unit]
		if !ok {
			q.Diagnostic = fmt.Sprintf("unknown mass unit %q retained at original position", unit)
			return q
		}
		q.Value, q.Unit, q.Valid = f*conv, UnitMass, true
	case "time":
		conv, ok := timeFactors[unit]
		if !ok {
			q.Diagnostic = fmt.Sprintf("unknown time unit %q", unit)
			return q
		}
		q.Value, q.Unit, q.Valid = f*conv, UnitTime, true
	case "temp":
		u := strings.ToLower(strings.TrimSpace(unit))
		switch u {
		case "c", "°c", "degc", "":
			q.Value, q.Unit, q.Valid = f, UnitTemp, true
		case "k":
			q.Value, q.Unit, q.Valid = f-273.15, UnitTemp, true
		default:
			q.Diagnostic = fmt.Sprintf("unknown temperature unit %q", unit)
			return q
		}
	case "rate":
		u := strings.ToLower(strings.TrimSpace(unit))
		if u == "" || strings.Contains(u, "c/min") || strings.Contains(u, "c min") {
			q.Value, q.Unit, q.Valid = f, UnitRate, true
		} else {
			q.Diagnostic = fmt.Sprintf("unknown heating-rate unit %q", unit)
			return q
		}
	case "pct":
		u := strings.TrimSpace(unit)
		switch u {
		case "", "%":
			q.Value, q.Unit, q.Valid = f, UnitPct, true
		default:
			q.Diagnostic = fmt.Sprintf("unknown percent unit %q", unit)
			return q
		}
	case "flow":
		conv, ok := flowFactors[unit]
		if !ok {
			q.Diagnostic = fmt.Sprintf("unknown flow unit %q", unit)
			return q
		}
		q.Value, q.Unit, q.Valid = f*conv, UnitFlow, true
	default:
		q.Diagnostic = "unknown quantity kind"
		return q
	}
	if math.IsNaN(q.Value) || math.IsInf(q.Value, 0) {
		q.Valid = false
		q.Diagnostic = "non-finite value"
	}
	return q
}

// splitNumberUnit splits "12.3 mg" into ("12.3","mg"); falls back to declared.
func splitNumberUnit(text, declared string) (string, string) {
	text = strings.TrimSpace(text)
	i := 0
	for i < len(text) {
		r := text[i]
		if (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '+' || r == 'e' || r == 'E' {
			i++
			continue
		}
		break
	}
	num := strings.TrimSpace(text[:i])
	unit := strings.TrimSpace(text[i:])
	if unit == "" {
		unit = declared
	}
	return num, unit
}

// Fmt renders a normalized value with unit for the UI; invalid values keep raw.
func (q Q) Fmt() string {
	if !q.Valid {
		return q.Raw
	}
	return fmt.Sprintf("%g %s", q.Value, q.Unit)
}
