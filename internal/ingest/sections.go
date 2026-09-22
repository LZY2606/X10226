package ingest

import (
	"strings"

	"tga-archive/internal/domain"
)

// Section is one named layer fragment of a bundled import.
type Section struct {
	Name string
	Body string
}

// SplitSections splits a bundled text on lines beginning with "[" / "#".
func SplitSections(text string) []Section {
	var out []Section
	var cur Section
	flush := func() {
		if cur.Name != "" || strings.TrimSpace(cur.Body) != "" {
			out = append(out, cur)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if len(trim) >= 1 && (trim[0] == '#' || trim[0] == '[') {
			flush()
			name := strings.Trim(trim, "#[] \t")
			// a heading is "<layer> <human comment>"; the layer name is the
			// first whitespace-delimited token (or bracketed token)
			if fields := strings.FieldsFunc(name, func(r rune) bool {
				switch r {
				case ' ', '\t', '—', '–', ':', '：', '\t':
					return true
				}
				return false
			}); len(fields) > 0 {
				name = fields[0]
			}
			cur = Section{Name: normalizeLayer(name)}
			continue
		}
		cur.Body += line + "\n"
	}
	flush()
	return out
}

func normalizeLayer(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	name = strings.ReplaceAll(name, " ", "_")
	switch name {
	case "samples", "sample", "raw_sample", "tg", "tg_data":
		return domain.LayerSamples
	case "drift", "drift_correction", "balance_drift", "baseline_drift":
		return domain.LayerDrift
	case "furnace", "furnace_program", "program", "temperature_program":
		return domain.LayerFurnace
	case "gas", "gas_valve", "gas_valve_events", "valves", "atmosphere":
		return domain.LayerGas
	case "blank", "blank_curve", "empty_pan":
		return domain.LayerBlank
	case "initial_mass", "initial", "start_mass", "m0":
		return domain.LayerInitialMass
	}
	return name
}
