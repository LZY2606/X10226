// Package units keeps physical quantities together with the unit, coordinate
// basis and the original textual representation as imported. Normalization to a
// canonical internal unit happens only in the derived layer; a value that
// cannot be converted is kept in place with a diagnostic instead of dropped.
package units

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Dimension identifies the physical dimension of a quantity.
type Dimension string

const (
	DimMass     Dimension = "mass"
	DimTime     Dimension = "time"
	DimTemp     Dimension = "temperature"
	DimRate     Dimension = "mass_rate" // dmass/dtime
	DimHeating  Dimension = "heating_rate"
	DimFraction Dimension = "fraction" // dimensionless ratio or percent
	DimNone     Dimension = "none"
)

// Quantity is a value carrying its unit, coordinate basis and the exact
// original token submitted by the operator.
type Quantity struct {
	Value float64 `json:"value"`           // normalized value in CanonicalUnit
	Unit  string  `json:"unit"`            // unit id as imported/normalized
	Raw   string  `json:"raw,omitempty"`   // original textual representation
	Basis string  `json:"basis,omitempty"` // coordinate/reference basis
}

// Canonical units used by the derived layer.
var CanonicalUnit = map[Dimension]string{
	DimMass:     "mg",
	DimTime:     "s",
	DimTemp:     "K",
	DimRate:     "mg/min",
	DimHeating:  "K/min",
	DimFraction: "fraction",
}

type conv struct {
	dim   Dimension
	toBase func(float64) float64
	fromBase func(float64) float64
}

var unitTable = map[string]conv{
	// mass, base mg
	"mg": {DimMass, identity, identity},
	"g":  {DimMass, func(v float64) float64 { return v * 1000 }, func(v float64) float64 { return v / 1000 }},
	"ug": {DimMass, func(v float64) float64 { return v / 1000 }, func(v float64) float64 { return v * 1000 }},
	// time, base s
	"s":   {DimTime, identity, identity},
	"min": {DimTime, func(v float64) float64 { return v * 60 }, func(v float64) float64 { return v / 60 }},
	// temperature, base K
	"C":   {DimTemp, func(v float64) float64 { return v + 273.15 }, func(v float64) float64 { return v - 273.15 }},
	"K":   {DimTemp, identity, identity},
	// mass rate, base mg/min
	"mg/min": {DimRate, identity, identity},
	"mg/s":   {DimRate, func(v float64) float64 { return v * 60 }, func(v float64) float64 { return v / 60 }},
	"ug/min": {DimRate, func(v float64) float64 { return v / 1000 }, func(v float64) float64 { return v * 1000 }},
	// heating rate, base K/min
	"K/min": {DimHeating, identity, identity},
	"C/min": {DimHeating, identity, identity}, // increments are equal
	// fraction
	"fraction": {DimFraction, identity, identity},
	"%":        {DimFraction, func(v float64) float64 { return v / 100 }, func(v float64) float64 { return v * 100 }},
	"percent":  {DimFraction, func(v float64) float64 { return v / 100 }, func(v float64) float64 { return v * 100 }},
	"":         {DimNone, identity, identity},
}

func identity(v float64) float64 { return v }

// Normalize parses a raw token in the given unit into a canonical Quantity.
// An unknown/empty unit returns a Quantity with NaN value and a non-nil error;
// callers must retain the raw token and attach the diagnostic.
func Normalize(raw, unit, basis string) (Quantity, error) {
	c, ok := unitTable[strings.TrimSpace(unit)]
	if !ok {
		return Quantity{Value: math.NaN(), Unit: unit, Raw: raw, Basis: basis},
			fmt.Errorf("unknown unit %q (raw=%q, basis=%q)", unit, raw, basis)
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return Quantity{Value: math.NaN(), Unit: unit, Raw: raw, Basis: basis},
			fmt.Errorf("unparsable numeric value %q: %w", raw, err)
	}
	return Quantity{Value: c.toBase(v), Unit: CanonicalUnit[c.dim], Raw: raw, Basis: basis}, nil
}

// MustNormalize is used by generators/tests where inputs are known-good.
func MustNormalize(raw, unit, basis string) Quantity {
	q, err := Normalize(raw, unit, basis)
	if err != nil {
		panic(err)
	}
	return q
}

// DimensionOf reports the dimension of a registered unit.
func DimensionOf(unit string) Dimension {
	if c, ok := unitTable[strings.TrimSpace(unit)]; ok {
		return c.dim
	}
	return DimNone
}

// Display renders a canonical value back in a human-preferred unit.
func Display(q Quantity, preferUnit string) string {
	c, ok := unitTable[preferUnit]
	if !ok || !ok {
		return fmt.Sprintf("%g %s", q.Value, q.Unit)
	}
	_ = c
	return fmt.Sprintf("%g %s", InUnit(q, preferUnit), preferUnit)
}

// InUnit converts a canonical quantity into another unit of the same dimension.
func InUnit(q Quantity, unit string) float64 {
	from, ok1 := unitTable[q.Unit]
	to, ok2 := unitTable[unit]
	if !ok1 || !ok2 || from.dim != to.dim {
		return q.Value
	}
	return to.fromBase(q.Value)
}
