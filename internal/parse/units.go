// Package parse converts the small import text format into layered sections
// while preserving original representations.
package parse

import (
	"fmt"
	"strconv"
	"strings"

	"tgasys/internal/domain"
)

// ParseQ parses "value unit" (unit optional) and attaches a coordinate basis.
// Values that cannot be converted stay in place with a diagnostic.
func ParseQ(s, basis string) domain.Q {
	s = strings.TrimSpace(s)
	q := domain.Q{Raw: s, Basis: basis}
	if s == "" || strings.EqualFold(s, "n/a") || strings.EqualFold(s, "?") {
		q.Unconvertible = true
		q.Diagnostic = "missing value; raw token retained"
		return q
	}
	fields := strings.Fields(s)
	v, err := strconv.ParseFloat(strings.TrimRight(fields[0], ","), 64)
	if err != nil {
		q.Unconvertible = true
		q.Diagnostic = "non-numeric token: " + err.Error()
		return q
	}
	unit := ""
	if len(fields) > 1 {
		unit = strings.Join(fields[1:], " ")
	}
	norm, normUnit, err := normalize(v, unit, basis)
	if err != nil {
		q.Value = v
		q.Unit = unit
		q.Unconvertible = true
		q.Diagnostic = err.Error()
		return q
	}
	q.Value = norm
	q.Unit = normUnit
	return q
}

// Canonical units used by the derived layer.
const (
	UnitS   = "s"
	UnitMg  = "mg"
	UnitC   = "C"
	UnitK   = "K"
	UnitKPM = "K/min"
	UnitMLM = "mL/min"
)

func normalize(v float64, unit, basis string) (float64, string, error) {
	u := strings.ToLower(strings.TrimSpace(unit))
	switch basis {
	case "furnace_time_s":
		switch u {
		case "", "s", "sec", "secs", "second", "seconds":
			return v, UnitS, nil
		case "min", "minute", "minutes":
			return v * 60, UnitS, nil
		}
	case "mass_mg_actual":
		switch u {
		case "mg":
			return v, UnitMg, nil
		case "g":
			return v * 1000, UnitMg, nil
		case "ug", "µg":
			return v / 1000, UnitMg, nil
		case "":
			return v, "", nil
		}
	case "sample_temp_C", "furnace_temp_C", "setpoint_C":
		switch u {
		case "", "c", "°c", "degc", "degree_c":
			return v, UnitC, nil
		case "k":
			return v - 273.15, UnitC, nil
		case "f", "°f":
			return (v - 32) * 5 / 9, UnitC, nil
		}
	case "rate_K_min":
		switch u {
		case "", "k/min", "c/min", "°c/min":
			return v, UnitKPM, nil
		case "k/s":
			return v * 60, UnitKPM, nil
		}
	case "flow_mL_min":
		switch u {
		case "", "ml/min", "mlm":
			return v, UnitMLM, nil
		}
	}
	return v, unit, fmt.Errorf("unit %q not convertible on basis %q", unit, basis)
}
