package ingest

import (
	"fmt"
	"strconv"
	"strings"

	"tgalineage/internal/model"
)

// SectionResult is the outcome of parsing one section.
type SectionResult struct {
	Kind        string
	Valid       bool
	Diagnostics []model.Diagnostic
	Parsed      any
}

// BatchResult is the outcome of one import: per-section fate, so a malformed
// section never aborts the whole import (half-failure is first-class).
type BatchResult struct {
	Sections map[string]SectionResult
}

var sectionNames = map[string]string{
	"points": model.KindPoints, "point": model.KindPoints, "samples": model.KindPoints,
	"drift": model.KindDrift, "drift anchors": model.KindDrift,
	"blank": model.KindBlank, "blank curve": model.KindBlank,
	"furnace": model.KindFurnace, "furnace program": model.KindFurnace,
	"gas": model.KindGas, "gas events": model.KindGas, "valves": model.KindGas,
	"mass": model.KindMass, "initial mass": model.KindMass,
	"params": model.KindParams, "parameters": model.KindParams,
}

// ParseBatch splits a tga-section/v1 document and parses every section.
func ParseBatch(text string) BatchResult {
	res := BatchResult{Sections: map[string]SectionResult{}}
	type block struct {
		name string
		body []string
		hdr  int
	}
	var blocks []block
	var cur *block
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "##") {
			name := strings.TrimSpace(strings.TrimPrefix(line, "##"))
			blocks = append(blocks, block{name: name, hdr: i + 1})
			cur = &blocks[len(blocks)-1]
			continue
		}
		if cur != nil {
			cur.body = append(cur.body, line)
		}
	}
	seen := map[string]bool{}
	for _, b := range blocks {
		kind, known := sectionNames[strings.ToLower(b.name)]
		if !known {
			res.Sections["unknown:"+b.name] = SectionResult{
				Kind: "unknown", Valid: false,
				Diagnostics: []model.Diagnostic{{
					Level: "error", Code: "unknown-section", Line: b.hdr,
					Message: fmt.Sprintf("unknown section %q", b.name),
				}},
			}
			continue
		}
		if seen[kind] {
			res.Sections[kind+"#dup@"+strconv.Itoa(b.hdr)] = SectionResult{
				Kind: kind, Valid: false,
				Diagnostics: []model.Diagnostic{{
					Level: "error", Code: "duplicate-section", Line: b.hdr, Section: kind,
					Message: fmt.Sprintf("section %s appears twice; second block ignored", kind),
				}},
			}
			continue
		}
		seen[kind] = true
		sr := parseSection(kind, b.body, b.hdr)
		res.Sections[kind] = sr
	}
	return res
}

func parseSection(kind string, body []string, hdr int) SectionResult {
	var sr SectionResult
	kind = kind
	addErr := func(line int, code, msg string) {
		sr.Diagnostics = append(sr.Diagnostics, model.Diagnostic{
			Level: "error", Code: code, Line: line, Section: kind, Message: msg,
		})
	}
	addWarn := func(line int, code, msg string) {
		sr.Diagnostics = append(sr.Diagnostics, model.Diagnostic{
			Level: "warning", Code: code, Line: line, Section: kind, Message: msg,
		})
	}
	_ = addErr
	_ = addWarn
	switch kind {
	case model.KindPoints:
		rows := parsePoints(body, &sr)
		sr.Kind, sr.Parsed = kind, rows
	case model.KindDrift:
		rows := parseDrift(body, &sr)
		sr.Kind, sr.Parsed = kind, rows
	case model.KindBlank:
		rows := parseBlank(body, &sr)
		sr.Kind, sr.Parsed = kind, rows
	default:
		parseSection2(kind, body, &sr)
		return sr
	}
	sr.Valid = !hasError(sr.Diagnostics)
	return sr
}

func hasError(ds []model.Diagnostic) bool {
	for _, d := range ds {
		if d.Level == "error" {
			return true
		}
	}
	return false
}

// splitCells accepts comma or whitespace separated cells.
func splitCells(line string) []string {
	line = strings.ReplaceAll(line, ",", " ")
	return strings.Fields(line)
}

func parsePoints(body []string, sr *SectionResult) []model.SampleRow {
	var rows []model.SampleRow
	seenT := map[float64]bool{}
	for idx, line := range body {
		ln := idx + 1
		cells := splitCells(line)
		row := model.SampleRow{Line: ln, ID: fmt.Sprintf("pt_%06d", idx+1)}
		switch len(cells) {
		case 0:
			continue
		case 1:
			sr.Diagnostics = append(sr.Diagnostics, errAt(ln, model.KindPoints,
				"row-shape", "need t and mass columns (t[,T],m); got 1 cell"))
			rows = append(rows, row)
			continue
		case 2:
			if t, d := asRepr(cells[0], UnitS, UnitS); d.Message != "" {
				d.Line = ln
				sr.Diagnostics = append(sr.Diagnostics, d)
			} else {
				row.T = t
			}
			if m, d := asRepr(cells[1], UnitMg, UnitMg); d.Message != "" {
				d.Line = ln
				sr.Diagnostics = append(sr.Diagnostics, d)
			} else {
				row.Mass = m
			}
		default:
			for j, c := range cells {
				var d model.Diagnostic
				switch j {
				case 0:
					row.T, d = asRepr(c, UnitS, UnitS)
				case 1:
					row.Temp, d = asRepr(c, UnitC, UnitC)
				case 2:
					row.Mass, d = asRepr(c, UnitMg, UnitMg)
				default:
					sr.Diagnostics = append(sr.Diagnostics, model.Diagnostic{
						Level: "warning", Code: "extra-cell", Line: ln,
						Section: model.KindPoints,
						Message: fmt.Sprintf("ignoring extra column %d: %q", j+1, c),
					})
					continue
				}
				if d.Message != "" {
					d.Line = ln
					sr.Diagnostics = append(sr.Diagnostics, d)
				}
			}
		}
		if row.T.Parsable {
			if seenT[row.T.Value] {
				sr.Diagnostics = append(sr.Diagnostics, model.Diagnostic{
					Level: "warning", Code: "duplicate-time", Line: ln,
					Section: model.KindPoints,
					Message: fmt.Sprintf("duplicate sample time %g s retained (not dropped)", row.T.Value),
				})
			}
			seenT[row.T.Value] = true
		}
		rows = append(rows, row)
	}
	if len(rows) < 2 {
		sr.Diagnostics = append(sr.Diagnostics, errAt(0, model.KindPoints,
			"too-few-points", "points section needs at least 2 rows"))
	}
	return rows
}

func errAt(line int, section, code, msg string) model.Diagnostic {
	return model.Diagnostic{Level: "error", Code: code, Line: line, Section: section, Message: msg}
}

func parseDrift(body []string, sr *SectionResult) []model.DriftAnchor {
	var rows []model.DriftAnchor
	for idx, line := range body {
		ln := idx + 1
		cells := splitCells(line)
		row := model.DriftAnchor{Line: ln, ID: fmt.Sprintf("da_%06d", idx+1)}
		if len(cells) < 2 {
			sr.Diagnostics = append(sr.Diagnostics, errAt(ln, model.KindDrift, "row-shape",
				"drift anchor needs t,m"))
			rows = append(rows, row)
			continue
		}
		if r, d := asRepr(cells[0], UnitS, UnitS); d.Message != "" {
			d.Line = ln
			sr.Diagnostics = append(sr.Diagnostics, d)
		} else {
			row.T = r
		}
		if r, d := asRepr(cells[1], UnitMg, UnitMg); d.Message != "" {
			d.Line = ln
			sr.Diagnostics = append(sr.Diagnostics, d)
		} else {
			row.M = r
		}
		rows = append(rows, row)
	}
	if len(rows) < 2 {
		sr.Diagnostics = append(sr.Diagnostics, errAt(0, model.KindDrift,
			"too-few-anchors", "drift section needs at least 2 anchors"))
	}
	return rows
}

func parseBlank(body []string, sr *SectionResult) []model.BlankPoint {
	var rows []model.BlankPoint
	for idx, line := range body {
		ln := idx + 1
		cells := splitCells(line)
		row := model.BlankPoint{Line: ln, ID: fmt.Sprintf("bk_%06d", idx+1)}
		if len(cells) < 2 {
			sr.Diagnostics = append(sr.Diagnostics, errAt(ln, model.KindBlank, "row-shape",
				"blank point needs t,m"))
			rows = append(rows, row)
			continue
		}
		if r, d := asRepr(cells[0], UnitS, UnitS); d.Message != "" {
			d.Line = ln
			sr.Diagnostics = append(sr.Diagnostics, d)
		} else {
			row.T = r
		}
		if r, d := asRepr(cells[1], UnitMg, UnitMg); d.Message != "" {
			d.Line = ln
			sr.Diagnostics = append(sr.Diagnostics, d)
		} else {
			row.Mass = r
		}
		rows = append(rows, row)
	}
	if len(rows) < 2 {
		sr.Diagnostics = append(sr.Diagnostics, errAt(0, model.KindBlank,
			"too-few-points", "blank section needs at least 2 points"))
	}
	return rows
}
