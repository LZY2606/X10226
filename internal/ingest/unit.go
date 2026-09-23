package ingest

import (
	"fmt"
	"strconv"
	"strings"

	"gotga/internal/domain"
)

// ParseMeasure converts one raw cell into a domain.Measure. Unconvertible
// values stay at their original position with a diagnostic; they are never
// silently dropped to make plots render.
func ParseMeasure(raw, wantKind, column string, row int) domain.Measure {
	m := domain.Measure{Raw: raw, Column: column, Row: row}
	s := strings.TrimSpace(raw)
	num, unit := splitNumUnit(s)
	m.Unit = unit
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		m.Valid = false
		m.Diag = fmt.Sprintf("unconvertible value %q for %s at row %d col %s", raw, wantKind, row, column)
		return m
	}
	cv, cu, err := convert(v, unit, wantKind)
	if err != nil {
		m.Valid = false
		m.Diag = err.Error()
		return m
	}
	m.Valid = true
	m.Value = cv
	m.Unit = cu
	return m
}

func splitNumUnit(s string) (string, string) {
	i := 0
	for i < len(s) {
		r := s[i]
		if (r >= '0' && r <= '9') || r == '.' || r == '-' || r == '+' || r == 'e' || r == 'E' {
			i++
			continue
		}
		break
	}
	return s[:i], strings.TrimSpace(s[i:])
}

func convert(v float64, unit, kind string) (float64, string, error) {
	unit = strings.ToLower(strings.TrimSpace(unit))
	switch kind {
	case "time":
		switch unit {
		case "", "s", "sec", "secs", "second", "seconds":
			return v, "s", nil
		case "min", "minute", "minutes":
			return v * 60, "s", nil
		case "ms", "millisecond", "milliseconds":
			return v / 1000, "s", nil
		}
	case "mass":
		switch unit {
		case "mg", "":
			return v, "mg", nil
		case "g":
			return v * 1000, "mg", nil
		case "ug", "µg":
			return v / 1000, "mg", nil
		}
	case "temperature":
		switch unit {
		case "c", "°c", "", "celsius":
			return v, "C", nil
		case "k", "kelvin":
			return v - 273.15, "C", nil
		case "f", "fahrenheit":
			return (v - 32) * 5 / 9, "C", nil
		}
	case "rate":
		switch unit {
		case "mg/min", "":
			return v, "mg/min", nil
		case "mg/s":
			return v * 60, "mg/min", nil
		}
	case "percent":
		switch unit {
		case "%", "pct", "":
			return v, "%", nil
		case "fraction":
			return v * 100, "%", nil
		}
	}
	return 0, "", fmt.Errorf("cannot convert unit %q to %s", unit, kind)
}
