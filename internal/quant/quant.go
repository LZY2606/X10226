// Package quant handles domain quantities. Original textual representation is
// always preserved; normalization happens only on the derived layer. Values
// that cannot be converted stay in place with a diagnostic instead of being
// dropped for plotting.
package quant

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Status marks whether a quantity could be normalized.
type Status string

const (
	StatusOK      Status = "ok"
	StatusUnknown Status = "unknown" // empty / "NA": plotted as a gap, never interpolated
	StatusInvalid Status = "invalid" // unconvertible: original text retained with diagnostic
)

// Q is a quantity carrying its original representation, normalized value, unit
// and the coordinate basis it is expressed in.
type Q struct {
	Raw      string  `json:"raw"`
	Value    float64 `json:"value,omitempty"`
	Unit     string  `json:"unit"`
	Basis    string  `json:"basis,omitempty"` // coordinate basis, e.g. furnace_time_s
	Status   Status  `json:"status"`
	Diagnostic string `json:"diagnostic,omitempty"`
}

type unitSpec struct {
	canonical string
	kind      string
	factor    float64 // multiply value to reach canonical
}

var units = map[string]unitSpec{
	// mass, canonical mg
	"mg": {"mg", "mass", 1},
	"g":  {"mg", "mass", 1000},
	"kg": {"mg", "mass", 1_000_000},
	"ug": {"mg", "mass", 1e-3},
	"µg": {"mg", "mass", 1e-3},
	// temperature, canonical °C
	"c":    {"°C", "temp", 1},
	"°c":   {"°C", "temp", 1},
	"k":    {"°C", "temp", 1}, // handled specially for offset
	// time, canonical s
	"s":    {"s", "time", 1},
	"sec":  {"s", "time", 1},
	"min":  {"s", "time", 60},
	"m":    {"s", "time", 60},
	"h":    {"s", "time", 3600},
	"ms":   {"s", "time", 1e-3},
	// rate
	"k/min": {"°C/min", "rate", 1},
	"c/min": {"°C/min", "rate", 1},
}

var numRe = regexp.MustCompile(`^\s*([+-]?[0-9]*\.?[0-9]+(?:[eE][+-]?[0-9]+)?)\s*([^\s]*)\s*$`)

// Parse converts a raw cell for the declared expected kind (mass|temp|time|rate).
// An empty / NA cell becomes unknown; an unconvertible cell becomes invalid and
// keeps its text.
func Parse(raw, expectedKind string) Q {
	text := strings.TrimSpace(raw)
	q := Q{Raw: raw, Unit: "", Basis: basisFor(expectedKind)}
	low := strings.ToLower(text)
	if text == "" || low == "na" || low == "n/a" || low == "nan" || low == "--" {
		q.Status = StatusUnknown
		return q
	}
	m := numRe.FindStringSubmatch(text)
	if m == nil {
		q.Status = StatusInvalid
		q.Diagnostic = fmt.Sprintf("cannot parse %q as %s", text, expectedKind)
		return q
	}
	num, err := strconv.ParseFloat(m[1], 64)
	if err != nil || math.IsNaN(num) {
		q.Status = StatusInvalid
		q.Diagnostic = fmt.Sprintf("cannot parse number %q", m[1])
		return q
	}
	unitText := strings.ToLower(m[2])
	if unitText == "" {
		// unitless input: accept as canonical for the expected kind
		spec, ok := units[canonicalUnitless(expectedKind)]
		if !ok {
			q.Status = StatusInvalid
			q.Diagnostic = "no unit registry for " + expectedKind
			return q
		}
		q.Unit, q.Value, q.Status = spec.canonical, num, StatusOK
		return q
	}
	spec, ok := units[unitText]
	if !ok {
		q.Status = StatusInvalid
		q.Diagnostic = fmt.Sprintf("unknown unit %q", unitText)
		return q
	}
	if spec.kind != expectedKind {
		q.Status = StatusInvalid
		q.Diagnostic = fmt.Sprintf("unit %s is %s, expected %s", unitText, spec.kind, expectedKind)
		return q
	}
	q.Unit = spec.canonical
	if spec.canonical == "°C" && unitText == "k" {
		q.Value = num - 273.15
	} else {
		q.Value = num * spec.factor
	}
	q.Status = StatusOK
	return q
}

func canonicalUnitless(kind string) string {
	switch kind {
	case "mass":
		return "mg"
	case "temp":
		return "c"
	case "time":
		return "s"
	case "rate":
		return "k/min"
	}
	return ""
}

func basisFor(kind string) string {
	switch kind {
	case "mass":
		return "sample_balance_mg"
	case "temp":
		return "furnace_program_degC"
	case "time":
		return "furnace_time_s"
	}
	return ""
}

// Format renders a normalized value with its canonical unit (display only).
func (q Q) Format() string {
	if q.Status != StatusOK {
		return "?"
	}
	return fmt.Sprintf("%g %s", q.Value, q.Unit)
}

// Finite reports whether q is a usable number.
func (q Q) Finite() bool { return q.Status == StatusOK && !math.IsNaN(q.Value) }

// Percent renders a normalized fraction (0..1) as a percentage string.
func Percent(fraction float64) float64 { return fraction * 100 }
