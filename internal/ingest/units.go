// Package ingest parses the copy-pasteable tga-section/v1 text format.
//
// Contract with the domain rules:
//   - original representation is retained per cell (raw token + unit);
//   - derived layers do normalization, parsing only offers canonical values;
//   - values that cannot be converted stay in place with a Diagnostic and
//     never get dropped for the sake of plotting.
package ingest

import (
	"fmt"
	"strconv"
	"strings"

	"tgalineage/internal/model"
)

// Canonical units used by derived layers.
const (
	UnitS   = "s"
	UnitC   = "C"
	UnitMg  = "mg"
	UnitCPM = "C/min"
	UnitPct = "%"
)

// convert returns the canonical value for token in unit.
// Unknown units remain unconverted: Parsable=false, value=0, unit retained.
func convert(token, unit, want string) (model.Repr, error) {
	r := model.Repr{Raw: token, Unit: unit}
	v, err := strconv.ParseFloat(strings.TrimSpace(token), 64)
	if err != nil {
		return r, fmt.Errorf("not a number: %q", token)
	}
	switch want {
	case UnitS:
		switch unit {
		case "", "s", "sec", "second", "seconds":
			r.Value, r.Unit, r.Parsable = v, UnitS, true
		case "min", "minute", "minutes":
			r.Value, r.Unit, r.Parsable = v*60, UnitS, true
		case "h", "hour", "hours":
			r.Value, r.Unit, r.Parsable = v*3600, UnitS, true
		default:
			r.Unit = unit
			return r, fmt.Errorf("cannot convert time unit %q to s", unit)
		}
	case UnitC:
		switch unit {
		case "", "C", "°C", "degC":
			r.Value, r.Unit, r.Parsable = v, UnitC, true
		case "K":
			r.Value, r.Unit, r.Parsable = v-273.15, UnitC, true
		default:
			r.Unit = unit
			return r, fmt.Errorf("cannot convert temperature unit %q to C", unit)
		}
	case UnitMg:
		switch unit {
		case "", "mg":
			r.Value, r.Unit, r.Parsable = v, UnitMg, true
		case "g":
			r.Value, r.Unit, r.Parsable = v*1000, UnitMg, true
		case "ug", "µg":
			r.Value, r.Unit, r.Parsable = v/1000, UnitMg, true
		default:
			r.Unit = unit
			return r, fmt.Errorf("cannot convert mass unit %q to mg", unit)
		}
	case UnitCPM:
		switch unit {
		case "", "C/min", "Cmin-1":
			r.Value, r.Unit, r.Parsable = v, UnitCPM, true
		case "K/min":
			r.Value, r.Unit, r.Parsable = v, UnitCPM, true
		default:
			r.Unit = unit
			return r, fmt.Errorf("cannot convert rate unit %q to C/min", unit)
		}
	case UnitPct:
		switch unit {
		case "", "%":
			r.Value, r.Unit, r.Parsable = v, UnitPct, true
		default:
			r.Unit = unit
			return r, fmt.Errorf("cannot convert %q to %%", unit)
		}
	default:
		r.Value = v
		r.Parsable = true
		r.Unit = unit
	}
	return r, nil
}

// token splits "12.3mg" / "12.3 mg" into number token and unit token.
func token(cell string) (num, unit string) {
	cell = strings.TrimSpace(cell)
	i := 0
	for i < len(cell) {
		c := cell[i]
		if c >= '0' && c <= '9' || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' {
			i++
			continue
		}
		break
	}
	return strings.TrimSpace(cell[:i]), strings.TrimSpace(cell[i:])
}

func asRepr(cell, defaultUnit, want string) (model.Repr, model.Diagnostic) {
	num, unit := token(cell)
	if unit == "" {
		unit = defaultUnit
	}
	r, err := convert(num, unit, want)
	if err != nil {
		d := model.Diagnostic{Level: "error", Code: "unconvertible-value", Message: err.Error()}
		return r, d
	}
	return r, model.Diagnostic{}
}
