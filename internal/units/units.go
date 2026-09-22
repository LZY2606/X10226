// Package units keeps the original representation of every domain quantity and
// performs deterministic conversions to canonical units only in the derived
// layer. Values that cannot be converted stay where they are with a diagnostic.
package units

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Canonical units used by derived calculations.
const (
	CanonTime = "s"
	CanonTemp = "degC"
	CanonMass = "mg"
	CanonRate = "degC/min"
)

var amountRe = regexp.MustCompile(`^\s*([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)\s*([^\s]*)\s*$`)

// Q is a physical quantity preserving raw text, parsed magnitude, unit and
// dimension. Value is the magnitude expressed in the original unit.
type Q struct {
	Raw   string  `json:"raw"`
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Dim   Dim     `json:"dim"`
}

// Dim is the dimension of a quantity.
type Dim string

const (
	DimNone Dim = ""
	DimTime Dim = "time"
	DimTemp Dim = "temperature"
	DimMass Dim = "mass"
	DimRate Dim = "rate"
)

// Parse parses "<number><unit>" text. An unknown unit is preserved: the
// returned quantity carries DimNone and a non-nil diagnostic.
func Parse(raw string) (Q, error) {
	m := amountRe.FindStringSubmatch(raw)
	if m == nil {
		return Q{Raw: strings.TrimSpace(raw)}, fmt.Errorf("unparseable quantity %q", raw)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return Q{Raw: strings.TrimSpace(raw)}, err
	}
	unit := canonicalUnit(m[2])
	dim, ok := Dimension(unit)
	if !ok {
		return Q{Raw: strings.TrimSpace(raw), Value: v, Unit: m[2], Dim: DimNone},
			fmt.Errorf("unknown unit %q", m[2])
	}
	return Q{Raw: strings.TrimSpace(raw), Value: v, Unit: unit, Dim: dim}, nil
}

func canonicalUnit(u string) string {
	switch strings.ToLower(u) {
	case "s", "sec", "secs", "second", "seconds":
		return "s"
	case "min", "minute", "minutes":
		return "min"
	case "h", "hr", "hour", "hours":
		return "h"
	case "c", "°c", "oc", "degc", "degree_c":
		return "degC"
	case "k":
		return "K"
	case "mg":
		return "mg"
	case "g":
		return "g"
	case "ug", "µg":
		return "ug"
	case "%":
		return "%"
	case "c/min", "degc/min", "k/min", "°c/min":
		return "degC/min"
	}
	return u
}

// Dimension reports the dimension of a canonical unit.
func Dimension(unit string) (Dim, bool) {
	switch unit {
	case "s", "min", "h":
		return DimTime, true
	case "degC", "K":
		return DimTemp, true
	case "mg", "g", "ug":
		return DimMass, true
	case "degC/min", "K/min":
		return DimRate, true
	case "%":
		return DimNone, true
	}
	return DimNone, false
}

// ToCanon converts a quantity to the canonical unit of want.
func (q Q) ToCanon(want Dim) (float64, error) {
	if q.Dim == DimNone || q.Dim != want {
		return 0, fmt.Errorf("cannot convert %q (dim=%q) to %q", q.Raw, q.Dim, want)
	}
	switch want {
	case DimTime:
		switch q.Unit {
		case "s":
			return q.Value, nil
		case "min":
			return q.Value * 60, nil
		case "h":
			return q.Value * 3600, nil
		}
	case DimTemp:
		switch q.Unit {
		case "degC":
			return q.Value, nil
		case "K":
			return q.Value - 273.15, nil
		}
	case DimMass:
		switch q.Unit {
		case "mg":
			return q.Value, nil
		case "g":
			return q.Value * 1000, nil
		case "ug":
			return q.Value / 1000, nil
		}
	case DimRate:
		switch q.Unit {
		case "degC/min", "K/min":
			return q.Value, nil
		}
	}
	return 0, fmt.Errorf("no conversion for %q", q.Raw)
}

// Canon returns the canonical unit label for a dimension.
func Canon(want Dim) string {
	switch want {
	case DimTime:
		return CanonTime
	case DimTemp:
		return CanonTemp
	case DimMass:
		return CanonMass
	case DimRate:
		return CanonRate
	}
	return ""
}
