package ingest

import (
	"strings"

	"tga-archive/internal/domain"
)

// ParseInitialMass accepts:
//
//	mass: 10.000 mg
//	at: 0 s
//	basis: tared balance reading before furnace start
func ParseInitialMass(text string) (*domain.InitialMass, []domain.Diagnostic, bool) {
	var diags []domain.Diagnostic
	im := &domain.InitialMass{}
	haveMass, haveAt := false, false
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := cutColon(line)
		if !ok {
			diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "bad_key_value", Layer: domain.LayerInitialMass, Fragment: line, Message: "expected key: value"})
			continue
		}
		switch strings.ToLower(key) {
		case "mass", "m0":
			m := domain.ParseMeasure(val, domain.UnitMg)
			if !m.Valid {
				diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "unconvertible_mass", Layer: domain.LayerInitialMass, Fragment: line, Message: m.Note})
				continue
			}
			im.Mass = m
			haveMass = true
		case "at", "time":
			m := domain.ParseMeasure(val, domain.UnitSecond)
			if !m.Valid {
				diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "unconvertible_at", Layer: domain.LayerInitialMass, Fragment: line, Message: m.Note})
				continue
			}
			im.At = m
			haveAt = true
		case "basis", "note":
			im.Basis = strings.TrimSpace(val)
		case "replaces":
			im.Replaces = strings.TrimSpace(val)
		default:
			diags = append(diags, domain.Diagnostic{Severity: domain.SevWarning, Code: "unknown_key", Layer: domain.LayerInitialMass, Fragment: line, Message: "ignored key " + key})
		}
	}
	if !haveMass {
		diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "missing_mass", Layer: domain.LayerInitialMass, Message: "initial mass requires mass"})
		return nil, diags, false
	}
	if !haveAt {
		im.At = domain.Measure{Value: 0, Unit: domain.UnitSecond, Raw: "0 s", Valid: true}
	}
	return im, diags, true
}

func cutColon(s string) (string, string, bool) {
	i := strings.Index(s, ":")
	if i < 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+1:]), true
}
