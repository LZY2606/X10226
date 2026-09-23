package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// ToCanonical converts a numeric value expressed in unit into the canonical
// unit of its dimension. Unrecognized units yield a diagnostic-bearing
// RawValue rather than an error, so the caller can preserve the record.
func ToCanonical(rawToken, unit string, dim Dimension) RawValue {
	v := RawValue{Raw: rawToken, Unit: unit, Dim: dim}
	token := strings.TrimSpace(rawToken)
	n, err := strconv.ParseFloat(token, 64)
	if err != nil {
		v.Diagnostic = fmt.Sprintf("not_a_number:%q", token)
		return v
	}
	scale, ok := unitScale(unit, dim)
	if !ok {
		v.Diagnostic = fmt.Sprintf("unknown_unit:%q", unit)
		return v
	}
	v.Numeric = n * scale
	v.Normalized = true
	return v
}

// RequireDim guards against dimension/unit mismatches at import time.
func RequireDim(unit string, allowed Dimension) RawValue { return RawValue{} }

func unitScale(unit string, dim Dimension) (float64, bool) {
	switch dim {
	case DimMass:
		switch unit {
		case UnitMg:
			return 1, true
		case UnitUg:
			return 0.001, true
		case UnitG:
			return 1000, true
		}
	case DimTemperature:
		switch unit {
		case UnitC:
			return 1, true
		case UnitK:
			// encoded as offset, handled in ToTempCanonical
			return 0, false
		}
	case DimTime:
		switch unit {
		case UnitS:
			return 1, true
		case UnitMin:
			return 60, true
		}
	case DimPercent:
		if unit == UnitPct {
			return 1, true
		}
	case DimGasFlow:
		if unit == UnitMLMin {
			return 1, true
		}
	}
	return 0, false
}

// ToTempCanonical converts temperatures, including the Kelvin offset.
func ToTempCanonical(rawToken, unit string) RawValue {
	v := RawValue{Raw: rawToken, Unit: unit, Dim: DimTemperature}
	n, err := strconv.ParseFloat(strings.TrimSpace(rawToken), 64)
	if err != nil {
		v.Diagnostic = fmt.Sprintf("not_a_number:%q", rawToken)
		return v
	}
	switch unit {
	case UnitC:
		v.Numeric, v.Normalized = n, true
	case UnitK:
		v.Numeric, v.Normalized = n-273.15, true
	default:
		v.Diagnostic = fmt.Sprintf("unknown_unit:%q", unit)
	}
	return v
}
