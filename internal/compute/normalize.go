package compute

import (
	"strconv"

	"tgareview/internal/domain"
)

// ParseQ converts a raw "value unit" token to the canonical unit. Conversions
// supported: min->s, C/min->C/min identity. Unconvertible values stay Bad.
func ParseQ(rawText string, canonical domain.Unit) domain.Q {
	out := domain.Q{Raw: rawText, Unit: canonical}
	var num, unit string
	num, unit = split(rawText)
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		out.Bad = true
		out.Diag = "non-numeric value: " + num
		return out
	}
	out.Value = v
	switch canonical {
	case domain.UnitS:
		switch unit {
		case "s":
		case "min":
			out.Value = v * 60
		default:
			out.Bad = true
			out.Diag = "cannot convert unit " + unit + " to s"
		}
	case domain.UnitC:
		if unit != "C" {
			out.Bad = true
			out.Diag = "cannot convert unit " + unit + " to C"
		}
	case domain.UnitMg:
		if unit != "mg" {
			out.Bad = true
			out.Diag = "cannot convert unit " + unit + " to mg"
		}
	case domain.UnitMgPerMin:
		if unit != "mg/min" {
			out.Bad = true
			out.Diag = "cannot convert unit " + unit + " to mg/min"
		}
	default:
		out.Unit = domain.Unit(unit)
	}
	return out
}

func split(s string) (string, string) {
	i := 0
	for i < len(s) && (isDigit(rune(s[i])) || s[i] == '.' || s[i] == '-' || s[i] == '+') {
		i++
	}
	return s[:i], s[i:]
}

func isDigit(r rune) bool { return r >= '0' && r <= '9' }

// field returns the raw token of a named field.
func field(fs []domain.Field, name string) string {
	for _, f := range fs {
		if f.Name == name {
			return f.Raw
		}
	}
	return ""
}
