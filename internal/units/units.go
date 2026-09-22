// Package units implements quantities with explicit units and coordinate bases.
// Raw representations are preserved at the import layer; normalisation to SI
// happens only in the derived layer.
package units

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Basis identifies the coordinate reference of a value (see domain basis names).
type Basis struct {
	Kind    string `json:"kind"`               // "time" | "temperature" | "mass" | "rate" | "fraction"
	Name    string `json:"name"`               // e.g. "furnace_time_s", "sample_temp_C"
	ZeroRef string `json:"zero_ref,omitempty"` // anchor description, e.g. "run_start"
}

// Quantity is a domain value that always carries its unit and coordinate basis.
// Raw preserves the original textual representation supplied at import time.
type Quantity struct {
	Value float64 `json:"value"`
	Unit  string  `json:"unit"`
	Basis string  `json:"basis,omitempty"`
	Raw   string  `json:"raw,omitempty"`
	OK    bool    `json:"ok"`
	Diag  string  `json:"diag,omitempty"`
}

func (q Quantity) String() string {
	if !q.OK {
		return fmt.Sprintf("?%s", q.Raw)
	}
	return strconv.FormatFloat(q.Value, 'g', -1, 64) + " " + q.Unit
}

// MarshalJSON keeps unknown quantities explicit instead of dropping them.
func (q Quantity) MarshalJSON() ([]byte, error) {
	type wire struct {
		Value *float64 `json:"value"`
		Unit  string   `json:"unit"`
		Basis string   `json:"basis,omitempty"`
		Raw   string   `json:"raw,omitempty"`
		OK    bool     `json:"ok"`
		Diag  string   `json:"diag,omitempty"`
	}
	w := wire{Unit: q.Unit, Basis: q.Basis, Raw: q.Raw, OK: q.OK, Diag: q.Diag}
	if q.OK {
		v := q.Value
		w.Value = &v
	}
	return json.Marshal(w)
}

// Diagnostics describe values that could not be converted. They stay attached
// to the original cell; the point is never silently discarded.
type Diagnostic struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func bad(raw, unit, basis, msg string) Quantity {
	return Quantity{Unit: unit, Basis: basis, Raw: raw, OK: false, Diag: msg}
}

// unitFactor maps supported display units to (factor, siUnit, basisKind).
// A value in unit u converts to SI as v*factor.
var unitFactor = map[string]struct {
	factor float64
	si     string
	kind   string
}{
	"mg":    {1e-6, "kg", "mass"},
	"g":     {1e-3, "kg", "mass"},
	"kg":    {1, "kg", "mass"},
	"C":     {1, "K_offs", "temperature"},
	"°C":    {1, "K_offs", "temperature"},
	"K":     {1, "K", "temperature"},
	"F":     {5.0 / 9.0, "F_offs", "temperature"},
	"s":     {1, "s", "time"},
	"sec":   {1, "s", "time"},
	"min":   {60, "s", "time"},
	"ms":    {1e-3, "s", "time"},
	"%/min": {1.0 / 60.0, "fraction_per_s", "rate"},
	"%/s":   {1, "fraction_per_s", "rate"},
}

// Normalise converts a quantity to the canonical SI representation.
// Temperature zero offsets are handled separately by NormaliseTemperature.
func Normalise(q Quantity) Quantity {
	if !q.OK {
		return q
	}
	u := canonicalUnit(q.Unit)
	info, ok := unitFactor[u]
	if !ok {
		return bad(q.Raw, q.Unit, q.Basis, "unsupported unit: "+q.Unit)
	}
	out := Quantity{Value: q.Value * info.factor, Unit: info.si, Basis: q.Basis, Raw: q.Raw, OK: true}
	if info.si == "K_offs" {
		out.Unit = "C"
		out.Value = q.Value
	}
	if info.si == "F_offs" {
		out.Unit = "C"
		out.Value = (q.Value - 32) * 5 / 9
	}
	return out
}

// NormaliseTemperature returns kelvin from a temperature quantity.
func NormaliseTemperature(q Quantity) (float64, bool) {
	if !q.OK {
		return 0, false
	}
	switch canonicalUnit(q.Unit) {
	case "C", "°C":
		return q.Value + 273.15, true
	case "K":
		return q.Value, true
	case "F":
		return (q.Value-32)*5/9 + 273.15, true
	}
	return 0, false
}

func canonicalUnit(u string) string {
	u = strings.TrimSpace(u)
	u = strings.ReplaceAll(u, "％", "%")
	return u
}

// ParseMass parses a mass cell, preserving the raw representation.
func ParseMass(raw, unit string) Quantity { return parse(raw, unit, "sample_mass") }

// ParseTime parses a furnace-time cell.
func ParseTime(raw, unit string) Quantity { return parse(raw, unit, "furnace_time") }

// ParseTemperature parses a temperature cell.
func ParseTemperature(raw, unit string) Quantity {
	return parse(raw, unit, "temperature")
}

func parse(raw, unit, basis string) Quantity {
	trim := strings.TrimSpace(raw)
	v, err := strconv.ParseFloat(strings.ReplaceAll(trim, ",", ""), 64)
	if err != nil {
		return bad(raw, unit, basis, "not a number: "+trim)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return bad(raw, unit, basis, "non-finite value")
	}
	return Quantity{Value: v, Unit: canonicalUnit(unit), Basis: basis, Raw: raw, OK: true}
}

// ParseFloat is used for dimensionless/unit-declared columns.
func ParseFloat(raw string) (float64, string, bool) {
	trim := strings.TrimSpace(raw)
	v, err := strconv.ParseFloat(strings.ReplaceAll(trim, ",", ""), 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, trim, false
	}
	return v, trim, true
}

// ValidUnit reports whether u is a supported unit for the given basis kind.
func ValidUnit(u, kind string) bool {
	info, ok := unitFactor[canonicalUnit(u)]
	return ok && (kind == "" || info.kind == kind)
}
