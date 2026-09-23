// Package ingest turns submitted bytes into immutable domain documents. It
// accepts a small, copy-pasteable sample format and emits precise format
// errors. Original representations are always retained.
package ingest

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// canonical units: seconds, milligrams, degrees Celsius.

// convertTime converts value expressed in unit to seconds.
func convertTime(v float64, unit string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "s", "sec", "second", "seconds":
		return v, true
	case "ms":
		return v / 1000.0, true
	case "min", "minute", "minutes":
		return v * 60.0, true
	case "h", "hr", "hour", "hours":
		return v * 3600.0, true
	}
	return 0, false
}

// convertMass converts to milligrams.
func convertMass(v float64, unit string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "mg":
		return v, true
	case "ug", "µg":
		return v / 1000.0, true
	case "g":
		return v * 1000.0, true
	}
	return 0, false
}

// convertTemp converts to degrees Celsius.
func convertTemp(v float64, unit string) (float64, bool) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "c", "°c", "degc", "celsius":
		return v, true
	case "k", "kelvin":
		return v - 273.15, true
	case "f", "°f", "degf", "fahrenheit":
		return (v - 32.0) * 5.0 / 9.0, true
	}
	return 0, false
}

// parseNumeric parses a cell, recording the original representation. "n/a",
// empty and non-numeric values return ok=false; the caller attaches a
// diagnostic instead of discarding the row.
func parseNumeric(raw string) (float64, bool) {
	s := strings.TrimSpace(raw)
	if s == "" || strings.EqualFold(s, "n/a") || strings.EqualFold(s, "na") || s == "-" {
		return 0, false
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, false
	}
	return v, true
}

func badCell(row int, field, raw, code, msg string) diag {
	return diag{row: row, field: field, raw: raw, code: code, msg: msg}
}

type diag struct {
	row   int
	field string
	raw   string
	code  string
	msg   string
}

func (d diag) String() string {
	return fmt.Sprintf("row %d field %s: %s (%q)", d.row, d.field, d.msg, d.raw)
}
