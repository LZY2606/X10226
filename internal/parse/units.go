package parse

import (
	"math"
	"regexp"
	"strconv"
	"strings"

	"tga/internal/domain"
)

// toCanon converts (value,unit) to the canonical unit of dimension dim:
// time->s, temperature->degC, mass->mg, rate->ug/min, flow->mL/min.
// ok=false means the representation cannot be converted; the caller keeps the
// raw representation in place and attaches a diagnostic instead of dropping it.
func toCanon(v float64, unit, dim string) (float64, string, bool) {
	u := normUnit(unit)
	def, ok := unitTable[u]
	if !ok || def.dim != dim {
		return 0, "", false
	}
	return v * def.f, canonUnit[dim], true
}

type unitDef struct {
	dim string
	f   float64 // multiply to canonical
}

var canonUnit = map[string]string{
	"time": "s", "temp": "degC", "mass": "mg",
	"rate": "ug/min", "flow": "mL/min",
}

func normUnit(u string) string {
	u = strings.TrimSpace(u)
	u = strings.ReplaceAll(u, "μ", "u")
	u = strings.ReplaceAll(u, "µ", "u")
	u = strings.ReplaceAll(u, " ", "")
	u = strings.ToLower(u)
	u = strings.ReplaceAll(u, "°", "")
	u = strings.TrimSuffix(u, "⁻¹")
	u = strings.ReplaceAll(u, "min^-1", "/min")
	u = strings.ReplaceAll(u, "/min", "/min")
	return u
}

var unitTable = map[string]unitDef{
	// time -> seconds
	"s": {"time", 1}, "sec": {"time", 1}, "second": {"time", 1}, "seconds": {"time", 1},
	"min": {"time", 60}, "minute": {"time", 60}, "minutes": {"time", 60},
	"h": {"time", 3600}, "hr": {"time", 3600}, "hour": {"time", 3600}, "hours": {"time", 3600},
	// temperature -> degC (offset units handled separately)
	"c": {"temp", 1}, "degc": {"temp", 1}, "oc": {"temp", 1}, "celsius": {"temp", 1},
	"k": {"temp", 0}, // special-cased below
	"f": {"temp", 0}, // special-cased below
	// mass -> milligrams
	"mg":    {"mass", 1},
	"ug":    {"mass", 0.001},
	"g":     {"mass", 1000},
	"kg":    {"mass", 1e6},
	"ug/mg": {"", 0},
	// mass/time drift rates -> ug/min
	"ug/min": {"rate", 1},
	"mg/min": {"rate", 1000},
	"ug/h":   {"rate", 1.0 / 60},
	"mg/h":   {"rate", 1000.0 / 60},
	"ug/s":   {"rate", 60},
	// flow -> mL/min
	"ml/min": {"flow", 1},
	"ml/s":   {"flow", 60},
	"l/min":  {"flow", 1000},
	"sccm":   {"flow", 1},
}

func convert(v float64, rawUnit, dim string) (float64, string, bool) {
	u := normUnit(rawUnit)
	if u == "k" && dim == "temp" {
		return v - 273.15, canonUnit[dim], true
	}
	if u == "f" && dim == "temp" {
		return (v - 32) * 5 / 9, canonUnit[dim], true
	}
	return toCanon(v, rawUnit, dim)
}

var numRe = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

// parseValue keeps the exact raw text; on failure it returns a Value with only
// Raw populated so no representation is lost for plotting convenience.
func parseValue(raw, dim string, diag func(string)) domain.Value {
	raw = strings.TrimSpace(raw)
	val := domain.Value{Raw: raw}
	if raw == "" {
		if diag != nil {
			diag("missing value")
		}
		return val
	}
	numPart, unitPart := splitNumberUnit(raw)
	n, err := strconv.ParseFloat(numPart, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		if diag != nil {
			diag("not a number: " + numPart)
		}
		return val
	}
	cv, cu, ok := convert(n, unitPart, dim)
	if !ok {
		if diag != nil {
			diag("unconvertible unit '" + unitPart + "' for " + dim)
		}
		return val
	}
	val.Value = cv
	val.Unit = cu
	return val
}

func splitNumberUnit(raw string) (string, string) {
	raw = strings.TrimSpace(raw)
	i := 0
	for i < len(raw) {
		c := raw[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-' || c == 'e' || c == 'E' {
			i++
			continue
		}
		break
	}
	return raw[:i], strings.TrimSpace(raw[i:])
}

func valid(v domain.Value) bool { return v.Unit != "" }
