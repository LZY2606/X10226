package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// valuePattern extracts a leading numeric token and a trailing unit token.
var valuePattern = regexp.MustCompile(`^\s*([+-]?(?:\d+\.?\d*|\.\d+)(?:[eE][+-]?\d+)?)\s*(.*?)\s*$`)

// ParseValue preserves the original text. Recognised units are normalised;
// unrecognised or non-numeric values get OK=false plus a diagnostic instead
// of being dropped for plotting.
func ParseValue(text, expected string) RawValue {
	rv := RawValue{Text: text, Unit: ""}
	m := valuePattern.FindStringSubmatch(text)
	if m == nil {
		rv.OK = false
		rv.Diags = []string{fmt.Sprintf("unparseable value %q (expected %s)", text, expected)}
		return rv
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		rv.OK = false
		rv.Diags = []string{fmt.Sprintf("non-numeric value %q: %v", text, err)}
		return rv
	}
	unit := strings.TrimSpace(m[2])
	converted, normUnit, diags := convertUnit(v, unit, expected)
	rv.Unit = normUnit
	if len(diags) == 0 {
		rv.Value = converted
		rv.OK = true
	} else {
		rv.Diags = diags
	}
	return rv
}

func convertUnit(v float64, unit, expected string) (float64, string, []string) {
	u := strings.ToLower(unit)
	switch expected {
	case "time":
		switch u {
		case "s", "sec", "secs", "second", "seconds", "":
			return v, "s", nil
		case "min", "minute", "minutes":
			return v * 60, "s", nil
		case "ms":
			return v / 1000, "s", nil
		case "h", "hr", "hour", "hours":
			return v * 3600, "s", nil
		}
	case "temp":
		switch u {
		case "c", "°c", "degc", "celsius", "":
			return v, "°C", nil
		case "k", "kelvin":
			return v - 273.15, "°C", nil
		case "f", "°f", "fahrenheit":
			return (v - 32) * 5 / 9, "°C", nil
		}
	case "mass":
		switch u {
		case "mg", "":
			return v, "mg", nil
		case "g":
			return v * 1000, "mg", nil
		case "ug", "µg":
			return v / 1000, "mg", nil
		}
	case "flow":
		switch u {
		case "sccm", "":
			return v, "sccm", nil
		case "ml/min":
			return v, "sccm", nil
		}
	}
	return 0, unit, []string{fmt.Sprintf("unit %q not convertible to %s; value kept at original location", unit, expected)}
}

// Fingerprint returns a stable short sha256 hash of a canonical JSON payload.
func Fingerprint(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// ShortID truncates a fingerprint for human-facing stable identifiers.
func ShortID(h string, n int) string {
	if len(h) > n {
		return h[:n]
	}
	return h
}
