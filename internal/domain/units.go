// Package domain holds the core TGA/stage-decomposition/atmosphere model:
// physical quantities with explicit units, immutable input layers,
// append-only events and content-addressed derived nodes.
package domain

import (
	"fmt"
	"math"
	"strings"
)

// Dimension enumerates the physical dimensions used by TGA materials.
type Dimension string

const (
	DimMass        Dimension = "mass"
	DimTemperature Dimension = "temperature"
	DimTime        Dimension = "time"
	DimRate        Dimension = "rate" // mass/time (DTG)
	DimFraction    Dimension = "fraction"
	DimFlow        Dimension = "flow" // gas flow
)

// Unit describes a single unit belonging to a dimension.
type Unit struct {
	Name      string    `json:"name"`
	Dimension Dimension `json:"dimension"`
	// factor converts value*factor+offset into canonical SI-ish storage units.
	Factor float64 `json:"factor"`
	Offset float64 `json:"offset"`
}

// Canonical storage units:
//
//	mass mg, temperature K, time s, rate mg/min, fraction 1, flow mL/min.
var knownUnits = map[string]Unit{
	// mass
	"mg": {"mg", DimMass, 1, 0},
	"g":  {"g", DimMass, 1000, 0},
	"ug": {"ug", DimMass, 1e-3, 0},
	"µg": {"µg", DimMass, 1e-3, 0},
	"kg": {"kg", DimMass, 1e6, 0},
	// temperature
	"K":    {"K", DimTemperature, 1, 0},
	"C":    {"C", DimTemperature, 1, 273.15},
	"degC": {"degC", DimTemperature, 1, 273.15},
	// time
	"s":   {"s", DimTime, 1, 0},
	"min": {"min", DimTime, 60, 0},
	"h":   {"h", DimTime, 3600, 0},
	// rate
	"mg/min": {"mg/min", DimRate, 1, 0},
	"mg/s":   {"mg/s", DimRate, 60, 0},
	"ug/min": {"ug/min", DimRate, 1e-3, 0},
	// fraction
	"1":       {"1", DimFraction, 1, 0},
	"%":       {"%", DimFraction, 0.01, 0},
	"percent": {"percent", DimFraction, 0.01, 0},
	// flow
	"mL/min":  {"mL/min", DimFlow, 1, 0},
	"ml/min":  {"ml/min", DimFlow, 1, 0},
	"cm3/min": {"cm3/min", DimFlow, 1, 0},
}

// CanonicalUnit returns the canonical unit name of a dimension.
func CanonicalUnit(d Dimension) string {
	switch d {
	case DimMass:
		return "mg"
	case DimTemperature:
		return "K"
	case DimTime:
		return "s"
	case DimRate:
		return "mg/min"
	case DimFraction:
		return "1"
	case DimFlow:
		return "mL/min"
	default:
		return "1"
	}
}

// LookupUnit resolves a unit token (case sensitive for C/K, loose for others).
func LookupUnit(name string) (Unit, bool) {
	if u, ok := knownUnits[name]; ok {
		return u, true
	}
	if u, ok := knownUnits[strings.TrimSpace(name)]; ok {
		return u, true
	}
	return Unit{}, false
}

// Q is a physical quantity. Raw is the value exactly as imported (never
// overwritten); RawUnit is its original textual unit. Values that cannot be
// converted keep ConvOK=false and stay in place with a diagnostic.
type Q struct {
	Raw     float64   `json:"raw"`
	RawUnit string    `json:"raw_unit"`
	Value   float64   `json:"value"` // canonical value; 0 when !ConvOK
	Unit    string    `json:"unit"`  // canonical unit; "" when !ConvOK
	Dim     Dimension `json:"dim"`
	ConvOK  bool      `json:"conv_ok"`
	Note    string    `json:"note,omitempty"`
}

// Canonical presents Q in a stable form for hashing.
func (q Q) Canonical() map[string]any {
	return map[string]any{
		"raw":      q.Raw,
		"raw_unit": q.RawUnit,
		"value":    q.Value,
		"unit":     q.Unit,
		"dim":      string(q.Dim),
		"conv_ok":  q.ConvOK,
		"note":     q.Note,
	}
}

func (q Q) Display() string {
	return fmt.Sprintf("%g %s", q.Raw, q.RawUnit)
}

// Valid reports whether the underlying float is finite.
func (q Q) Finite() bool {
	return math.IsNaN(q.Raw) == false && math.IsInf(q.Raw, 0) == false
}

// NewQ parses a quantity carrying its original representation.
func NewQ(raw float64, rawUnit string, dim Dimension) (Q, error) {
	q := Q{Raw: raw, RawUnit: rawUnit, Dim: dim}
	if math.IsNaN(raw) || math.IsInf(raw, 0) {
		q.Note = "non-finite value retained at source"
		return q, fmt.Errorf("non-finite quantity %v %s", raw, rawUnit)
	}
	if rawUnit == "" {
		q.Note = "missing unit: kept raw, no normalization"
		return q, fmt.Errorf("missing unit for dimension %s", dim)
	}
	u, ok := LookupUnit(rawUnit)
	if !ok {
		q.Note = fmt.Sprintf("unknown unit %q: kept raw, no normalization", rawUnit)
		return q, fmt.Errorf("unknown unit %q", rawUnit)
	}
	if u.Dimension != dim {
		q.Note = fmt.Sprintf("unit %q is not %s: kept raw", rawUnit, dim)
		return q, fmt.Errorf("unit %q dimension %q != expected %q", rawUnit, u.Dimension, dim)
	}
	q.Value = raw*u.Factor + u.Offset
	q.Unit = CanonicalUnit(dim)
	q.ConvOK = true
	return q, nil
}

// MustQ panics on failure; only for test/generated well-formed values.
func MustQ(raw float64, unit string, dim Dimension) Q {
	q, err := NewQ(raw, unit, dim)
	if err != nil {
		panic(err)
	}
	return q
}
