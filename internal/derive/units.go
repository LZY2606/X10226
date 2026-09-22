// Package derive implements the layered TGA domain calculations.
//
// The raw layer preserves original textual representations; normalization
// happens only in the derived layer. Values that cannot be converted stay in
// place with diagnostics rather than being dropped for plotting.
package derive

import (
	"strconv"
	"strings"

	"tga/internal/domain"
)

func normalizeTime(raw, unit string) (*float64, string) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return nil, "unparseable number: " + err.Error()
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "s", "sec", "secs", "second", "seconds":
		return ptr(v), ""
	case "min", "minute", "minutes":
		return ptr(v * 60), ""
	case "h", "hr", "hour", "hours":
		return ptr(v * 3600), ""
	case "ms", "millisecond", "milliseconds":
		return ptr(v / 1000), ""
	default:
		return nil, "unknown time unit: " + unit
	}
}

func normalizeMass(raw, unit string) (*float64, string) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return nil, "unparseable number: " + err.Error()
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "mg", "milligram", "milligrams":
		return ptr(v), ""
	case "g", "gram", "grams":
		return ptr(v * 1000), ""
	case "ug", "µg", "microgram", "micrograms":
		return ptr(v / 1000), ""
	default:
		return nil, "unknown mass unit: " + unit
	}
}

func normalizeTemp(raw, unit string) (*float64, string) {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return nil, "unparseable number: " + err.Error()
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "c", "°c", "degc", "celsius":
		return ptr(v), ""
	case "k", "kelvin":
		return ptr(v - 273.15), ""
	case "f", "°f", "fahrenheit":
		return ptr((v - 32) * 5 / 9), ""
	default:
		return nil, "unknown temperature unit: " + unit
	}
}

func ptr(v float64) *float64 { return &v }

func annotate(m *domain.Measurement, norm *float64, diag string) {
	m.Normalized = norm
	m.Diagnostics = diag
}
