// Package parse turns the layered, copy-pasteable TGA case text into typed
// source records. The original textual representation is always retained; only
// the derived layer normalizes units. Values that cannot be converted stay in
// place with a diagnostic and are never silently dropped.
package parse

import (
	"bufio"
	"fmt"
	"strconv"
	"strings"

	"tga-archive/internal/domain"
)

// Bundle is the typed result of one import text.
type Bundle struct {
	Samples     []domain.RawPoint
	Drift       []domain.DriftPoint
	Program     []domain.ProgramStep
	GasEvents   []domain.GasEvent
	Blank       []domain.BlankPoint
	InitialMass *domain.InitialMass
	Meta        map[string]string

	// AcceptedSections records which sections parsed without any error.
	AcceptedSections []string
	// Diagnostics are section-scoped parse diagnostics. A parse error in one
	// section never aborts the other sections (import half-failure).
	Diagnostics []domain.Diagnostic
}

// Section names recognized in the import format.
var sectionNames = map[string]bool{
	"meta": true, "initial_mass": true, "furnace_program": true,
	"gas_events": true, "balance_drift": true, "blank_curve": true,
	"samples": true,
}

// Parse reads the layered TGA text format:
//
//	## meta
//	key=value
//	## initial_mass
//	mass=10.250 mg
//	## furnace_program
//	start=0 s; from=25 C; to=120 C; rate=10 C/min
//	## gas_events
//	at=600 s; N2 -> CO2
//	## balance_drift
//	t s, dm mg
//	0, 0.000
//	## blank_curve
//	t s, m mg
//	0, 0.000
//	## samples
//	t s, T C, m mg
//	0, 25.0, 10.250
func Parse(text string) Bundle {
	b := Bundle{Meta: map[string]string{}}
	var section string
	var headerCols []string
	seen := map[string]bool{}

	scanner := bufio.NewScanner(strings.NewReader(text))
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	lineNo := 0
	sectionErr := map[string]bool{}

	flush := func() {
		if section != "" && !sectionErr[section] {
			b.AcceptedSections = append(b.AcceptedSections, section)
		}
	}

	for scanner.Scan() {
		lineNo++
		raw := strings.TrimSpace(scanner.Text())
		if raw == "" {
			continue
		}
		if strings.HasPrefix(raw, "#") {
			flush()
			name := strings.Trim(strings.TrimLeft(raw, "#"), " \t")
			key := canonicalSection(name)
			if !sectionNames[key] {
				b.addErr(lineNo, "unknown_section", "unknown section header: "+name)
				section = ""
				continue
			}
			if seen[key] {
				b.addErr(lineNo, "duplicate_section", "section appears twice: "+key)
			}
			seen[key] = true
			section = key
			headerCols = nil
			continue
		}
		if section == "" {
			b.addErr(lineNo, "outside_section", "data outside any section: "+raw)
			continue
		}
		if err := b.parseLine(section, lineNo, raw, &headerCols); err != nil {
			sectionErr[section] = true
			b.addErr(lineNo, "section:"+section, err.Error())
		}
	}
	flush()
	return b
}

func canonicalSection(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

func (b *Bundle) addErr(line int, code, msg string) {
	b.Diagnostics = append(b.Diagnostics, domain.Diagnostic{
		Code: code, Severity: "error",
		Message: fmt.Sprintf("line %d: %s", line, msg),
		Ref:     fmt.Sprintf("L%d", line),
	})
}

func (b *Bundle) parseLine(section string, line int, raw string, headerCols *[]string) error {
	switch section {
	case "meta":
		eq := strings.IndexByte(raw, '=')
		if eq < 0 {
			return fmt.Errorf("meta line must be key=value: %q", raw)
		}
		b.Meta[strings.TrimSpace(raw[:eq])] = strings.TrimSpace(raw[eq+1:])
		return nil
	case "initial_mass":
		return b.parseInitialMass(line, raw)
	case "furnace_program":
		return b.parseProgram(line, raw)
	case "gas_events":
		return b.parseGas(line, raw)
	case "balance_drift":
		if isHeader(raw) {
			*headerCols = splitHeader(raw)
			return nil
		}
		return b.parseDrift(line, raw)
	case "blank_curve":
		if isHeader(raw) {
			*headerCols = splitHeader(raw)
			return nil
		}
		return b.parseBlank(line, raw)
	case "samples":
		if isHeader(raw) {
			*headerCols = splitHeader(raw)
			return nil
		}
		return b.parseSample(line, raw)
	}
	return nil
}

func isHeader(raw string) bool {
	// Headers contain unit annotations such as "t s, T C, m mg" and no digits
	// in the first comma field.
	first := strings.SplitN(raw, ",", 2)[0]
	return strings.ContainsAny(first, " \t") && !containsDigit(first)
}
func containsDigit(s string) bool {
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}
func splitHeader(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func fieldsN(raw string, n int) ([]string, error) {
	parts := strings.Split(raw, ",")
	if len(parts) != n {
		return nil, fmt.Errorf("expected %d comma fields, got %d: %q", n, len(parts), raw)
	}
	out := make([]string, n)
	for i := range parts {
		out[i] = strings.TrimSpace(parts[i])
	}
	return out, nil
}

// parseFloatToken accepts "10.250 mg", "300°C", plain numbers and also invalid
// tokens such as "NA mg": the latter yields a diagnostic-bearing unknown Q.
type parsed struct {
	q  domain.Q
	ok bool
}

func parseQ(raw, defaultUnit string, line int, code string) parsed {
	orig := raw
	raw = strings.TrimSpace(raw)
	num, unit := splitNumUnit(raw)
	if unit == "" {
		unit = defaultUnit
	}
	if num == "" {
		return parsed{q: domain.Q{Unit: unit, Original: orig}, ok: false}
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return parsed{q: domain.Q{Unit: unit, Original: orig}, ok: false}
	}
	nv, nu, conv := normalize(v, unit)
	if !conv {
		return parsed{q: domain.Q{Value: v, Unit: unit, Original: orig}, ok: false}
	}
	return parsed{q: domain.Q{Value: nv, Unit: nu, Basis: basisFor(nu), Original: orig}, ok: true}
}

func splitNumUnit(s string) (string, string) {
	s = strings.TrimSpace(s)
	i := 0
	for i < len(s) {
		c := s[i]
		if (c >= '0' && c <= '9') || c == '.' || c == '-' || c == '+' || c == 'e' || c == 'E' {
			i++
			continue
		}
		break
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i:])
}
