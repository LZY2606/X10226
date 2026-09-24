package tgaio

import (
	"fmt"
	"strings"

	"github.com/example/tga/internal/domain"
)

// MediaType is the domain-specific, copy-pasteable TGA dossier format.
const MediaType = "text/x-tga-dossier"

// FileResult is the parsed content of one dossier file. Parse errors never
// abort the whole file: good sections survive bad rows and are returned
// together with diagnostics so an import can commit partially.
type FileResult struct {
	Sections    []domain.Section
	RawCells    []domain.RawCell
	Diagnostics []domain.Diagnostic
}

// Partial reports whether any error-level diagnostic was produced.
func (f *FileResult) Partial() bool {
	for _, d := range f.Diagnostics {
		if d.Severity == "error" {
			return true
		}
	}
	return false
}

type sectionSpec struct {
	kind    string
	headers []string
	units   map[string]string // expected canonical unit per numeric column
	tokens  map[string]bool   // columns kept verbatim (no unit)
}

var sectionSpecs = map[string]sectionSpec{
	"sample": {
		kind:    "sample",
		headers: []string{"time_s", "sample_temp_c", "mass_mg"},
		units:   map[string]string{"time_s": "s", "sample_temp_c": "C", "mass_mg": "mg"},
	},
	"furnace": {
		kind:    "furnace",
		headers: []string{"time_s", "setpoint_c", "segment"},
		units:   map[string]string{"time_s": "s", "setpoint_c": "C"},
		tokens:  map[string]bool{"segment": true},
	},
	"gas": {
		kind:    "gas",
		headers: []string{"valve_on_s", "valve_off_s", "gas"},
		units:   map[string]string{"valve_on_s": "s", "valve_off_s": "s"},
		tokens:  map[string]bool{"gas": true},
	},
	"blank": {
		kind:    "blank",
		headers: []string{"time_s", "blank_mass_mg"},
		units:   map[string]string{"time_s": "s", "blank_mass_mg": "mg"},
	},
	"initial": {
		kind:    "initial",
		headers: []string{"sample_id", "initial_mass_mg", "note"},
		units:   map[string]string{"initial_mass_mg": "mg"},
		tokens:  map[string]bool{"sample_id": true, "note": true},
	},
}

// Parse parses a TGA dossier. Unknown sections are retained verbatim with a
// warning; malformed rows are retained and marked bad.
func Parse(text string) *FileResult {
	res := &FileResult{}
	lines := strings.Split(text, "\n")
	var cur *domain.Section
	var spec sectionSpec
	flush := func(endLine int) {
		if cur != nil {
			cur.LineEnd = endLine
			res.Sections = append(res.Sections, *cur)
			cur = nil
		}
	}
	for idx, raw := range lines {
		lineNo := idx + 1
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") || trimmed == "---" {
			continue
		}
		if strings.HasPrefix(trimmed, "@") {
			flush(lineNo)
			name := strings.TrimSpace(strings.TrimPrefix(trimmed, "@"))
			name = strings.ToLower(strings.Fields(name)[0])
			s, known := sectionSpecs[name]
			cur = &domain.Section{
				Kind:      name,
				LineStart: lineNo,
				LineEnd:   lineNo,
				RawText:   "",
			}
			if known {
				spec = s
				cur.Headers = append([]string(nil), s.headers...)
			} else {
				spec = sectionSpec{}
				cur.Headers = nil
				res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
					Severity: "warning", Section: name, Line: lineNo,
					Code:    "unknown_section",
					Message: fmt.Sprintf("unknown section @%s retained verbatim", name),
				})
			}
			continue
		}
		if cur == nil {
			res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
				Severity: "error", Line: lineNo, Code: "orphan_row",
				Message: "data row outside of any @section",
			})
			continue
		}
		cur.RawText += line + "\n"
		fields := strings.Fields(trimmed)
		rowIndex := len(cur.Rows)
		row := domain.BatchRow{
			Row:    rowIndex,
			Line:   lineNo,
			Fields: map[string]domain.Quantity{},
			Tokens: map[string]string{},
		}
		if len(fields) > 0 && strings.EqualFold(fields[0], "header") {
			continue // header marker line, no data
		}
		if spec.kind == "" {
			row.Trailing = fields
			cur.Rows = append(cur.Rows, row)
			continue
		}
		if len(fields) < len(spec.headers) {
			row.Bad = true
			res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
				Severity: "error", Section: spec.kind, Line: lineNo, Row: rowIndex,
				Code:    "too_few_columns",
				Message: fmt.Sprintf("got %d fields, want %d", len(fields), len(spec.headers)),
			})
		}
		for colIdx, h := range spec.headers {
			var token string
			if colIdx < len(fields) {
				token = fields[colIdx]
			} else {
				token = ""
			}
			res.RawCells = append(res.RawCells, domain.RawCell{
				Section: spec.kind, Row: rowIndex, Column: h, Raw: token, Line: lineNo,
			})
			if spec.tokens[h] {
				row.Tokens[h] = token
				continue
			}
			q := ParseQuantity(token, spec.units[h])
			if q.ParseError != "" {
				row.Bad = true
				q.Unit = spec.units[h]
				code := "unconvertible_value"
				if token == "" {
					code = "missing_value"
				}
				res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
					Severity: "error", Section: spec.kind, Line: lineNo, Row: rowIndex,
					Code:    code,
					Message: fmt.Sprintf("column %s: %s", h, q.ParseError),
				})
			}
			row.Fields[h] = q
		}
		if len(fields) > len(spec.headers) {
			row.Trailing = fields[len(spec.headers):]
			res.Diagnostics = append(res.Diagnostics, domain.Diagnostic{
				Severity: "warning", Section: spec.kind, Line: lineNo, Row: rowIndex,
				Code:    "extra_columns",
				Message: fmt.Sprintf("%d trailing field(s) retained", len(row.Trailing)),
			})
		}
		cur.Rows = append(cur.Rows, row)
	}
	flush(len(lines))
	// Copy section diagnostics up to file level.
	for _, s := range res.Sections {
		for _, d := range s.Diagnostics {
			res.Diagnostics = append(res.Diagnostics, d)
		}
	}
	return res
}
