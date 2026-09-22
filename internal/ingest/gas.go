package ingest

import (
	"strings"

	"tga-archive/internal/domain"
)

// ParseValves accepts lines:
//
//	10 min N2 50 mL/min
//	25 min air 50 mL/min switch to oxidizing
//
// Only the first three whitespace fields are structured; the rest is a note.
func ParseValves(text string) ([]domain.ValveEvent, []domain.Diagnostic, bool) {
	var valves []domain.ValveEvent
	var diags []domain.Diagnostic
	for _, rawLine := range strings.Split(text, "\n") {
		line := strings.TrimSpace(rawLine)
		if line == "" || strings.HasPrefix(line, "//") || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 3 {
			diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "bad_valve_line", Layer: domain.LayerGas, Fragment: line, Message: "expected '<time> <gas> <flow>'"})
			continue
		}
		tm := domain.ParseMeasure(fields[0]+" "+fields[1], domain.UnitSecond)
		if !tm.Valid {
			diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "unconvertible_valve_time", Layer: domain.LayerGas, Fragment: line, Message: tm.Note})
			continue
		}
		gas := fields[2]
		flow := domain.Measure{Valid: false}
		note := ""
		if len(fields) >= 4 {
			// flow may be "50" + unit (2 tokens) or "50mL/min" (1 token)
			if strings.ContainsAny(fields[3], "0123456789.") {
				flowTok := fields[3]
				if len(fields) >= 5 && looksLikeUnit(fields[4]) {
					flowTok += " " + fields[4]
					note = strings.Join(fields[5:], " ")
				} else {
					note = strings.Join(fields[4:], " ")
				}
				flow = domain.ParseMeasure(flowTok, domain.UnitMlPerMin)
				if !flow.Valid {
					diags = append(diags, domain.Diagnostic{Severity: domain.SevWarning, Code: "unconvertible_flow", Layer: domain.LayerGas, Fragment: line, Message: flow.Note})
				}
			} else {
				note = strings.Join(fields[3:], " ")
			}
		}
		valves = append(valves, domain.ValveEvent{Time: tm, Gas: gas, Flow: flow, Note: note})
	}
	if len(valves) == 0 {
		diags = append(diags, domain.Diagnostic{Severity: domain.SevError, Code: "no_valves", Layer: domain.LayerGas, Message: "no valve events parsed"})
		return nil, diags, false
	}
	return valves, diags, true
}

func looksLikeUnit(s string) bool {
	for _, ch := range s {
		if ch >= '0' && ch <= '9' {
			return false
		}
	}
	return true
}
