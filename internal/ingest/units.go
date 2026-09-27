// Package ingest parses the layered raw evidence streams. Parsing NEVER
// overwrites the original representation: every parsed value keeps its raw
// text and declared unit. Values that cannot be converted stay in place with a
// diagnostic (Valid=false) instead of being dropped for plotting.
package ingest

import (
	"math"
	"strconv"
	"strings"

	"tga/internal/domain"
)

// QuantityFromText preserves raw text and records conversion problems.
func QuantityFromText(raw, declaredUnit string) domain.Quantity {
	var q domain.Quantity
	q.Raw = raw
	q.Unit = strings.TrimSpace(declaredUnit)
	t := strings.TrimSpace(raw)
	if t == "" {
		q.Valid = false
		q.Reason = "empty value"
		return q
	}
	f, err := strconv.ParseFloat(t, 64)
	if err != nil {
		q.Valid = false
		q.Reason = "not a number: " + err.Error()
		return q
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		q.Valid = false
		q.Reason = "non-finite value"
		return q
	}
	q.Value = f
	q.Valid = true
	return q
}

func ToSeconds(v float64, unit string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "s", "sec", "secs", "second", "seconds":
		return v, true
	case "min", "minute", "minutes":
		return v * 60, true
	case "h", "hr", "hour", "hours":
		return v * 3600, true
	case "ms":
		return v / 1000, true
	}
	return 0, false
}

func ToCelsius(v float64, unit string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "c", "°c", "celsius", "degc":
		return v, true
	case "k", "kelvin":
		return v - 273.15, true
	case "f", "°f", "fahrenheit":
		return (v - 32) * 5 / 9, true
	}
	return 0, false
}

func ToGrams(v float64, unit string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "g", "gram", "grams":
		return v, true
	case "mg", "milligram", "milligrams":
		return v / 1000, true
	case "ug", "µg", "microgram", "micrograms":
		return v / 1e6, true
	}
	return 0, false
}
