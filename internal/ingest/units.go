package ingest

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"tgasys/internal/domain"
)

// Quantity keeps the raw text in all cases; conversion failure is a diagnosis,
// never a dropped value.
func quantity(raw, unit string, conv func(float64, string) (float64, bool, string)) domain.Quantity {
	q := domain.Quantity{Raw: raw, Unit: strings.TrimSpace(unit)}
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		q.Diagnostic = fmt.Sprintf("unparseable number %q", raw)
		return q
	}
	nv, ok, diag := conv(v, strings.ToLower(strings.TrimSpace(unit)))
	if !ok {
		q.Diagnostic = diag
		return q
	}
	q.Value = nv
	q.Converted = true
	return q
}

func convMass(v float64, unit string) (float64, bool, string) {
	switch unit {
	case "g", "gram", "grams":
		return v, true, ""
	case "mg":
		return v / 1000.0, true, ""
	case "kg":
		return v * 1000.0, true, ""
	case "ug", "µg":
		return v / 1e6, true, ""
	}
	return 0, false, fmt.Sprintf("unknown mass unit %q", unit)
}

func convTime(v float64, unit string) (float64, bool, string) {
	switch unit {
	case "s", "sec", "second", "seconds":
		return v, true, ""
	case "min", "minute", "minutes":
		return v * 60.0, true, ""
	case "ms":
		return v / 1000.0, true, ""
	case "h", "hour", "hours":
		return v * 3600.0, true, ""
	}
	return 0, false, fmt.Sprintf("unknown time unit %q", unit)
}

func convTemp(v float64, unit string) (float64, bool, string) {
	switch unit {
	case "c", "°c", "degc", "celsius":
		return v, true, ""
	case "k", "kelvin":
		return v - 273.15, true, ""
	case "f", "°f", "degf":
		return (v - 32.0) * 5.0 / 9.0, true, ""
	}
	return 0, false, fmt.Sprintf("unknown temperature unit %q", unit)
}

func convSlope(v float64, unit string) (float64, bool, string) {
	switch unit {
	case "g/s":
		return v, true, ""
	case "mg/s":
		return v / 1000.0, true, ""
	case "mg/min", "mg/minute":
		return v / 1000.0 / 60.0, true, ""
	case "g/min":
		return v / 60.0, true, ""
	}
	return 0, false, fmt.Sprintf("unknown drift slope unit %q", unit)
}

func convRate(v float64, unit string) (float64, bool, string) {
	switch unit {
	case "c/min", "°c/min", "k/min":
		return v, true, ""
	case "c/s":
		return v * 60.0, true, ""
	}
	return 0, false, fmt.Sprintf("unknown ramp rate unit %q", unit)
}

func fptr(v float64) *float64 { return &v }

// finite reports whether a converted value is usable numerically.
func finite(v *float64) bool {
	return v != nil && !math.IsNaN(*v) && !math.IsInf(*v, 0)
}
