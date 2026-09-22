package domain

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Quantity is a physical value that retains the representation used at import.
// Normalization into the canonical unit happens only on a derived layer.
type Quantity struct {
	Value    float64 `json:"value"`
	Unit     string  `json:"unit"`               // unit as written in the source
	Original string  `json:"original,omitempty"` // verbatim source token
	Kind     DimKind `json:"kind"`
}

type DimKind string

const (
	DimMass      DimKind = "mass"
	DimTemp      DimKind = "temperature"
	DimTime      DimKind = "time"
	DimFlow      DimKind = "flow"
	DimRate      DimKind = "rate" // dmass/dtime, e.g. mg/min
	DimFraction  DimKind = "fraction"
	DimDimension DimKind = "dimensionless"
)

// canonical units
const (
	UnitMg      = "mg"
	UnitG       = "g"
	UnitUg      = "ug"
	UnitK       = "K"
	UnitC       = "C"
	UnitMin     = "min"
	UnitS       = "s"
	UnitH       = "h"
	UnitSccm    = "sccm"
	UnitMlMin   = "mL/min"
	UnitMgMin   = "mg/min"
	UnitPercent = "%"
	UnitNone    = "1"
)

// CanonicalUnit returns the canonical unit used by derived layers.
func CanonicalUnit(k DimKind) string {
	switch k {
	case DimMass:
		return UnitMg
	case DimTemp:
		return UnitK
	case DimTime:
		return UnitMin
	case DimFlow:
		return UnitSccm
	case DimRate:
		return UnitMgMin
	case DimFraction:
		return UnitPercent
	default:
		return UnitNone
	}
}

// Normalize converts a quantity to its canonical unit, retaining the original.
// A value that cannot be converted is returned in place with a non-nil error.
func (q Quantity) Normalize() (Quantity, error) {
	if math.IsNaN(q.Value) || math.IsInf(q.Value, 0) {
		return q, errors.New("non-finite value cannot be normalized: " + q.Original)
	}
	out := Quantity{Value: q.Value, Unit: q.Unit, Original: q.Original, Kind: q.Kind}
	switch q.Kind {
	case DimMass:
		switch strings.ToLower(q.Unit) {
		case UnitMg, "milligram", "milligrams":
			out.Unit = UnitMg
		case UnitG, "gram", "grams":
			out.Value = q.Value * 1000
			out.Unit = UnitMg
		case UnitUg, "microgram", "micrograms":
			out.Value = q.Value / 1000
			out.Unit = UnitMg
		default:
			return q, fmt.Errorf("unknown mass unit %q", q.Unit)
		}
	case DimTemp:
		switch q.Unit {
		case UnitK, "kelvin":
			out.Unit = UnitK
		case UnitC, "°C", "degC", "celsius":
			out.Value = q.Value + 273.15
			out.Unit = UnitK
		default:
			return q, fmt.Errorf("unknown temperature unit %q", q.Unit)
		}
	case DimTime:
		switch q.Unit {
		case UnitMin, "minute", "minutes":
			out.Unit = UnitMin
		case UnitS, "sec", "second", "seconds":
			out.Value = q.Value / 60
			out.Unit = UnitMin
		case UnitH, "hour", "hours":
			out.Value = q.Value * 60
			out.Unit = UnitMin
		default:
			return q, fmt.Errorf("unknown time unit %q", q.Unit)
		}
	case DimFlow:
		switch q.Unit {
		case UnitSccm, UnitMlMin, "cm3/min":
			out.Unit = UnitSccm
		default:
			return q, fmt.Errorf("unknown flow unit %q", q.Unit)
		}
	case DimFraction:
		switch q.Unit {
		case UnitPercent, "pct", "percent":
			out.Unit = UnitPercent
		case UnitNone, "fraction":
			out.Value = q.Value * 100
			out.Unit = UnitPercent
		default:
			return q, fmt.Errorf("unknown fraction unit %q", q.Unit)
		}
	case DimRate:
		switch q.Unit {
		case UnitMgMin:
		case "mg/s":
			out.Value = q.Value * 60
			out.Unit = UnitMgMin
		default:
			return q, fmt.Errorf("unknown rate unit %q", q.Unit)
		}
	case DimDimension:
		out.Unit = UnitNone
	default:
		return q, fmt.Errorf("unknown dimension %q", q.Kind)
	}
	return out, nil
}

// InCelsius renders a canonical temperature (K) in C for display.
func (q Quantity) InCelsius() (float64, error) {
	if q.Kind != DimTemp {
		return 0, fmt.Errorf("not a temperature: %s", q.Kind)
	}
	if q.Unit == UnitK {
		return q.Value - 273.15, nil
	}
	n, err := q.Normalize()
	if err != nil {
		return 0, err
	}
	return n.Value - 273.15, nil
}

// ParseQuantity parses a token like "12.34 mg"; unit may be empty only for dimensionless.
func ParseQuantity(kind DimKind, token, defaultUnit string) (Quantity, error) {
	token = strings.TrimSpace(token)
	if token == "" {
		return Quantity{}, errors.New("empty quantity")
	}
	i := strings.IndexAny(token, " \t")
	var numPart, unitPart string
	if i < 0 {
		numPart = token
		unitPart = defaultUnit
	} else {
		numPart = strings.TrimSpace(token[:i])
		unitPart = strings.TrimSpace(token[i+1:])
	}
	v, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return Quantity{}, fmt.Errorf("unparseable numeric part %q: %w", numPart, err)
	}
	if unitPart == "" && kind != DimDimension {
		return Quantity{}, fmt.Errorf("missing unit for %s", kind)
	}
	return Quantity{Value: v, Unit: unitPart, Original: token, Kind: kind}, nil
}
