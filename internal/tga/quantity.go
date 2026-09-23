// Package tga contains the domain model, import pipeline, event-sourced
// history and derivation engine for the TGA mass-loss lineage system.
package tga

import (
	"fmt"
	"strconv"
	"strings"
)

// Dimension enumerates the physical dimensions handled by the import layer.
type Dimension string

const (
	DimTime          Dimension = "time"        // seconds
	DimTemperature   Dimension = "temperature" // kelvin
	DimMass          Dimension = "mass"        // milligrams
	DimRate          Dimension = "rate"        // mg/min or fraction/min
	DimDimensionless Dimension = "dimensionless"
)

// Unit conversion table: canonical = factor*value + offset.
// time base seconds, temperature base kelvin, mass base milligrams.
var unitDefs = map[string]struct {
	dim    Dimension
	factor float64
	offset float64
}{
	"s":        {DimTime, 1, 0},
	"min":      {DimTime, 60, 0},
	"sec":      {DimTime, 1, 0},
	"k":        {DimTemperature, 1, 0},
	"c":        {DimTemperature, 1, 273.15},
	"°c":       {DimTemperature, 1, 273.15},
	"degc":     {DimTemperature, 1, 273.15},
	"mg":       {DimMass, 1, 0},
	"g":        {DimMass, 1000, 0},
	"ug":       {DimMass, 0.001, 0},
	"%":        {DimDimensionless, 0.01, 0},
	"fraction": {DimDimensionless, 1, 0},
	"mg/min":   {DimRate, 1, 0},
	"mg/s":     {DimRate, 60, 0},
	"%/min":    {DimRate, 0.01, 0},
}

// Quantity is a value that still carries its original representation.
// Raw is the exact token as imported. Canonical is the normalized value in the
// dimension base unit; when conversion is impossible Converted is false and
// Diagnostic explains why: the value stays at its original position and is
// never silently dropped for plotting.
type Quantity struct {
	Raw        string    `json:"raw"`
	Unit       string    `json:"unit"`
	Dim        Dimension `json:"dim"`
	Canonical  float64   `json:"canonical,omitempty"`
	Converted  bool      `json:"converted"`
	Diagnostic string    `json:"diagnostic,omitempty"`
}

// ParseQuantity converts an imported token plus its declared unit.
func ParseQuantity(raw, unit string, want Dimension) Quantity {
	q := Quantity{Raw: strings.TrimSpace(raw), Unit: strings.TrimSpace(unit), Dim: want}
	def, known := unitDefs[strings.ToLower(q.Unit)]
	if q.Unit == "" {
		def, known = unitDefs["fraction"]
}
	if !known {
		q.Diagnostic = fmt.Sprintf("unknown unit %q for dimension %s; value retained at original position", q.Unit, want)
		return q
	}
	if def.dim != want {
		q.Diagnostic = fmt.Sprintf("unit %q is not compatible with %s; value retained unconverted", q.Unit, want)
		return q
	}
	v, err := strconv.ParseFloat(q.Raw, 64)
	if err != nil {
		q.Diagnostic = fmt.Sprintf("non-numeric value %q retained at original position", q.Raw)
		return q
	}
	q.Canonical = v*def.factor + def.offset
	q.Converted = true
	return q
}

// Display renders a quantity in its base unit, or the raw token when it could
// not be converted.
func (q Quantity) Display() string {
	if !q.Converted {
		return q.Raw + " " + q.Unit
	}
	return strconv.FormatFloat(q.Canonical, 'g', -1, 64) + " " + baseUnit(q.Dim)
}

func baseUnit(d Dimension) string {
	switch d {
	case DimTime:
		return "s"
	case DimTemperature:
		return "K"
	case DimMass:
		return "mg"
	case DimRate:
		return "base/min"
	default:
		return ""
	}
}
