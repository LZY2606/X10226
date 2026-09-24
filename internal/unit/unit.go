// Package unit handles domain quantities with their raw representation intact.
// Normalisation to canonical (SI) units happens only on the derived layer;
// values that cannot be converted stay in place with a diagnostic.
package unit

import (
	"fmt"
	"math"
	"strings"
)

// Kind enumerates the physical kinds used by the TGA domain.
type Kind string

const (
	KindMass       Kind = "mass"
	KindTemperature Kind = "temperature"
	KindTime       Kind = "time"
	KindRate       Kind = "rate" // mass/time, e.g. mg/min
	KindPercent    Kind = "percent"
	KindDimensionless Kind = "dimensionless"
)

// Canonical units per kind.
const (
	Ug   = "ug"   // mass, canonical
	UmG  = "mg"
	UgG  = "g"
	UK   = "K"   // temperature, canonical
	UC   = "degC"
	UF   = "degF"
	Us   = "s"   // time, canonical
	Umin = "min"
	Uh   = "h"
	UugPerMin = "ug/min" // rate, canonical
	UmgPerMin = "mg/min"
	UPercent = "%"
	UOne     = "1"
)

// Quantity is a normalised domain value carrying its raw text and raw unit.
type Quantity struct {
	Raw      string  `json:"raw"`       // original representation, never overwritten
	RawUnit  string  `json:"raw_unit"`  // unit as imported (may be empty)
	Value    float64 `json:"value"`     // canonical SI value
	Unit     string  `json:"unit"`      // canonical unit
	Kind     Kind    `json:"kind"`
	Valid    bool    `json:"valid"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

func canonicalFor(k Kind) (string, bool) {
	switch k {
	case KindMass:
		return Ug, true
	case KindTemperature:
		return UK, true
	case KindTime:
		return Us, true
	case KindRate:
		return UugPerMin, true
	case KindPercent:
		return UPercent, true
	case KindDimensionless:
		return UOne, true
	}
	return "", false
}

// factor converts from u into the canonical unit of kind k.
// Temperature conversion is offset based and handled separately.
func linearFactor(k Kind, u string) (float64, error) {
	switch k {
	case KindMass:
		switch u {
		case Ug:
			return 1, nil
		case UmG:
			return 1000, nil
		case UgG:
			return 1e6, nil
		}
	case KindTime:
		switch u {
		case Us:
			return 1, nil
		case Umin:
			return 60, nil
		case Uh:
			return 3600, nil
		}
	case KindRate:
		switch u {
		case UugPerMin:
			return 1, nil
		case UmgPerMin:
			return 1000, nil
		}
	case KindPercent:
		if u == UPercent || u == "" {
			return 1, nil
		}
	case KindDimensionless:
		if u == UOne || u == "" {
			return 1, nil
		}
	}
	return 0, fmt.Errorf("unit %q is not valid for kind %s", u, k)
}

// Parse converts a raw textual value+unit into a canonical Quantity.
// On failure it returns an invalid Quantity with a diagnostic and keeps raw text.
func Parse(k Kind, raw, rawUnit string) Quantity {
	q := Quantity{Raw: raw, RawUnit: rawUnit, Kind: k}
	can, ok := canonicalFor(k)
	if !ok {
		q.Diagnostic = fmt.Sprintf("unknown quantity kind %q", k)
		return q
	}
	q.Unit = can
	v, err := parseFloat(raw)
	if err != nil {
		q.Diagnostic = err.Error()
		return q
	}
	if k == KindTemperature {
		switch normalizeUnit(rawUnit) {
		case UK, "":
			q.Value = v
		case UC:
			q.Value = v + 273.15
		case UF:
			q.Value = (v-32)*5.0/9.0 + 273.15
		default:
			q.Diagnostic = fmt.Sprintf("temperature unit %q not convertible", rawUnit)
			return q
		}
		q.Valid = true
		return q
	}
	f, err := linearFactor(k, normalizeUnit(rawUnit))
	if err != nil {
		q.Diagnostic = err.Error()
		return q
	}
	q.Value = v * f
	q.Valid = true
	return q
}

func normalizeUnit(u string) string {
	return strings.TrimSpace(u)
}

func parseFloat(s string) (float64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty numeric value")
	}
	var v float64
	if _, err := fmt.Sscanf(s, "%g", &v); err != nil {
		return 0, fmt.Errorf("not a number: %q", s)
	}
	// Sscanf is permissive; verify the whole token parses.
	if math.IsNaN(v) {
		return 0, fmt.Errorf("NaN not accepted: %q", s)
	}
	return v, nil
}

// In returns the value expressed in the requested display unit.
func (q Quantity) In(unit string) (float64, error) {
	switch q.Kind {
	case KindTemperature:
		switch normalizeUnit(unit) {
		case UK:
			return q.Value, nil
		case UC:
			return q.Value - 273.15, nil
		case UF:
			return (q.Value-273.15)*9.0/5.0 + 32, nil
		}
	}
	f, err := linearFactor(q.Kind, normalizeUnit(unit))
	if err != nil {
		return 0, err
	}
	return q.Value / f, nil
}
