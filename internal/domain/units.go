package domain

import (
	"math"
	"strconv"
	"strings"
)

// NormalizeTime converts a raw time into seconds. Unknown units stay invalid.
func NormalizeTime(raw, unit string) Q {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return Quantity(raw, unit, math.NaN(), false, "not a number")
	}
	u := strings.ToLower(strings.TrimSpace(unit))
	switch u {
	case "s", "sec", "secs", "second", "seconds":
		return Quantity(raw, UnitSecond, v, true, "")
	case "min", "mins", "minute", "minutes":
		return Quantity(raw, UnitSecond, v*60, true, "")
	case "ms", "millis":
		return Quantity(raw, UnitSecond, v/1000.0, true, "")
	case "h", "hr", "hour", "hours":
		return Quantity(raw, UnitSecond, v*3600, true, "")
	}
	return Quantity(raw, unit, math.NaN(), false, "unknown time unit: "+unit)
}

// NormalizeTemp converts a raw temperature into °C.
func NormalizeTemp(raw, unit string) Q {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return Quantity(raw, unit, math.NaN(), false, "not a number")
	}
	u := strings.ToLower(strings.TrimSpace(unit))
	switch u {
	case "c", "°c", "degc", "celsius":
		return Quantity(raw, UnitCelsius, v, true, "")
	case "k", "kelvin":
		return Quantity(raw, UnitCelsius, v-273.15, true, "")
	case "f", "°f", "fahrenheit":
		return Quantity(raw, UnitCelsius, (v-32)*5/9, true, "")
	}
	return Quantity(raw, unit, math.NaN(), false, "unknown temperature unit: "+unit)
}

// NormalizeMass converts a raw mass into mg.
func NormalizeMass(raw, unit string) Q {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return Quantity(raw, unit, math.NaN(), false, "not a number")
	}
	u := strings.ToLower(strings.TrimSpace(unit))
	switch u {
	case "mg", "milligram", "milligrams":
		return Quantity(raw, UnitMg, v, true, "")
	case "g", "gram", "grams":
		return Quantity(raw, UnitMg, v*1000, true, "")
	case "ug", "µg", "microgram", "micrograms":
		return Quantity(raw, UnitMg, v/1000.0, true, "")
	}
	return Quantity(raw, unit, math.NaN(), false, "unknown mass unit: "+unit)
}

// NormalizeRate converts mg/min-like drift rates.
func NormalizeRate(raw, unit string) Q {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return Quantity(raw, unit, math.NaN(), false, "not a number")
	}
	u := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(unit), " ", ""))
	switch u {
	case "mg/min", "mgmin-1", "mgmin^-1":
		return Quantity(raw, UnitMgPerMin, v, true, "")
	case "mg/s":
		return Quantity(raw, UnitMgPerMin, v*60, true, "")
	}
	return Quantity(raw, unit, math.NaN(), false, "unknown rate unit: "+unit)
}
