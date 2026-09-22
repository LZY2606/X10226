// Package domain defines the core quantities of the TGA review system.
// Every domain quantity keeps its raw representation; normalization happens
// only in the derived layer. Values that cannot be converted stay in place
// and carry a diagnostic instead of being dropped.
package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Quantity is a domain value with a unit or coordinate basis.
// Raw preserves the original textual representation exactly as imported.
type Quantity struct {
	Raw  string  `json:"raw"`           // original representation, never rewritten
	Num  float64 `json:"num"`           // numeric part as written
	Unit string  `json:"unit"`          // unit as written (may be non-canonical)
	Canon float64 `json:"canon"`         // value converted to the canonical unit of its dimension
	Dim  string  `json:"dim"`           // "time" | "temp" | "mass" | "flow" | "rate" | ""
	OK   bool    `json:"ok"`            // false when the value could not be converted
	Diag string  `json:"diag,omitempty"` // diagnostic when OK is false
}

// canonical units per dimension
var dimCanon = map[string]string{
	"time": "s",
	"temp": "C",
	"mass": "mg",
	"flow": "mL/min",
	"rate": "mg/min",
}

// unit -> (dimension, multiplier to canonical)
var unitTable = map[string]struct {
	Dim  string
	Mult float64
}{
	"s":      {"time", 1},
	"sec":    {"time", 1},
	"min":    {"time", 60},
	"h":      {"time", 3600},
	"C":      {"temp", 1},
	"°C":     {"temp", 1},
	"K":      {"temp", 1}, // offset handled specially
	"mg":     {"mass", 1},
	"g":      {"mass", 1000},
	"ug":     {"mass", 0.001},
	"mL/min": {"flow", 1},
	"sccm":   {"flow", 1},
	"mg/min": {"rate", 1},
	"mg/s":   {"rate", 60},
	"ug/min": {"rate", 0.001},
}

// ParseQuantity parses strings like "12.5 mg", "300 °C", "45 s".
// Unconvertible values are returned with OK=false and a diagnostic;
// the raw representation is always preserved.
func ParseQuantity(raw string) Quantity {
	q := Quantity{Raw: raw}
	s := strings.TrimSpace(raw)
	if s == "" {
		q.Diag = "empty quantity"
		return q
	}
	fields := strings.Fields(s)
	numPart := fields[0]
	unitPart := strings.TrimSpace(strings.TrimPrefix(s, numPart))
	n, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		q.Diag = fmt.Sprintf("cannot parse number in %q", raw)
		return q
	}
	q.Num = n
	q.Unit = unitPart
	if unitPart == "" {
		q.Diag = fmt.Sprintf("missing unit in %q", raw)
		return q
	}
	entry, ok := unitTable[unitPart]
	if !ok {
		q.Diag = fmt.Sprintf("unknown unit %q in %q; value left in place", unitPart, raw)
		return q
	}
	q.Dim = entry.Dim
	canon := n * entry.Mult
	if unitPart == "K" { // Kelvin -> Celsius offset
		canon = n - 273.15
	}
	q.Canon = canon
	q.OK = true
	return q
}

// CanonUnit returns the canonical unit for a dimension.
func CanonUnit(dim string) string { return dimCanon[dim] }
