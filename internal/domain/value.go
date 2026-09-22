package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// canonical normalised targets
const (
	UnitS      = "s"
	UnitMin    = "min"
	UnitMg     = "mg"
	UnitG      = "g"
	UnitUg     = "ug"
	UnitDegC   = "degC"
	UnitK      = "K"
	UnitMgPerS = "mg/s"
)

// timeConversions converts a time unit to seconds.
var timeConversions = map[string]float64{
	UnitS: 1, "sec": 1, "secs": 1, "second": 1, "seconds": 1,
	UnitMin: 60, "m": 60, "mins": 60, "minute": 60, "minutes": 60,
}

// massConversions converts a mass unit to milligrams.
var massConversions = map[string]float64{
	UnitMg: 1, "mgs": 1, "milligram": 1, "milligrams": 1,
	UnitG: 1000, "gram": 1000, "grams": 1000,
	UnitUg: 0.001, "mcg": 0.001, "microgram": 0.001, "micrograms": 0.001,
}

// ParseValue parses "12.3 mg" style text into a Value normalised to target
// category ("time","mass","temp"). The raw text is always preserved; a value
// that cannot be converted is returned with Valid=false and an error message,
// never dropped.
func ParseValue(raw, category string) Value {
	v := Value{Raw: strings.TrimSpace(raw)}
	numPart, unit := splitNumberUnit(v.Raw)
	if numPart == "" {
		v.Error = "empty numeric representation"
		return v
	}
	num, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		v.Error = fmt.Sprintf("cannot parse number %q", numPart)
		return v
	}
	unit = strings.TrimSpace(unit)
	v.Unit = unit

	switch category {
	case "time":
		if factor, ok := timeConversions[normUnit(unit)]; ok {
			v.Value, v.Valid = num*factor, true
			v.Unit = UnitS
			return v
		}
		if unit == "" {
			v.Value, v.Valid = num, true
			v.Unit = UnitS
			return v
		}
		v.Error = fmt.Sprintf("cannot convert time unit %q to seconds", unit)
	case "mass":
		if factor, ok := massConversions[normUnit(unit)]; ok {
			v.Value, v.Valid = num*factor, true
			v.Unit = UnitMg
			return v
		}
		if unit == "" {
			v.Value, v.Valid = num, true
			v.Unit = UnitMg
			return v
		}
		v.Error = fmt.Sprintf("cannot convert mass unit %q to milligrams", unit)
	case "temp":
		switch normUnit(unit) {
		case "degc", "c", "°c":
			v.Value, v.Valid = num, true
			v.Unit = UnitDegC
		case "k":
			v.Value, v.Valid = num-273.15, true
			v.Unit = UnitDegC
		case "":
			v.Value, v.Valid = num, true
			v.Unit = UnitDegC
		default:
			v.Error = fmt.Sprintf("cannot convert temperature unit %q to degC", unit)
		}
	case "rate":
		switch normUnit(unit) {
		case "mg/s", "":
			v.Value, v.Valid = num, true
			v.Unit = UnitMgPerS
		case "mg/min":
			v.Value, v.Valid = num/60, true
			v.Unit = UnitMgPerS
		default:
			v.Error = fmt.Sprintf("cannot convert rate unit %q to mg/s", unit)
		}
	case "number":
		if unit != "" {
			v.Error = fmt.Sprintf("unexpected unit %q on dimensionless value", unit)
			return v
		}
		v.Value, v.Valid = num, true
	default:
		v.Error = fmt.Sprintf("unknown quantity category %q", category)
	}
	return v
}

func splitNumberUnit(s string) (string, string) {
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
	return s[:i], s[i:]
}

func normUnit(u string) string {
	return strings.ToLower(strings.TrimSpace(u))
}

// Fingerprint returns a stable canonical JSON fingerprint of a value (used for
// parameter fingerprints).
func canonicalJSON(a any) string {
	b, err := json.Marshal(a)
	if err != nil {
		return ""
	}
	return string(b)
}

// ParamFingerprint fingerprints parameters together with algorithm version.
func ParamFingerprint(algoVersion string, p Params) string {
	return algoVersion + "|" + canonicalJSON(p)
}
