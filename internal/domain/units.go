package domain

import (
	"math"
	"strconv"
	"strings"
)

// Canonical units used by every derived layer.
const (
	UnitSecond = "s"
	UnitKelvin = "K"
	UnitMg     = "mg"
	UnitMgPerS = "mg/s"
	UnitKPerS  = "K/s"
	UnitMlPerMin = "mL/min"
)

// ParseMeasure converts "value unit" into canonical measure, keeping the
// original representation. Unknown units or unparsable numbers produce an
// invalid measure that callers must keep in place with a diagnostic.
func ParseMeasure(token, canonical string) Measure {
	token = strings.TrimSpace(token)
	raw := token
	i := strings.LastIndexAny(token, " \t")
	var num, unit string
	if i < 0 {
		num, unit = token, ""
	} else {
		num, unit = strings.TrimSpace(token[:i]), strings.TrimSpace(token[i+1:])
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return invalidMeasure(raw, unit, "cannot parse number: "+num)
	}
	cv, cu, ok := convert(f, unit, canonical)
	if !ok {
		return invalidMeasure(raw, unit, "cannot convert unit "+strconv.Quote(unit)+" to "+canonical)
	}
	if math.IsNaN(cv) || math.IsInf(cv, 0) {
		return invalidMeasure(raw, unit, "non-finite converted value")
	}
	return Measure{Value: cv, Unit: cu, Raw: raw, RawUnit: unit, Valid: true}
}

func convert(v float64, from, canonical string) (float64, string, bool) {
	if from == "" {
		from = canonical // unitless tokens are assumed canonical
	}
	switch canonical {
	case UnitSecond:
		switch from {
		case "s", "sec", "secs":
			return v, UnitSecond, true
		case "min", "mins", "minute", "minutes":
			return v * 60, UnitSecond, true
		case "h", "hr", "hour", "hours":
			return v * 3600, UnitSecond, true
		case "ms":
			return v / 1000, UnitSecond, true
		}
	case UnitKelvin:
		switch from {
		case "K":
			return v, UnitKelvin, true
		case "C", "°C", "degC", "oC":
			return v + 273.15, UnitKelvin, true
		case "F", "°F":
			return (v-32)*5/9 + 273.15, UnitKelvin, true
		}
	case UnitMg:
		switch from {
		case "mg":
			return v, UnitMg, true
		case "g":
			return v * 1000, UnitMg, true
		case "ug", "µg":
			return v / 1000, UnitMg, true
		}
	case UnitMgPerS:
		switch from {
		case "mg/s", "mgs-1":
			return v, UnitMgPerS, true
		case "mg/min":
			return v / 60, UnitMgPerS, true
		case "ug/s":
			return v / 1000, UnitMgPerS, true
		}
	case UnitKPerS:
		switch from {
		case "K/s", "C/min", "°C/min", "K/min":
			if strings.HasSuffix(from, "/min") {
				return v / 60, UnitKPerS, true
			}
			return v, UnitKPerS, true
		}
	case UnitMlPerMin:
		switch from {
		case "mL/min", "ml/min", "sccm":
			return v, UnitMlPerMin, true
		case "L/min":
			return v * 1000, UnitMlPerMin, true
		}
	}
	return 0, "", false
}

// FormatMeasure renders a canonical measure with its canonical unit while the
// raw representation stays available separately.
func FormatMeasure(m Measure, digits int) string {
	if !m.Valid {
		return m.Raw + " [!]"
	}
	return strconv.FormatFloat(round(m.Value, digits), 'f', digits, 64) + " " + m.Unit
}

func round(v float64, digits int) float64 {
	p := math.Pow(10, float64(digits))
	return math.Round(v*p) / p
}
