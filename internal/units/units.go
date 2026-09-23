// Package units normalizes domain quantities to canonical bases
// (s, degC, mg, mL/min). Values in unknown units are left in place and
// reported through diagnostics; they are never silently dropped.
package units

import (
	"fmt"
	"strconv"
	"strings"
)

// Canonical bases used by the derived layer.
const (
	TimeBase = "s"
	TempBase = "C"
	MassBase = "mg"
	FlowBase = "mL/min"
)

// ToSeconds converts a time value with unit to seconds.
func ToSeconds(v float64, unit string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "s", "sec", "secs", "second", "seconds":
		return v, nil
	case "min", "mins", "minute", "minutes":
		return v * 60, nil
	case "h", "hr", "hour", "hours":
		return v * 3600, nil
	}
	return 0, fmt.Errorf("unknown time unit %q", unit)
}

// ToCelsius converts a temperature to degC.
func ToCelsius(v float64, unit string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "c", "degc", "°c":
		return v, nil
	case "k", "kelvin":
		return v - 273.15, nil
	case "f", "degf", "°f":
		return (v - 32) * 5 / 9, nil
	}
	return 0, fmt.Errorf("unknown temperature unit %q", unit)
}

// ToMilligram converts a mass to mg.
func ToMilligram(v float64, unit string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "mg":
		return v, nil
	case "g":
		return v * 1000, nil
	case "ug", "µg":
		return v / 1000, nil
	case "kg":
		return v * 1e6, nil
	}
	return 0, fmt.Errorf("unknown mass unit %q", unit)
}

// ToFlow converts a flow to mL/min.
func ToFlow(v float64, unit string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "ml/min", "mlmin", "sccm":
		return v, nil
	case "l/min":
		return v * 1000, nil
	}
	return 0, fmt.Errorf("unknown flow unit %q", unit)
}

// ParseFloat trims and parses a float, accepting a trailing unit when the
// column itself does not declare one ("12.5 mg").
func ParseFloat(s string) (float64, string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, "", fmt.Errorf("empty value")
	}
	fields := strings.Fields(s)
	if len(fields) == 1 {
		v, err := strconv.ParseFloat(fields[0], 64)
		return v, "", err
	}
	v, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return 0, "", err
	}
	return v, strings.Join(fields[1:], " "), nil
}
