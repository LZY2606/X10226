// Package units parses and normalizes physical quantities.
// Raw representations are always preserved; normalization happens here,
// and values that cannot be converted stay in place with a diagnostic.
package units

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Dimension is a physical dimension class.
type Dimension string

const (
	DimMass   Dimension = "mass"
	DimTemp   Dimension = "temperature"
	DimTime   Dimension = "time"
	DimFlow   Dimension = "flow"
	DimPct    Dimension = "percent"
	DimNumber Dimension = "number"
)

// Quantity is a domain quantity carrying its original raw representation,
// a normalized value in the canonical unit for its dimension, and a
// diagnostic when conversion was impossible.
type Quantity struct {
	Raw   string    `json:"raw"`             // original text as imported, never rewritten
	Value float64   `json:"value"`           // normalized value (0 when !OK)
	Unit  string    `json:"unit"`            // canonical unit: mg, c, s, ml_min, pct, 1
	Dim   Dimension `json:"dim"`             // physical dimension
	OK    bool      `json:"ok"`              // conversion succeeded
	Diag  string    `json:"diag,omitempty"`  // why conversion failed
}

// Canonical units per dimension.
var canonicalUnit = map[Dimension]string{
	DimMass:   "mg",
	DimTemp:   "c",
	DimTime:   "s",
	DimFlow:   "ml_min",
	DimPct:    "pct",
	DimNumber: "1",
}

// unitTable maps accepted unit spellings to (dimension, factor to canonical).
var unitTable = map[string]struct {
	dim    Dimension
	factor float64
}{
	"mg": {DimMass, 1}, "g": {DimMass, 1000}, "ug": {DimMass, 0.001}, "kg": {DimMass, 1e6},
	"c": {DimTemp, 1}, "°c": {DimTemp, 1}, "degc": {DimTemp, 1}, "celsius": {DimTemp, 1},
	"k": {DimTemp, 1}, // offset handled by caller convention; we treat K as c+(-273.15) below
	"s": {DimTime, 1}, "sec": {DimTime, 1}, "min": {DimTime, 60}, "h": {DimTime, 3600},
	"ml/min": {DimFlow, 1}, "ml_min": {DimFlow, 1}, "sccm": {DimFlow, 1},
	"%": {DimPct, 1}, "pct": {DimPct, 1}, "percent": {DimPct, 1},
	"": {DimNumber, 1}, "1": {DimNumber, 1},
}

var qtyRe = regexp.MustCompile(`^\s*([+-]?[0-9]*\.?[0-9]+(?:[eE][+-]?[0-9]+)?)\s*([a-zA-Z%°/_]*)\s*$`)

// Parse converts raw text like "10.000 mg" into a normalized Quantity for
// the expected dimension. The raw text is preserved verbatim. Values that
// cannot be converted are returned with OK=false and a diagnostic; callers
// must keep them in place rather than dropping them.
func Parse(raw string, want Dimension) Quantity {
	q := Quantity{Raw: raw, Dim: want, Unit: canonicalUnit[want]}
	m := qtyRe.FindStringSubmatch(strings.TrimSpace(raw))
	if m == nil {
		q.Diag = fmt.Sprintf("unparseable quantity %q", raw)
		return q
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		q.Diag = fmt.Sprintf("bad number in %q: %v", raw, err)
		return q
	}
	unit := strings.ToLower(m[2])
	entry, ok := unitTable[unit]
	if !ok {
		q.Diag = fmt.Sprintf("unknown unit %q in %q", m[2], raw)
		return q
	}
	if entry.dim != want && !(entry.dim == DimNumber && want == DimNumber) {
		// A bare number is accepted for any dimension (unit inherited from
		// the column header); a mismatched explicit unit is not.
		if unit != "" {
			q.Diag = fmt.Sprintf("unit %q is %s, expected %s", m[2], entry.dim, want)
			return q
		}
		q.Value = v
		q.OK = true
		return q
	}
	if unit == "k" && want == DimTemp {
		v -= 273.15
	}
	q.Value = v * entry.factor
	q.OK = true
	return q
}

// CanonicalUnit returns the canonical unit for a dimension.
func CanonicalUnit(d Dimension) string { return canonicalUnit[d] }
