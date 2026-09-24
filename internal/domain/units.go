package domain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Dim identifies a physical dimension used by the TGA domain.
type Dim string

const (
	DimMass Dim = "mass"
	DimTime Dim = "time"
	DimTemp Dim = "temperature"
)

// Measurement is a domain quantity that keeps the original textual
// representation and normalizes to a fixed internal SI-ish basis only on the
// derived layer. Raw input is never rewritten.
type Measurement struct {
	Raw      string  `json:"raw"`
	Unit     string  `json:"unit"`
	Dim      Dim     `json:"dim"`
	Value    float64 `json:"value"`
	Internal string  `json:"internal"`
	Valid    bool    `json:"valid"`
	Diag     string  `json:"diag,omitempty"`
}

var factors = map[Dim]map[string]struct {
	Factor   float64 // multiply value by factor to reach internal unit
	Internal string
}{
	DimMass: {
		"mg": {1.0, "mg"}, "milligram": {1.0, "mg"}, "milligrams": {1.0, "mg"},
		"g": {1000.0, "mg"}, "gram": {1000.0, "mg"}, "grams": {1000.0, "mg"},
		"ug": {0.001, "mg"}, "µg": {0.001, "mg"}, "kg": {1e6, "mg"},
	},
	DimTime: {
		"s": {1.0, "s"}, "sec": {1.0, "s"}, "second": {1.0, "s"}, "seconds": {1.0, "s"},
		"min": {60.0, "s"}, "minute": {60.0, "s"}, "minutes": {60.0, "s"},
		"ms": {0.001, "s"}, "h": {3600.0, "s"}, "hour": {3600.0, "s"},
	},
	DimTemp: {
		"c": {1.0, "°C"}, "°c": {1.0, "°C"}, "celsius": {1.0, "°C"},
		"k": {1.0, "K"}, "kelvin": {1.0, "K"},
	},
}

// ParseMeasurement parses a raw value+unit pair for the given dimension.
// Unconvertible values stay in place with Valid=false and a diagnostic; they
// are never silently dropped.
func ParseMeasurement(raw, unit string, dim Dim) Measurement {
	m := Measurement{Raw: strings.TrimSpace(raw), Unit: strings.TrimSpace(unit), Dim: dim}
	v, err := strconv.ParseFloat(m.Raw, 64)
	if err != nil {
		m.Diag = fmt.Sprintf("unparseable numeric value %q", m.Raw)
		return m
	}
	tbl, ok := factors[dim]
	if !ok {
		m.Diag = fmt.Sprintf("unknown dimension %q", dim)
		return m
	}
	conv, ok := tbl[strings.ToLower(m.Unit)]
	if !ok {
		m.Diag = fmt.Sprintf("unconvertible unit %q for dimension %s", m.Unit, dim)
		return m
	}
	m.Valid = true
	if dim == DimTemp {
		switch conv.Internal {
		case "°C":
			m.Value = v
		case "K":
			m.Value = v - 273.15
		}
		m.Internal = "°C"
	} else {
		m.Value = v * conv.Factor
		m.Internal = conv.Internal
	}
	return m
}

func (m Measurement) String() string {
	if !m.Valid {
		return m.Raw + " " + m.Unit + " (unconvertible)"
	}
	return fmt.Sprintf("%g %s (raw: %s %s)", round4(m.Value), m.Internal, m.Raw, m.Unit)
}

func round4(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return v
	}
	return math.Round(v*1e4) / 1e4
}
