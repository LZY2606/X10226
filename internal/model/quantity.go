package model

import (
	"math"
	"strconv"
	"strings"
)

// ParseQValue parses raw text carrying a declared unit. The original text
// is preserved; unparseable input stays in place with Valid=false and a
// diagnostic rather than being dropped.
func ParseQValue(raw, unit string, basis string) QValue {
	q := QValue{Original: strings.TrimSpace(raw), Unit: Unit(strings.TrimSpace(unit)), Basis: basis}
	if q.Unit == "" {
		q.Unit = UnitUnknown
	}
	s := strings.ReplaceAll(q.Original, ",", ".")
	s = strings.TrimSpace(s)
	if s == "" {
		q.Valid = false
		q.Diagnostic = "empty value"
		return q
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		q.Diagnostic = "unparseable numeric value: " + err.Error()
		return q
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		q.Diagnostic = "non-finite value"
		return q
	}
	q.Value, q.Valid = v, true
	return q
}

// ToMin converts a time quantity to minutes. Returns ok=false when the
// unit is not a recognised time unit; the caller must keep the original.
func (q QValue) ToMin() (float64, bool) {
	if !q.Valid {
		return 0, false
	}
	switch q.Unit {
	case UnitMin:
		return q.Value, true
	case UnitSec:
		return q.Value / 60.0, true
	}
	return 0, false
}

// ToMG converts a mass quantity to milligrams.
func (q QValue) ToMG() (float64, bool) {
	if !q.Valid {
		return 0, false
	}
	switch q.Unit {
	case UnitMG:
		return q.Value, true
	case UnitG:
		return q.Value * 1000.0, true
	}
	return 0, false
}

// ToC converts a temperature quantity to degrees Celsius.
func (q QValue) ToC() (float64, bool) {
	if !q.Valid {
		return 0, false
	}
	switch q.Unit {
	case UnitC:
		return q.Value, true
	case UnitK:
		return q.Value - 273.15, true
	}
	return 0, false
}
