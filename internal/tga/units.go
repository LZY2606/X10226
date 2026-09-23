package tga

import (
	"fmt"
	"strconv"
	"strings"
)

// parseTimeS converts a time representation with optional unit to seconds.
func parseTimeS(s, unitHint string) (float64, string, bool) {
	v, unit, err := splitNumberUnit(s)
	if err != nil {
		return 0, unitHint, false
	}
	if unit == "" {
		unit = unitHint
	}
	switch strings.ToLower(unit) {
	case "s", "sec", "secs", "second", "seconds":
		return v, BasisTimeS, true
	case "min", "mins", "minute", "minutes":
		return v * 60, BasisTimeS, true
	case "h", "hr", "hour", "hours":
		return v * 3600, BasisTimeS, true
	case "":
		return v, BasisTimeS, true
	}
	return 0, unit, false
}

// parseTempC converts a temperature with optional unit to degrees Celsius.
func parseTempC(s string) (float64, string, bool) {
	v, unit, err := splitNumberUnit(s)
	if err != nil {
		return 0, "", false
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "c", "°c", "degc", "celsius":
		return v, BasisTempC, true
	case "k":
		return v - 273.15, BasisTempC, true
	case "f", "°f":
		return (v - 32) * 5 / 9, BasisTempC, true
	}
	return v, unit, false
}

// parseMassMg converts a mass with optional unit to milligrams.
func parseMassMg(s string) (float64, string, bool) {
	v, unit, err := splitNumberUnit(s)
	if err != nil {
		return 0, "", false
	}
	switch strings.ToLower(strings.TrimSpace(unit)) {
	case "", "mg":
		return v, BasisMassMg, true
	case "g":
		return v * 1000, BasisMassMg, true
	case "ug", "µg":
		return v / 1000, BasisMassMg, true
	}
	return v, unit, false
}

// splitNumberUnit splits "12.3 mg" into 12.3 and "mg".
func splitNumberUnit(s string) (float64, string, error) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' {
			i++
			continue
		}
		break
	}
	num := strings.TrimSpace(s[:i])
	rest := strings.TrimSpace(s[i:])
	if num == "" {
		return 0, rest, fmt.Errorf("no number in %q", s)
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, rest, err
	}
	return v, rest, nil
}

// fmtF formats a float compactly for stored display strings.
func fmtF(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
