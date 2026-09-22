package parse

import (
	"bufio"
	"fmt"
	"strings"

	"tgasys/internal/domain"
)

// The import format is a small, copy-pasteable sectioned text:
//
//	## samples
//	# t_s  mass_mg  sample_temp_C
//	0  10.000  25.1
//	## furnace
//	# start_s end_s rate_K/min target_C
//	...
//
// Unknown sections are retained verbatim so a half-failed import keeps the
// offending material with a diagnostic instead of aborting everything.
type ParsedImport struct {
	Sections []domain.Section
	Issues   []domain.Issue
}

// ImportText splits a payload into labeled sections and tokenized records.
func ImportText(raw string) ParsedImport {
	out := ParsedImport{}
	var cur *domain.Section
	sc := bufio.NewScanner(strings.NewReader(raw))
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := sc.Text()
		trim := strings.TrimSpace(line)
		switch {
		case trim == "":
			continue
		case strings.HasPrefix(trim, "##"):
			name := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(trim, "##")))
			kind, ok := resolveKind(name)
			if !ok {
				out.Issues = append(out.Issues, domain.Issue{
					Code: "import.unknown_section", Severity: "error",
					Message: fmt.Sprintf("unknown section %q retained verbatim", name),
				})
			}
			out.Sections = append(out.Sections, domain.Section{
				Kind: kind, Header: name, Raw: line + "\n",
			})
			cur = &out.Sections[len(out.Sections)-1]
		default:
			if cur == nil {
				out.Issues = append(out.Issues{
					Code: "import.content_before_section", Severity: "error",
					Message: "data appeared before any ## section: " + trim,
				})
				out.Sections = append(out.Sections, domain.Section{
					Kind: domain.KindUnknown, Header: "(orphan)", Raw: line + "\n",
				})
				cur = &out.Sections[len(out.Sections)-1]
				continue
			}
			cur.Raw += line + "\n"
			if strings.HasPrefix(strings.TrimSpace(line), "#") {
				continue
			}
			cur.Records = append(cur.Records, strings.Fields(line))
		}
	}
	return out
}

func resolveKind(name string) (string, bool) {
	switch name {
	case "samples", "sample", "tg", "tg_points":
		return domain.KindSamples, true
	case "drift", "drift_correction", "balance_drift":
		return domain.KindDrift, true
	case "furnace", "furnace_program", "program":
		return domain.KindFurnace, true
	case "gas", "gases", "gas_events", "valves", "atmosphere":
		return domain.KindGas, true
	case "blank", "blank_curve", "baseline":
		return domain.KindBlank, true
	case "initial", "initial_mass", "mass_initial":
		return domain.KindInitial, true
	case "meta", "metadata":
		return domain.KindMeta, true
	case "parameters", "params":
		return domain.KindParameter, true
	}
	return domain.KindUnknown, false
}
