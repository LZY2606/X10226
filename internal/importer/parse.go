// Package importer reads the small, copyable TGA bundle format. It keeps the
// original representation of every value, converts only into the derived
// layer, retains values it cannot convert (with diagnostics, in place) and
// implements per-layer half failure: a bad layer is rejected while the rest
// of the submission still lands.
package importer

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

// LayerData is one accepted, parsed layer with stable row ids.
type LayerData struct {
	Drift   []domain.DriftCorrection
	Program []domain.ProgramSegment
	Gas     []domain.GasEvent
	Blank   []domain.BlankPoint
	Initial []domain.InitialMass
	Samples []domain.TimePoint
}

// DiagnosticRow retains a row that could not be structurally placed.
type DiagnosticRow struct {
	Layer   string `json:"layer"`
	Ordinal int    `json:"ordinal"`
	Line    string `json:"line"`
	Reason  string `json:"reason"`
}

// Result is the outcome of one submission.
type Result struct {
	Project        string
	SourceRef      string
	AcceptedLayers []string
	Rejected       []domain.RejectedLayer
	Diagnostics    []DiagnosticRow
	Data           LayerData
}

// Parse dispatches on content: JSON object -> json path, otherwise text.
func Parse(project, source string, payload []byte) (*Result, error) {
	trimmed := strings.TrimSpace(string(payload))
	if strings.HasPrefix(trimmed, "{") {
		return parseJSON(project, source, payload)
	}
	return parseText(project, source, payload)
}

var timeFactors = map[string]float64{
	domain.UnitSecond: 1,
	domain.UnitMinute: 60,
}

var massFactors = map[string]float64{
	domain.UnitMilligram: 1,
	domain.UnitGram:      1000,
	domain.UnitMicrogram: 0.001,
}

var tempOffsets = map[string]bool{domain.UnitCelsius: true, domain.UnitKelvin: true}

// quantity parses "<number><unit-aware>" using the header-declared unit. The
// raw token is retained verbatim. Unconvertible values stay in place.
func quantity(token, unit string) domain.Quantity {
	token = strings.TrimSpace(token)
	q := domain.Quantity{Raw: token, Unit: unit}
	v, err := strconv.ParseFloat(strings.TrimSpace(token), 64)
	if err != nil {
		q.Diagnostic = "not a number: " + err.Error()
		q.Value = math.NaN()
		q.ConvertedValue = math.NaN()
		return q
	}
	q.Value = v
	switch {
	case timeFactors[unit] != 0:
		q.Canonical = domain.UnitSecond
		q.ConvertedValue = v * timeFactors[unit]
		q.Convertible = true
	case massFactors[unit] != 0:
		q.Canonical = domain.UnitMilligram
		q.ConvertedValue = v * massFactors[unit]
		q.Convertible = true
	case tempOffsets[unit]:
		q.Canonical = domain.UnitCelsius
		if unit == domain.UnitKelvin {
			q.ConvertedValue = v - 273.15
		} else {
			q.ConvertedValue = v
		}
		q.Convertible = true
	case unit == domain.UnitSCCM || strings.HasPrefix(unit, "degC"):
		q.Canonical = unit
		q.ConvertedValue = v
		q.Convertible = true
	default:
		q.Diagnostic = "unknown unit: " + unit
		q.ConvertedValue = math.NaN()
	}
	return q
}

// headerCol describes one declared column: name and bracketed unit.
type headerCol struct{ name, unit string }

func parseHeader(fields []string) ([]headerCol, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("empty header")
	}
	cols := make([]headerCol, len(fields))
	for i, f := range fields {
		name, unit := f, ""
		if open := strings.Index(f, "["); open >= 0 && strings.HasSuffix(f, "]") {
			name = f[:open]
			unit = f[open+1 : len(f)-1]
		}
		if strings.ContainsAny(name, "°") {
			name = strings.ReplaceAll(name, "°", "deg")
		}
		cols[i] = headerCol{name: strings.ToLower(name), unit: normalizeUnit(unit)}
	}
	return cols, nil
}

func normalizeUnit(u string) string {
	u = strings.TrimSpace(u)
	switch u {
	case "s", "sec", "seconds":
		return domain.UnitSecond
	case "min", "minute", "minutes":
		return domain.UnitMinute
	case "mg", "milligram", "milligrams":
		return domain.UnitMilligram
	case "g", "gram", "grams":
		return domain.UnitGram
	case "ug", "µg":
		return domain.UnitMicrogram
	case "C", "°C", "degC", "celsius":
		return domain.UnitCelsius
	case "K", "kelvin":
		return domain.UnitKelvin
	case "sccm":
		return domain.UnitSCCM
	case "%", "pct":
		return domain.UnitPercent
	case "mg/min":
		return domain.UnitmgPerMin
	case "degC/min":
		return domain.UnitCelsius + "/min"
	}
	return u
}

func colMap(cols []headerCol) map[string]int {
	m := make(map[string]int, len(cols))
	for i, c := range cols {
		m[c.name] = i
	}
	return m
}

func get(fields []string, m map[string]int, name, unit string) domain.Quantity {
	idx, ok := m[name]
	if !ok || idx >= len(fields) {
		return domain.Quantity{Raw: "", Unit: unit, Diagnostic: "missing column " + name}
	}
	return quantity(fields[idx], unit)
}

func getStr(fields []string, m map[string]int, name string) string {
	idx, ok := m[name]
	if !ok || idx >= len(fields) {
		return ""
	}
	return strings.TrimSpace(fields[idx])
}
