// Package tga implements the thermogravimetric analysis domain pipeline:
// unit normalisation, baseline-drift and blank correction, gap-aware DTG,
// atmosphere-delay mapping and stage-candidate generation.
package tga

import (
	"fmt"
	"strings"
)

// Diagnostic describes a value that could not be normalised. The raw
// representation is always preserved alongside the diagnostic.
type Diagnostic struct {
	Where  string `json:"where"`
	Raw    string `json:"raw"`
	Reason string `json:"reason"`
}

// ToSeconds converts a time value to seconds.
func ToSeconds(v float64, unit string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "s", "sec", "secs", "second", "seconds":
		return v, nil
	case "min", "mins", "minute", "minutes":
		return v * 60, nil
	case "h", "hr", "hrs", "hour", "hours":
		return v * 3600, nil
	}
	return 0, fmt.Errorf("unknown time unit %q", unit)
}

// ToMg converts a mass value to milligrams.
func ToMg(v float64, unit string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "mg":
		return v, nil
	case "g":
		return v * 1000, nil
	case "ug", "µg":
		return v / 1000, nil
	}
	return 0, fmt.Errorf("unknown mass unit %q", unit)
}

// ToCelsius converts a temperature value to degrees Celsius.
func ToCelsius(v float64, unit string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "c", "degc", "°c":
		return v, nil
	case "k":
		return v - 273.15, nil
	}
	return 0, fmt.Errorf("unknown temperature unit %q", unit)
}

// ToMlMin converts a flow value to mL/min.
func ToMlMin(v float64, unit string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "ml_min", "ml/min", "sccm":
		return v, nil
	case "l_min", "l/min":
		return v * 1000, nil
	}
	return 0, fmt.Errorf("unknown flow unit %q", unit)
}
