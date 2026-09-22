package importer

import (
	"fmt"
	"strconv"
	"strings"
)

// NormalizeMass converts a mass token to milligrams.
func NormalizeMass(token, unit string) (float64, string, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(token), 64)
	if err != nil {
		return 0, "", fmt.Errorf("mass %q is not numeric: %w", token, err)
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "mg":
		return v, "mg", nil
	case "g":
		return v * 1000, "mg", nil
	case "ug", "µg":
		return v / 1000.0, "mg", nil
	default:
		return 0, "", fmt.Errorf("unknown mass unit %q", unit)
	}
}

// NormalizeTemp converts to degrees Celsius.
func NormalizeTemp(token, unit string) (float64, string, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(token), 64)
	if err != nil {
		return 0, "", fmt.Errorf("temperature %q is not numeric: %w", token, err)
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "c", "°c":
		return v, "C", nil
	case "k":
		return v - 273.15, "C", nil
	default:
		return 0, "", fmt.Errorf("unknown temperature unit %q", unit)
	}
}

// NormalizeTime converts to seconds.
func NormalizeTime(token, unit string) (float64, string, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(token), 64)
	if err != nil {
		return 0, "", fmt.Errorf("time %q is not numeric: %w", token, err)
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "s", "sec":
		return v, "s", nil
	case "min", "m":
		return v * 60, "s", nil
	case "h":
		return v * 3600, "s", nil
	default:
		return 0, "", fmt.Errorf("unknown time unit %q", unit)
	}
}
