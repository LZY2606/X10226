// Package unit defines measured quantities with their original representation,
// physical unit and coordinate basis. Raw text is preserved at import time;
// normalization to the canonical units happens only in the derived layer.
package unit

import (
	"fmt"
	"strconv"
	"strings"
)

// Canonical units used by the derived (normalized) layer.
const (
	MassMG        = "mg"
	TimeS         = "s"     // seconds on the furnace clock
	TempC         = "°C"    // degrees Celsius
	RateCperMin   = "°C/min"
	Percent       = "%"
	MGperMin      = "mg/min" // drift slope
	CperMin       = "°C/min"
)

// Value is a single domain quantity. Raw is the original textual
// representation as imported (never discarded). Value holds the normalized
// number in Unit; when normalization is impossible Value=NaN, Unit keeps the
// original unit and a Diagnostic explains why.
type Value struct {
	Raw    string  `json:"raw"`
	Value  float64 `json:"value"`
	Unit   string  `json:"unit"`
	Basis  string  `json:"basis,omitempty"` // coordinate basis, e.g. "furnace_clock", "TGA_sample_temp"
	OK     bool    `json:"ok"`
	DiagID string  `json:"diagId,omitempty"`
}

// Diagnostic is attached to a Value or a source row when it cannot be
// normalized. The value stays in place (raw preserved) instead of being
// dropped for plotting.
type Diagnostic struct {
	ID       string `json:"id"`
	Code     string `json:"code"`
	Message  string `json:"message"`
	Raw      string `json:"raw,omitempty"`
	Expected string `json:"expected,omitempty"`
	Row      int    `json:"row,omitempty"`
	Field    string `json:"field,omitempty"`
}

// NaN returns a non-normalizable value with a diagnostic.
func NaN(raw, unit, basis string, d Diagnostic) Value {
	return Value{Raw: raw, Value: nan(), Unit: unit, Basis: basis, OK: false, DiagID: d.ID}
}

func nan() float64 { return float64FromBits(0x7FF8000000000000) }

// IsNaN reports whether the value failed normalization.
func (v Value) IsNaN() bool { return !v.OK }

func float64FromBits(b uint64) float64 { return math_Float64frombits(b) }

// Conversions from supported import units to canonical units.
var massToMG = map[string]float64{
	"mg": 1, "milligram": 1, "milligrams": 1,
	"g": 1000, "gram": 1000, "grams": 1000,
	"ug": 0.001, "µg": 0.001, "microgram": 0.001,
	"kg": 1_000_000,
}

var timeToS = map[string]float64{
	"s": 1, "sec": 1, "second": 1, "seconds": 1,
	"ms": 0.001, "millisecond": 0.001,
	"min": 60, "minute": 60, "minutes": 60,
	"h": 3600, "hr": 3600, "hour": 3600, "hours": 3600,
}

var tempToC = map[string]float64{
	"c": 1, "°c": 1, "degc": 1, "celsius": 1,
	"k": 0, // special-cased: offset, not scale
	"f": 0, // special-cased: offset
}

var rateToCPerMin = map[string]float64{
	"°c/min": 1, "c/min": 1, "degc/min": 1, "k/min": 1,
	"°c/s": 60, "c/s": 60,
}

// ParseMass parses a mass value, preserving raw.
func ParseMass(raw, basis string, diags *[]Diagnostic, row int) Value {
	return parse(raw, basis, MassMG, diags, row, "mass", convertMass)
}

// ParseTime parses a furnace-clock time value.
func ParseTime(raw, basis string, diags *[]Diagnostic, row int) Value {
	return parse(raw, basis, TimeS, diags, row, "time", convertTime)
}

// ParseTemp parses a temperature value.
func ParseTemp(raw, basis string, diags *[]Diagnostic, row int) Value {
	return parse(raw, basis, TempC, diags, row, "temperature", convertTemp)
}

// ParseRate parses a heating/cooling rate to °C/min.
func ParseRate(raw, basis string, diags *[]Diagnostic, row int) Value {
	return parse(raw, basis, RateCperMin, diags, row, "rate", convertRate)
}

func parse(raw, basis, canonical string, diags *[]Diagnostic, row int, field string, conv func(float64, string) (float64, bool)) Value {
	text := strings.TrimSpace(raw)
	num, unitText, err := splitNumberUnit(text)
	if err != nil {
		d := newDiag(row, field, "UNIT_UNPARSEABLE", err.Error()+" (expected <number><unit>, e.g. 10.5mg)", text)
		*diags = append(*diags, d)
		return NaN(raw, unitText, basis, d)
	}
	if unitText == "" { // numbers without units are kept raw with a diagnostic
		d := newDiag(row, field, "UNIT_MISSING", "quantity without unit; left in place", text)
		*diags = append(*diags, d)
		return NaN(raw, unitText, basis, d)
	}
	out, ok := conv(num, unitText)
	if !ok {
		d := newDiag(row, field, "UNIT_UNSUPPORTED",
			"unsupported unit "+strconv.Quote(unitText)+" for "+field, text)
		*diags = append(*diags, d)
		return NaN(raw, unitText, basis, d)
	}
	return Value{Raw: text, Value: out, Unit: canonical, Basis: basis, OK: true}
}

func newDiag(row int, field, code, msg, raw string) Diagnostic {
	return Diagnostic{
		ID:       fmt.Sprintf("D-%s-r%d-%s", code, row, shortHash(raw+code+strconv.Itoa(row)+field)),
		Code:     code,
		Message:  msg,
		Raw:      raw,
		Row:      row,
		Field:    field,
		Expected: "<number><unit>",
	}
}

func splitNumberUnit(text string) (float64, string, error) {
	if text == "" {
		return 0, "", fmt.Errorf("empty field")
	}
	i := 0
	for i < len(text) && (isDigitByte(text[i]) || text[i] == '.' || text[i] == '-' || text[i] == '+' || text[i] == 'e' || text[i] == 'E') {
		i++
	}
	if i == 0 {
		return 0, strings.TrimSpace(text), fmt.Errorf("no numeric prefix")
	}
	num, err := strconv.ParseFloat(text[:i], 64)
	if err != nil {
		return 0, strings.TrimSpace(text[i:]), err
	}
	return num, strings.ToLower(strings.TrimSpace(text[i:])), nil
}

func isDigitByte(b byte) bool { return b >= '0' && b <= '9' }

func convertMass(n float64, u string) (float64, bool) {
	f, ok := massToMG[u]
	return n * f, ok
}
func convertTime(n float64, u string) (float64, bool) {
	f, ok := timeToS[u]
	return n * f, ok
}
func convertTemp(n float64, u string) (float64, bool) {
	switch u {
	case "k":
		return n - 273.15, true
	case "f":
		return (n - 32) * 5 / 9, true
	default:
		f, ok := tempToC[u]
		if !ok {
			return 0, false
		}
		return n * f, true
	}
}
func convertRate(n float64, u string) (float64, bool) {
	f, ok := rateToCPerMin[u]
	return n * f, ok
}

// Fmt renders a normalized value with unit for the UI (raw is always also
// available on the Value).
func Fmt(v Value, prec int) string {
	if !v.OK {
		return v.Raw + " ⚠"
	}
	return strconv.FormatFloat(round(v.Value, prec), 'f', prec, 64) + v.Unit
}

func round(x float64, prec int) float64 {
	p := 1.0
	for i := 0; i < prec; i++ {
		p *= 10
	}
	return float64(int64(x*p+sign(x)*0.5)) / p
}
func sign(x float64) float64 {
	if x < 0 {
		return -1
	}
	return 1
}

// shortHash is used for stable diagnostic ids (6 hex chars).
func shortHash(s string) string {
	return stableHex(s, 6)
}
