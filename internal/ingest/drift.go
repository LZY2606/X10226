package ingest

import (
	"bufio"
	"strings"

	"tga-archive/internal/domain"
)

// ParseDrift accepts:
//
//	rate: 0.0004 mg/s
//	anchor_at: 0 s
//
// or a "points" table:
//
//	at offset
//	0 s  0 mg
func ParseDrift(text string) (*domain.DriftSpec, []domain.Diagnostic, bool) {
	var diags []domain.Diagnostic
	spec := &domain.DriftSpec{}
	r := bufio.NewScanner(strings.NewReader(text))
	haveRate, haveAnchor := false, false
	for r.Scan() {
		line := strings.TrimSpace(r.Text())
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "bad_key_value", Layer: domain.LayerDrift, Fragment: line, Message: "expected key: value"})
			continue
		}
		switch strings.ToLower(strings.TrimSpace(key)) {
		case "rate", "drift_rate":
			m := domain.ParseMeasure(val, domain.UnitMgPerS)
			if !m.Valid {
				diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "unconvertible_rate", Layer: domain.LayerDrift, Fragment: line, Message: m.Note})
				continue
			}
			spec.Rate = m
			haveRate = true
		case "anchor_at", "anchor":
			m := domain.ParseMeasure(val, domain.UnitSecond)
			if !m.Valid {
				diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "unconvertible_anchor", Layer: domain.LayerDrift, Fragment: line, Message: m.Note})
				continue
			}
			spec.AnchorAt = m
			haveAnchor = true
		case "note":
			spec.Note = strings.TrimSpace(val)
		default:
			diags = append(diags, domain.Diagnostic{Severity: domain.SevWarning, Code: "unknown_key", Layer: domain.LayerDrift, Fragment: line, Message: "ignored key " + key})
		}
	}
	if !haveRate {
		diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "missing_rate", Layer: domain.LayerDrift, Message: "drift correction requires a rate"})
	}
	if !haveAnchor {
		spec.AnchorAt = domain.Measure{Value: 0, Unit: domain.UnitSecond, Raw: "0 s", Valid: true}
	}
	accepted := haveRate
	if !accepted {
		return nil, diags, false
	}
	return spec, diags, true
}
