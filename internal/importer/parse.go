package importer

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

var numRe = regexp.MustCompile(`[-+]?[0-9]*\.?[0-9]+(?:[eE][-+]?[0-9]+)?`)

// parseQuantity extracts a number while preserving the raw token.
// canonicalUnit is the unit the derived layer works in; hint is an optional
// unit hint from the row. Unconvertible/unknown cells come back with
// Status=error or unknown and stay in place.
func parseQuantity(raw, hint string, canonical domain.Unit, toSec, toMG, toC func(float64, domain.Unit) (float64, bool)) domain.Number {
	n := domain.Number{Raw: raw, Unit: canonical, Status: domain.StatusOK}
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		n.Status = domain.StatusUnknown
		n.DiagCode = "EMPTY_CELL"
		n.DiagMsg = "empty sample cell"
		return n
	}
	low := strings.ToLower(trimmed)
	if low == "n/a" || low == "na" || low == "null" || low == "--" || low == "?" {
		n.Status = domain.StatusUnknown
		n.DiagCode = "UNCONVERTIBLE"
		n.DiagMsg = fmt.Sprintf("value %q cannot be converted to %s", trimmed, canonical)
		return n
	}
	m := numRe.FindString(trimmed)
	if m == "" {
		n.Status = domain.StatusError
		n.DiagCode = "NOT_A_NUMBER"
		n.DiagMsg = fmt.Sprintf("no numeric component in %q", trimmed)
		return n
	}
	v, err := strconv.ParseFloat(m, 64)
	if err != nil {
		n.Status = domain.StatusError
		n.DiagCode = "PARSE_FAIL"
		n.DiagMsg = err.Error()
		return n
	}
	unit := inferUnit(trimmed, hint, canonical)
	switch canonical {
	case domain.UnitSeconds:
		if cv, ok := toSec(v, unit); ok {
			v = cv
		} else {
			n.Status = domain.StatusError
			n.DiagCode = "UNIT_MISMATCH"
			n.DiagMsg = fmt.Sprintf("cannot interpret unit %q as time", unit)
		}
	case domain.UnitMG:
		if cv, ok := toMG(v, unit); ok {
			v = cv
		} else {
			n.Status = domain.StatusError
			n.DiagCode = "UNIT_MISMATCH"
			n.DiagMsg = fmt.Sprintf("cannot interpret unit %q as mass", unit)
		}
	case domain.UnitCelsius:
		if cv, ok := toC(v, unit); ok {
			v = cv
		} else {
			n.Status = domain.StatusError
			n.DiagCode = "UNIT_MISMATCH"
			n.DiagMsg = fmt.Sprintf("cannot interpret unit %q as temperature", unit)
		}
	default:
		n.Unit = unit
	}
	n.Value = v
	return n
}

func inferUnit(raw, hint string, canonical domain.Unit) domain.Unit {
	if h := strings.TrimSpace(hint); h != "" {
		return domain.Unit(h)
	}
	low := strings.ToLower(raw)
	switch canonical {
	case domain.UnitSeconds:
		switch {
		case strings.Contains(low, "min"):
			return domain.UnitMinutes
		case strings.Contains(low, "ms"):
			return "ms"
		}
		return domain.UnitSeconds
	case domain.UnitMG:
		switch {
		case strings.Contains(low, "mg"):
			return domain.UnitMG
		case strings.HasSuffix(low, "g"):
			return domain.UnitG
		case strings.Contains(low, "ug") || strings.Contains(low, "µg"):
			return domain.UnitUG
		}
		return domain.UnitMG
	case domain.UnitCelsius:
		switch {
		case strings.Contains(low, "k") && !strings.Contains(low, "c"):
			return domain.UnitKelvin
		}
		return domain.UnitCelsius
	}
	return canonical
}

func toSeconds(v float64, u domain.Unit) (float64, bool) {
	switch u {
	case domain.UnitSeconds:
		return v, true
	case domain.UnitMinutes:
		return v * 60, true
	case "ms":
		return v / 1000, true
	}
	return 0, false
}

func toMG(v float64, u domain.Unit) (float64, bool) {
	switch u {
	case domain.UnitMG:
		return v, true
	case domain.UnitG:
		return v * 1000, true
	case domain.UnitUG:
		return v / 1000, true
	}
	return 0, false
}

func toCelsius(v float64, u domain.Unit) (float64, bool) {
	switch u {
	case domain.UnitCelsius:
		return v, true
	case domain.UnitKelvin:
		return v - 273.15, true
	}
	return 0, false
}

// Fingerprint returns a stable content hash for a layer payload.
func Fingerprint(raw json.RawMessage) string {
	sum := sha256.Sum256(canonicalJSON(raw))
	return hex.EncodeToString(sum[:16])
}

func canonicalJSON(raw json.RawMessage) []byte {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	b, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return b
}
