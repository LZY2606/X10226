// Package quantity implements domain values that preserve their original text
// representation and unit. Normalisation to a canonical unit happens only in
// the derivation layer; values that cannot be converted stay in place and
// carry a diagnostic instead of being dropped for plotting.
package quantity

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Dimension identifies the kind of a measured quantity. DimensionUnknown
// keeps the value in place with a diagnostic rather than guessing.
type Dimension string

const (
	DimUnknown   Dimension = ""
	DimMass      Dimension = "mass"
	DimTime      Dimension = "time"
	DimTemp      Dimension = "temperature"
	DimPercent   Dimension = "percent"
	DimFlow      Dimension = "flow"
	DimRate      Dimension = "rate"       // percent/min
	DimMassRate  Dimension = "mass_rate"  // mg/min
	DimHeatRate  Dimension = "heat_rate"  // K/min
	DimDimensionless Dimension = "dimensionless"
)

// Quantity is an imported value exactly as written plus, when possible, a
// canonical numeric interpretation. Raw must never be overwritten.
type Quantity struct {
	Raw        string    `json:"raw"`
	Unit       string    `json:"unit"`
	Value      *float64  `json:"value,omitempty"`
	Canonical  string    `json:"canonical_unit,omitempty"`
	Dimension  Dimension `json:"dimension,omitempty"`
	Diagnostic string    `json:"diagnostic,omitempty"`
}

// SI / canonical units used by the derivation layer:
// mass mg, time s, temperature K, percent %, flow mL/min,
// rates %/min, mg/min and K/min respectively.
var canonical = map[string]struct {
	dim Dimension
	to  float64 // multiply value by to to reach canonical
}{
	"mg":     {DimMass, 1},
	"g":      {DimMass, 1000},
	"kg":     {DimMass, 1e6},
	"ug":     {DimMass, 1e-3},
	"\u00b5g": {DimMass, 1e-3},
	"s":      {DimTime, 1},
	"sec":    {DimTime, 1},
	"min":    {DimTime, 60},
	"minute": {DimTime, 60},
	"k":      {DimTemp, 1},
	"c":      {DimTemp, 1},       // special: offset handled in parse
	"\u00b0c": {DimTemp, 1},
	"degc":   {DimTemp, 1},
	"f":      {DimTemp, 1},       // special: offset handled in parse
	"\u00b0f": {DimTemp, 1},
	"degf":   {DimTemp, 1},
	"%":      {DimPercent, 1},
	"pct":    {DimPercent, 1},
	"percent": {DimPercent, 1},
	"wt%":    {DimPercent, 1},
	"wt％":   {DimPercent, 1},
	"ml/min": {DimFlow, 1},
	"mlm":    {DimFlow, 1},
	"cc/min": {DimFlow, 1},
	"sccm":   {DimFlow, 1},
	"l/min":  {DimFlow, 1000},
	"%/min":  {DimRate, 1},
	"pct/min": {DimRate, 1},
	"wt%/min": {DimRate, 1},
	"mg/min": {DimMassRate, 1},
	"g/min":  {DimMassRate, 1000},
	"k/min":  {DimHeatRate, 1},
	"c/min":  {DimHeatRate, 1},
	"\u00b0c/min": {DimHeatRate, 1},
	"-":      {DimDimensionless, 1},
	"":       {DimDimensionless, 1},
}

// Parse converts raw text and a declared unit. An empty raw value yields a
// Quantity carrying only a diagnostic.
func Parse(raw, unit string) Quantity {
	q := Quantity{Raw: strings.TrimSpace(raw), Unit: strings.TrimSpace(unit)}
	text := strings.TrimSpace(raw)
	if text == "" {
		q.Diagnostic = "empty value"
		return q
	}
	u := strings.ToLower(strings.TrimSpace(unit))
	entry, known := canonical[u]
	if !known {
		q.Diagnostic = fmt.Sprintf("unknown unit %q; value retained with original representation", unit)
		return q
	}
	num, err := strconv.ParseFloat(strings.TrimSuffix(strings.TrimPrefix(text, "+"), ""), 64)
	if err != nil {
		num, err = strconv.ParseFloat(text, 64)
	}
	if err != nil {
		q.Diagnostic = fmt.Sprintf("not a number: %q", text)
		return q
	}
	if math.IsNaN(num) || math.IsInf(num, 0) {
		q.Diagnostic = "non-finite value"
		return q
	}
	q.Dimension = entry.dim
	switch entry.dim {
	case DimTemp:
		switch u {
		case "c", "\u00b0c", "degc":
			num += 273.15
		case "f", "\u00b0f", "degf":
			num = (num-32)*5/9 + 273.15
		}
		q.Canonical = "K"
	case DimMass:
		num *= entry.to
		q.Canonical = "mg"
	case DimTime:
		num *= entry.to
		q.Canonical = "s"
	case DimPercent:
		num *= entry.to
		q.Canonical = "%"
	case DimFlow:
		num *= entry.to
		q.Canonical = "mL/min"
	case DimRate:
		num *= entry.to
		q.Canonical = "%/min"
	case DimMassRate:
		num *= entry.to
		q.Canonical = "mg/min"
	case DimHeatRate:
		num *= entry.to
		q.Canonical = "K/min"
	case DimDimensionless:
		q.Canonical = "-"
	}
	q.Value = &num
	return q
}

// CanonicalUnit returns the canonical unit label for a dimension.
func CanonicalUnit(d Dimension) string {
	switch d {
	case DimMass:
		return "mg"
	case DimTime:
		return "s"
	case DimTemp:
		return "K"
	case DimPercent:
		return "%"
	case DimFlow:
		return "mL/min"
	case DimRate:
		return "%/min"
	case DimMassRate:
		return "mg/min"
	case DimHeatRate:
		return "K/min"
	default:
		return "-"
	}
}

// OK reports whether the quantity has a canonical numeric value.
func (q Quantity) OK() bool { return q.Value != nil && q.Diagnostic == "" }

// MustValue returns the canonical value or panics; callers must check OK first.
func (q Quantity) MustValue() float64 {
	if q.Value == nil {
		panic("quantity has no canonical value: " + q.Diagnostic)
	}
	return *q.Value
}
