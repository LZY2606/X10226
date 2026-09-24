package tgaio

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/example/tga/internal/domain"
)

// knownUnitCanonical maps accepted unit spellings to canonical forms.
var knownUnitCanonical = map[string]string{
	"s": "s", "sec": "s", "secs": "s", "seconds": "s",
	"c": "C", "°c": "C", "celsius": "C", "degc": "C",
	"mg": "mg", "milligram": "mg", "milligrams": "mg", "milligramme": "mg",
	"g": "g", "kg": "kg",
	"%": "%", "pct": "%", "percent": "%",
	"%/s": "%/s", "%/sec": "%/s", "pct/s": "%/s",
}

// ParseQuantity preserves the raw token. A decimal comma is tolerated
// ("10,2 mg"), the unit is canonicalized, and failure is reported on the
// returned Quantity instead of dropping the value.
func ParseQuantity(raw, expectUnit string) domain.Quantity {
	q := domain.Quantity{Raw: raw, Unit: expectUnit}
	token := strings.TrimSpace(raw)
	if token == "" {
		q.ParseError = "empty value"
		return q
	}
	// Split numeric prefix from unit suffix on the first letter, degree
	// sign or percent rune.
	i := 0
	for i < len(token) {
		r, size := utf8.DecodeRuneInString(token[i:])
		if unicode.IsLetter(r) || r == '°' || r == '%' {
			break
		}
		i += size
	}
	numPart := strings.TrimSpace(token[:i])
	unitPart := strings.TrimSpace(token[i:])
	if numPart == "" {
		q.ParseError = fmt.Sprintf("no numeric prefix in %q", raw)
		return q
	}
	normalized := strings.ReplaceAll(numPart, ",", ".")
	// Reject ambiguous thousands separators like "1.012,3" already handled:
	// if both appear, treat '.' as thousands when comma is decimal.
	if strings.Contains(normalized, ".") && strings.Contains(numPart, ",") {
		// e.g. 1.012,3 -> remove dots, comma already became dot
		normalized = strings.ReplaceAll(numPart, ".", "")
		normalized = strings.ReplaceAll(normalized, ",", ".")
	}
	v, err := strconv.ParseFloat(normalized, 64)
	if err != nil {
		q.ParseError = fmt.Sprintf("cannot parse number %q", numPart)
		return q
	}
	q.Value = v
	if unitPart != "" {
		canon, ok := knownUnitCanonical[strings.ToLower(unitPart)]
		if !ok {
			q.ParseError = fmt.Sprintf("unknown unit %q", unitPart)
			return q
		}
		q.Unit = canon
		if expectUnit != "" && canon != expectUnit {
			q.ParseError = fmt.Sprintf("unit mismatch: got %s want %s", canon, expectUnit)
			return q
		}
	}
	return q
}
