package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var numRe = regexp.MustCompile(`^[+-]?(?:\d+(?:\.\d*)?|\.\d+)(?:[eE][+-]?\d+)?$`)

// canonical unit maps.
var timeUnits = map[string]float64{
	"s": 1, "sec": 1, "second": 1, "seconds": 1,
	"min": 60, "m": 60, "minute": 60, "minutes": 60,
	"h": 3600, "hr": 3600, "hour": 3600, "hours": 3600,
}
var tempUnits = map[string]bool{"c": true, "°c": true, "k": true, "f": true, "°f": true}
var massUnits = map[string]float64{
	"mg": 1, "milligram": 1, "milligrams": 1,
	"g": 1000, "gram": 1000, "grams": 1000,
	"ug": 0.001, "µg": 0.001, "kg": 1e6,
}
var rateUnits = map[string]bool{"c/min": true, "c/s": true, "k/min": true, "k/s": true}

// ParseQ keeps the original text and normalizes to the canonical unit of dim.
// Canonical units: time -> s, temp -> C, mass -> mg, rate -> C/min.
func ParseQ(raw string, dim Dim) Q {
	q := Q{Text: strings.TrimSpace(raw)}
	if q.Text == "" {
		q.Diag = "empty value"
		return q
	}
	m := splitNumberUnit(q.Text)
	if !numRe.MatchString(m.num) {
		q.Diag = fmt.Sprintf("not a number: %q", m.num)
		return q
	}
	val, err := strconv.ParseFloat(m.num, 64)
	if err != nil {
		q.Diag = err.Error()
		return q
	}
	unit := strings.ToLower(strings.TrimSpace(m.unit))
	switch dim {
	case DimTime:
		f, ok := timeUnits[unit]
		if !ok {
			q.Diag = fmt.Sprintf("unknown time unit %q", m.unit)
			return q
		}
		q.OK, q.Value, q.Unit = true, val*f, "s"
	case DimMass:
		f, ok := massUnits[unit]
		if !ok {
			q.Diag = fmt.Sprintf("unknown mass unit %q", m.unit)
			return q
		}
		q.OK, q.Value, q.Unit = true, val*f, "mg"
	case DimTemp:
		if !tempUnits[unit] {
			q.Diag = fmt.Sprintf("unknown temperature unit %q", m.unit)
			return q
		}
		switch unit {
		case "k":
			val -= 273.15
		case "f", "°f":
			val = (val - 32) * 5 / 9
		}
		q.OK, q.Value, q.Unit = true, val, "C"
	case DimRate:
		if !rateUnits[unit] {
			q.Diag = fmt.Sprintf("unknown rate unit %q", m.unit)
			return q
		}
		if strings.HasSuffix(unit, "/s") {
			val *= 60
		}
		q.OK, q.Value, q.Unit = true, val, "C/min"
	default:
		q.Diag = "unknown dimension"
	}
	return q
}

type numUnit struct {
	num  string
	unit string
}

func splitNumberUnit(s string) numUnit {
	s = strings.TrimSpace(s)
	// strip a leading sign for boundary scan
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	for i < len(s) {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' ||
			(c == '+' || c == '-') && i > 0 && (s[i-1] == 'e' || s[i-1] == 'E') {
			i++
			continue
		}
		break
	}
	return numUnit{num: strings.TrimSpace(s[:i]), unit: strings.TrimSpace(s[i:])}
}
