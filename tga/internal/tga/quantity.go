package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
)

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:6])
}

// parseQ converts an original token + declared unit into a canonical quantity.
// Canonical bases: mass -> mg, time -> s, temperature -> degC, mass-loss
// rate -> mg/min. Values that cannot be converted are retained with
// Valid=false and a diagnostic; they are never silently dropped.
func parseQ(raw, unit string) Q {
	q := Q{Raw: strings.TrimSpace(raw), Unit: strings.TrimSpace(unit)}
	tok := q.Raw
	if tok == "" {
		q.Valid = false
		q.Diagnostic = "empty value"
		return q
	}
	v, err := strconv.ParseFloat(tok, 64)
	if err != nil {
		q.Valid = false
		q.Diagnostic = "not a number: " + err.Error()
		return q
	}
	conv, canon, ok := canonicalise(v, q.Unit)
	if !ok {
		q.Valid = false
		q.Diagnostic = "unknown or incompatible unit: " + q.Unit
		return q
	}
	q.Value = conv
	q.Canon = canon
	q.Valid = true
	return q
}

func canonicalise(v float64, unit string) (float64, string, bool) {
	switch strings.ToLower(unit) {
	case "mg":
		return v, "mg", true
	case "g":
		return v * 1000, "mg", true
	case "ug", "µg":
		return v / 1000, "mg", true
	case "s", "sec", "secs", "second", "seconds":
		return v, "s", true
	case "min", "minute", "minutes":
	return v * 60, "s", true
	case "h", "hr", "hour", "hours":
		return v * 3600, "s", true
	case "c", "degc", "°c", "celsius":
		return v, "degC", true
	case "k", "kelvin":
		return v - 273.15, "degC", true
	case "%", "pct", "percent":
		return v, "%", true
	case "%/min", "pct/min":
		return v, "%/min", true
	case "ug/min", "µg/min":
		return v / 1000, "mg/min", true
	case "mg/min":
		return v, "mg/min", true
	default:
		return 0, "", false
	}
}

// FmtQ renders a canonical value back with unit.
func FmtQ(q Q) string {
	if !q.Valid {
		return q.Raw + " " + q.Unit + " (unconverted)"
	}
	return strconv.FormatFloat(q.Value, 'f', -1, 64) + " " + q.Canon
}
