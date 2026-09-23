package ingest

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"tgasys/internal/domain"
)

var numRe = regexp.MustCompile(`^[+-]?(\d+(\.\d*)?|\.\d+)([eE][+-]?\d+)?$`)

// splitToken splits "12.34 mg" / "300C" / "12.3" into number and unit.
func splitToken(tok string) (string, string) {
	tok = strings.TrimSpace(tok)
	i := 0
	for i < len(tok) {
		c := tok[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '+' || c == '-' || c == 'e' || c == 'E' {
			i++
			continue
		}
		break
	}
	num := strings.TrimSpace(tok[:i])
	rest := strings.TrimSpace(tok[i:])
	return num, rest
}

type unitConv struct {
	canonical string
	factor    float64 // multiply source value to get canonical
	offset    float64 // after factor, add offset (temperature scales)
}

// All domain quantities carry a coordinate reference. Canonical bases:
// time=s, temperature=degC, mass=mg, rate=C/min.
var unitTable = map[string]unitConv{
	"s":  {"s", 1, 0},
	"sec": {"s", 1, 0},
	"min": {"s", 60, 0},
	"m":   {"s", 60, 0},
	"h":   {"s", 3600, 0},
	"mg":  {"mg", 1, 0},
	"g":   {"mg", 1000, 0},
	"kg":  {"mg", 1_000_000, 0},
	"ug":  {"mg", 0.001, 0},
	"µg":  {"mg", 0.001, 0},
	"c":   {"C", 1, 0},
	"°c":  {"C", 1, 0},
	"oc":  {"C", 1, 0},
	"k":   {"C", 1, -273.15},
	"f":   {"C", 5.0 / 9.0, -32 * 5.0 / 9.0},
	"c/min":  {"C/min", 1, 0},
	"°c/min": {"C/min", 1, 0},
	"k/min":  {"C/min", 1, 0},
	"%":    {"%", 1, 0},
	"pct":  {"%", 1, 0},
	"1/s":  {"1/s", 1, 0},
	"mg/s": {"mg/s", 1, 0},
	"":     {"", 1, 0},
}

func normUnit(u string) string {
	return strings.ToLower(strings.TrimSpace(u))
}

// parseQuantity parses one cell. explicitUnit (from the column header or
// per-field unit column) is used when the cell has no unit suffix.
func parseQuantity(rawTok, explicitUnit, canonicalWanted string) domain.Quantity {
	tok := strings.TrimSpace(rawTok)
	q := domain.Quantity{Original: tok}
	if tok == "" {
		return q
	}
	numTok, cellUnit := splitToken(tok)
	srcUnit := cellUnit
	if srcUnit == "" {
		srcUnit = explicitUnit
	}
	q.SourceUnit = srcUnit
	if !numRe.MatchString(numTok) {
		return q // unconvertible: value stays nil, original kept; caller attaches diag
	}
	v, err := strconv.ParseFloat(numTok, 64)
	if err != nil || math.IsNaN(v) {
		return q
	}
	conv, ok := unitTable[normUnit(srcUnit)]
	if !ok {
		return q
	}
	if canonicalWanted != "" && conv.canonical != canonicalWanted {
		// keep original; let caller attach a dimensional diagnostic
		return q
	}
	cv := v*conv.factor + conv.offset
	q.Value = &cv
	q.Unit = conv.canonical
	return q
}

// diag helper to keep code readable in parser.
func errDiag(layer, field string, row int, code, msg string) domain.Diagnostic {
	return domain.Diagnostic{Code: code, Message: msg, Layer: layer, Row: row, Field: field, Severity: "error", Accepted: false}
}

func warnDiag(layer, field string, row int, code, msg string) domain.Diagnostic {
	return domain.Diagnostic{Code: code, Message: msg, Layer: layer, Row: row, Field: field, Severity: "warning", Accepted: true}
}

func formatFloat(v float64) string { return fmt.Sprintf("%.6g", v) }
