// Package tga implements the thermogravimetric-analysis domain model: layered
// ingestion of raw TGA data, versioned derivation of TG/DTG and stage
// candidates, atmosphere/furnace evidence, and human decisions.
//
// Design rules encoded here:
//
//   - Raw input is immutable; revisions create new versions with parent links.
//   - Every domain quantity carries its unit/coordinate basis. Original
//     representations are preserved at ingestion; normalisation happens only in
//     derived layers. Values that cannot be converted stay in place with a
//     diagnostic and are never silently dropped.
//   - Gas-valve switching times are not sample reaction times (atmosphere delay
//     and temperature lag). Stages may overlap and stage mass-loss percentages
//     are not forced to sum to 100.
//   - Sampling gaps are explicit: derivation never interpolates across a gap.
package tga

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Dimension identifies the physical dimension (coordinate basis) of a
// quantity. It drives unit normalisation in derived layers.
type Dimension string

const (
	DimTime        Dimension = "time"
	DimTemperature Dimension = "temperature"
	DimMass        Dimension = "mass"
	DimFlow        Dimension = "flow"
	DimPercent     Dimension = "percent"
	DimDimensionless Dimension = "dimensionless"
)

// Canonical units used by the derived (normalised) layer.
const (
	UnitMin   = "min"
	UnitC     = "degC"
	UnitMg    = "mg"
	UnitMLMin = "mL/min"
	UnitPct   = "%"
	UnitOne   = "1"
)

// Quantity is an imported value with both its original representation and, when
// possible, its normalised numeric value. If Norm is nil the value could not be
// converted; Diagnostics explains why and Raw stays in place.
type Quantity struct {
	Raw       string  `json:"raw"`
	Norm      *float64 `json:"norm,omitempty"`
	Unit      string  `json:"unit"`
	Dimension Dimension `json:"dimension"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

// Bad records an unconvertible value at its original position.
func Bad(dim Dimension, raw, reason string) Quantity {
	return Quantity{Raw: raw, Unit: "", Dimension: dim, Diagnostic: reason}
}

// Q builds a converted quantity.
func Q(dim Dimension, norm float64, raw, unit string) Quantity {
	return Quantity{Raw: raw, Norm: &norm, Unit: unit, Dimension: dim}
}

func (x Quantity) OK() bool { return x.Norm != nil && x.Diagnostic == "" }

// Format returns the original representation when present, otherwise the
// normalised value with its canonical unit.
func (x Quantity) Format() string {
	if strings.TrimSpace(x.Raw) != "" {
		return x.Raw
	}
	if x.Norm != nil {
		return trimNum(*x.Norm) + " " + x.Unit
	}
	return "?"
}

// NormFloat returns the normalised value and a flag; callers must keep the
// unconvertible case explicit rather than substituting zero.
func (x Quantity) NormFloat() (float64, bool) {
	if x.Norm != nil {
		return *x.Norm, true
	}
	return 0, false
}

func trimNum(v float64) string {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	s = strings.TrimRight(s, "0")
	s = strings.TrimRight(s, ".")
	return s
}

// parseQuantity parses "<number><unit>" style cells. It is deliberately strict
// and returns unconvertible values in place with diagnostics instead of erroring
// the whole import.
func parseQuantity(dim Dimension, cell string) Quantity {
	cell = strings.TrimSpace(cell)
	numPart, unitPart := splitNumberUnit(cell)
	num, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return Bad(dim, cell, "not a number: "+numPart)
	}
	norm, canon, err := normalise(dim, num, strings.TrimSpace(unitPart))
	if err != nil {
		q := Bad(dim, cell, err.Error())
		return q
	}
	return Q(dim, norm, cell, canon)
}

func splitNumberUnit(s string) (string, string) {
	i := 0
	for i < len(s) && (isDigitDot(s[i]) || s[i] == '+' || s[i] == '-' || s[i] == 'e' || s[i] == 'E') {
		i++
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i:])
}

func isDigitDot(b byte) bool {
	return (b >= '0' && b <= '9') || b == '.'
}

// normalise converts (value, sourceUnit) to the canonical unit for dim.
// Temperature is kept on the Celsius scale (a coordinate basis, not a purely
// multiplicative unit), time in minutes, mass in milligrams.
func normalise(dim Dimension, v float64, u string) (float64, string, error) {
	switch dim {
	case DimTime:
		switch lc := strings.ToLower(strings.TrimSuffix(u, ".")); lc {
		case "", "min", "mins", "minute", "minutes":
			return v, UnitMin, nil
		case "s", "sec", "secs", "second", "seconds":
			return v / 60.0, UnitMin, nil
		case "h", "hr", "hrs", "hour", "hours":
			return v * 60.0, UnitMin, nil
		}
	case DimTemperature:
		switch lc := strings.ReplaceAll(strings.ToLower(u), " ", ""); lc {
		case "", "c", "°c", "degc", "celsius":
			return v, UnitC, nil
		case "k", "kelvin":
			return v - 273.15, UnitC, nil
		case "f", "°f", "degf", "fahrenheit":
			return (v - 32.0) * 5.0 / 9.0, UnitC, nil
		}
	case DimMass:
		switch lc := strings.ToLower(u); lc {
		case "", "mg":
			return v, UnitMg, nil
		case "g":
			return v * 1000.0, UnitMg, nil
		case "ug", "µg":
			return v / 1000.0, UnitMg, nil
		}
	case DimFlow:
		switch lc := strings.ReplaceAll(strings.ToLower(u), " ", ""); lc {
		case "", "ml/min", "mlm", "sccm":
			return v, UnitMLMin, nil
		case "l/min":
			return v * 1000.0, UnitMLMin, nil
		}
	case DimPercent:
		switch u {
		case "", "%", "pct":
			return v, UnitPct, nil
		}
	case DimDimensionless:
		return v, UnitOne, nil
	}
	return 0, "", fmt.Errorf("unknown unit %q for dimension %s", u, dim)
}

// finite reports whether v is an ordinary finite number.
func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }
