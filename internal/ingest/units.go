package ingest

import (
	"fmt"
	"strconv"
	"strings"

	"tga-archive/internal/model"
)

// timeScale converts a time unit to seconds.
func timeScale(u string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(u)) {
	case "s", "sec", "secs", "second", "seconds":
		return 1, nil
	case "min", "minute", "minutes":
		return 60, nil
	case "h", "hr", "hour", "hours":
		return 3600, nil
	case "ms":
		return 0.001, nil
	}
	return 0, fmt.Errorf("unknown time unit %q", u)
}

// tempToKelvin converts a temperature value in the given unit to kelvin.
func tempToKelvin(v float64, u string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(u)) {
	case "k", "kelvin":
		return v, nil
	case "c", "°c", "degc", "celsius":
		return v + 273.15, nil
	case "f", "°f", "degf", "fahrenheit":
		return (v-32)*5/9 + 273.15, nil
	}
	return 0, fmt.Errorf("unknown temperature unit %q", u)
}

// massScale converts a mass unit to milligrams.
func massScale(u string) (float64, error) {
	switch strings.ToLower(strings.TrimSpace(u)) {
	case "mg":
		return 1, nil
	case "g":
		return 1000, nil
	case "ug", "µg":
		return 0.001, nil
	}
	return 0, fmt.Errorf("unknown mass unit %q", u)
}

func mkUnit(orig, base string, scale float64) model.Unit {
	return model.Unit{Original: orig, Base: base, Scale: scale}
}

// parseFloatKeepRaw parses one scalar, preserving its original text.
func parseFloatKeepRaw(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, false
	}
	return f, true
}
