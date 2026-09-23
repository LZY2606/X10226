package ingest

import (
	"regexp"
	"strconv"
	"strings"

	"tga-evidence/internal/domain"
)

// tokenRe splits "10,25 mg" / "12.3mg" / "300" into number and unit parts.
var tokenRe = regexp.MustCompile(`^\s*([+-]?[0-9][0-9., ]*?)\s*([^0-9.,][^0-9]*)?\s*$`)

// ParseQuantity preserves the raw token and normalizes to the canonical unit
// given by wantUnit. Supported conversions:
//   mass:   g -> mg; wantUnit mg
//   time:   min -> s, h -> s; wantUnit s
//   temp:   K -> °C; wantUnit C
//   flow:   mL/min, ml/min -> sccm; wantUnit sccm
// Unconvertible or unparseable tokens return Valid=false with a diagnostic,
// never an error: the raw value stays where it was.
func ParseQuantity(token, wantUnit string) (domain.Quantity, *domain.Diagnostic) {
	q := domain.Quantity{Raw: strings.TrimSpace(token), Unit: wantUnit}
	if q.Raw == "" {
		return q, &domain.Diagnostic{Code: "empty_value", Message: "empty numeric field"}
	}
	m := tokenRe.FindStringSubmatch(token)
	if m == nil {
		return q, &domain.Diagnostic{Code: "unparseable", Message: "cannot parse numeric token: " + q.Raw}
	}
	numPart := strings.TrimSpace(m[1])
	unitPart := strings.ToLower(strings.TrimSpace(m[2]))

	// German/French decimal comma: "10,25" -> "10.25". Spaces used as
	// thousands separators are dropped; a comma followed by exactly 3 digits
	// and then a dot/comma boundary is treated as grouping instead.
	numPart = strings.ReplaceAll(numPart, " ", "")
	if strings.Contains(numPart, ",") && !strings.Contains(numPart, ".") {
		numPart = strings.ReplaceAll(numPart, ",", ".")
	} else {
		numPart = strings.ReplaceAll(numPart, ",", "")
	}
	v, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return q, &domain.Diagnostic{Code: "unparseable", Message: err.Error()}
	}

	switch wantUnit {
	case "mg":
		switch unitPart {
		case "", "mg":
		case "g":
			v *= 1000
		default:
			return q, &domain.Diagnostic{Code: "bad_unit", Message: "unsupported mass unit: " + unitPart}
		}
	case "s":
		switch unitPart {
		case "", "s", "sec", "secs":
		case "min", "m":
			v *= 60
		case "h", "hr":
			v *= 3600
		default:
			return q, &domain.Diagnostic{Code: "bad_unit", Message: "unsupported time unit: " + unitPart}
		}
	case "C":
		switch unitPart {
		case "", "c", "°c", "degc":
		case "k":
			v -= 273.15
		default:
			return q, &domain.Diagnostic{Code: "bad_unit", Message: "unsupported temperature unit: " + unitPart}
		}
	case "sccm":
		switch unitPart {
		case "", "sccm", "ml/min", "mlm":
		default:
			return q, &domain.Diagnostic{Code: "bad_unit", Message: "unsupported flow unit: " + unitPart}
		}
	default:
		return q, &domain.Diagnostic{Code: "bad_basis", Message: "unknown canonical unit: " + wantUnit}
	}
	q.Value = v
	q.Valid = true
	return q, nil
}
