// Package importer parses layered TGA source inputs. Original textual
// representations are always preserved; normalization is a derived concern
// and failures stay in place with diagnostics.
package importer

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

func badQ(orig, unit, diag string) domain.Q {
	return domain.Q{Original: orig, Unit: unit, OK: false, Diag: diag}
}

// ParseMass parses a mass cell into milligrams.
func ParseMass(s string) domain.Q {
	orig := s
	s = strings.TrimSpace(s)
	num, unit, err := splitNumUnit(s)
	q := domain.Q{Original: orig, Unit: unit, Basis: "sample_pan"}
	if err != nil {
		q.Diag = err.Error()
		return q
	}
	switch strings.ToLower(unit) {
	case "", "mg":
		q.Value, q.NormUnit, q.OK = num, "mg", true
	case "g":
		q.Value, q.NormUnit, q.OK = num*1000, "mg", true
	case "ug", "µg":
		q.Value, q.NormUnit, q.OK = num/1000.0, "mg", true
	default:
		q.Diag = fmt.Sprintf("unknown mass unit %q; value left in place", unit)
	}
	return q
}

// ParseTemp parses a temperature cell into degrees Celsius.
func ParseTemp(s string) domain.Q {
	orig := s
	s = strings.TrimSpace(s)
	num, unit, err := splitNumUnit(s)
	q := domain.Q{Original: orig, Unit: unit, Basis: "furnace_program"}
	if err != nil {
		q.Diag = err.Error()
		return q
	}
	switch strings.ToLower(unit) {
	case "", "c", "°c", "degc":
		q.Value, q.NormUnit, q.OK = num, "degC", true
	case "k":
		q.Value, q.NormUnit, q.OK = num-273.15, "degC", true
	case "f", "°f":
		q.Value, q.NormUnit, q.OK = (num-32)*5/9, "degC", true
	default:
		q.Diag = fmt.Sprintf("unknown temperature unit %q; value left in place", unit)
	}
	return q
}

// ParseSeconds parses a time cell into seconds.
func ParseSeconds(s string) (float64, string, error) {
	num, unit, err := splitNumUnit(strings.TrimSpace(s))
	if err != nil {
		return 0, unit, err
	}
	switch strings.ToLower(unit) {
	case "", "s", "sec", "secs":
		return num, "s", nil
	case "min", "m", "mins":
		return num * 60, "s", nil
	case "h", "hr", "hrs":
		return num * 3600, "s", nil
	}
	return 0, unit, fmt.Errorf("unknown time unit %q", unit)
}

func splitNumUnit(s string) (float64, string, error) {
	if s == "" {
		return 0, "", fmt.Errorf("empty numeric field")
	}
	i := 0
	dot, e := false, false
	for i < len(s) {
		c := s[i]
		if c >= '0' && c <= '9' {
			i++
			continue
		}
		if c == '.' && !dot && !e && i > 0 {
			dot = true
			i++
			continue
		}
		if (c == 'e' || c == 'E') && !e && i > 0 {
			e = true
			i++
			if i < len(s) && (s[i] == '+' || s[i] == '-') {
				i++
			}
			continue
		}
		if c == '-' && i == 0 {
			i++
			continue
		}
		break
	}
	numText := strings.TrimSpace(s[:i])
	unit := strings.TrimSpace(s[i:])
	if numText == "" {
		return 0, unit, fmt.Errorf("no numeric prefix in %q", s)
	}
	num, err := strconv.ParseFloat(numText, 64)
	if err != nil {
		return 0, unit, fmt.Errorf("bad number %q", numText)
	}
	if math.IsNaN(num) || math.IsInf(num, 0) {
		return 0, unit, fmt.Errorf("non-finite number")
	}
	return num, unit, nil
}
