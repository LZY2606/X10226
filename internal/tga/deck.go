package tga

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Deck grammar (UTF-8 text, line oriented):
//
//	# comment / blank lines are ignored
//	@<layer>                 opens a section for one stratified input layer
//	columns: name(unit) ...   declares columns (one per section, required)
//	<cells ...>               comma/whitespace separated; "GAP" marks a missing sample
//
// Unknown sections, missing column declarations and arity mismatches are
// *fatal* for that fragment only, so a multi-layer submission can half-fail.
// Unconvertible cell values are not fatal: they stay in place with a
// diagnostic and the analysis layer marks dependent values unknown.
type ParsedFragment struct {
	Layer       string
	Columns     []string
	Units       []string
	Rows        [][]Cell
	Diagnostics []Diag // non-fatal, carried onto the accepted node
	Fatal       []Diag // fatal: fragment must be rejected but is retained
	RawText     string
	StartLine   int
	EndLine     int
}

var (
	headerRe  = regexp.MustCompile(`^@([A-Za-z0-9_-]+)\s*$`)
	columnsRe = regexp.MustCompile(`^columns\s*:\s*(.+)$`)
	colCellRe = regexp.MustCompile(`^([A-Za-z_][A-Za-z0-9_]*)\(([^)]*)\)$`)
	inlineRe  = regexp.MustCompile(`^([-+0-9.eEµμ°%/A-Za-z]+)\(([^)]*)\)$`)
)

// KnownSections maps deck section names to layer identifiers.
var KnownSections = map[string]string{
	"raw-samples":  LayerSamples,
	"samples":      LayerSamples,
	"drift":        LayerDrift,
	"program":      LayerProgram,
	"valve":        LayerValve,
	"gas-valve":    LayerValve,
	"blank":        LayerBlank,
	"blank-curve":  LayerBlank,
	"initial-mass": LayerInitialMass,
	"m0":           LayerInitialMass,
}

// ExpectedColumns maps each layer to the columns it must declare (by canonical
// name). Gas columns are free text and the third program/sample column is the
// temperature coordinate.
var ExpectedColumns = map[string][]string{
	LayerSamples:     {"t", "m", "T"},
	LayerBlank:       {"t", "m", "T"},
	LayerDrift:       {"t", "drift"},
	LayerProgram:     {"t", "T"},
	LayerValve:       {"t", "gas"},
	LayerInitialMass: {"m0"},
}

// ParseDeck splits a submission into independently judged fragments.
func ParseDeck(text string) []ParsedFragment {
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	var frags []ParsedFragment
	var cur *ParsedFragment
	var rawLines []int

	flush := func(endLine int) {
		if cur == nil {
			return
		}
		cur.EndLine = endLine
		cur.RawText = joinLines(lines, rawLines)
		frags = append(frags, *cur)
		cur = nil
		rawLines = nil
	}

	for i, line := range lines {
		lineNo := i + 1
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if strings.HasPrefix(trimmed, "@") {
			flush(lineNo - 1)
			m := headerRe.FindStringSubmatch(trimmed)
			if m == nil {
				frags = append(frags, ParsedFragment{
					RawText:   line,
					StartLine: lineNo,
					EndLine:   lineNo,
					Fatal: []Diag{{Code: "bad-header", Severity: "error", Row: lineNo,
						Message: fmt.Sprintf("malformed section header %q", trimmed)}},
				})
				continue
			}
			layer, known := KnownSections[m[1]]
			cur = &ParsedFragment{StartLine: lineNo}
			rawLines = []int{i}
			if !known {
				cur.Fatal = append(cur.Fatal, Diag{Code: "unknown-section", Severity: "error", Row: lineNo,
					Message: fmt.Sprintf("unknown input section %q", "@"+m[1])})
				cur.Layer = m[1]
			} else {
				cur.Layer = layer
			}
			continue
		}

		if cur == nil {
			frags = append(frags, ParsedFragment{
				RawText: line, StartLine: lineNo, EndLine: lineNo,
				Fatal: []Diag{{Code: "stray-line", Severity: "error", Row: lineNo,
					Message: fmt.Sprintf("data outside any section: %q", trimmed)}},
			})
			continue
		}
		rawLines = append(rawLines, i)

		if m := columnsRe.FindStringSubmatch(trimmed); m != nil {
			if len(cur.Columns) > 0 {
				cur.Fatal = append(cur.Fatal, Diag{Code: "dup-columns", Severity: "error",
					Layer: cur.Layer, Row: lineNo, Message: "columns declared twice"})
				continue
			}
			for _, tok := range splitCells(m[1]) {
				cm := colCellRe.FindStringSubmatch(tok)
				if cm == nil {
					cur.Fatal = append(cur.Fatal, Diag{Code: "bad-column", Severity: "error",
						Layer: cur.Layer, Row: lineNo, Column: tok,
						Message: fmt.Sprintf("column must look like name(unit), got %q", tok)})
					continue
				}
				cur.Columns = append(cur.Columns, cm[1])
				cur.Units = append(cur.Units, canonicalUnit(cm[2]))
			}
			continue
		}

		tokens := splitCells(trimmed)
		rowIdx := len(cur.Rows)
		cells := make([]Cell, len(tokens))
		for c, tok := range tokens {
			col := ""
			if c < len(cur.Columns) {
				col = cur.Columns[c]
			}
			cells[c] = parseCell(tok, col, lineNo, cur.Layer)
		}
		if len(cur.Columns) > 0 && len(tokens) != len(cur.Columns) {
			cur.Fatal = append(cur.Fatal, Diag{Code: "arity", Severity: "error",
				Layer: cur.Layer, Row: lineNo,
				Message: fmt.Sprintf("row has %d cells but %d columns declared", len(tokens), len(cur.Columns))})
		}
		_ = rowIdx
		cur.Rows = append(cur.Rows, cells)
	}
	flush(len(lines))

	for fi := range frags {
		f := &frags[fi]
		if f == nil || len(f.Fatal) > 0 || len(f.Columns) == 0 {
			if len(f.Columns) == 0 && len(f.Fatal) == 0 && f.Layer != "" {
				f.Fatal = append(f.Fatal, Diag{Code: "no-columns", Severity: "error",
					Layer: f.Layer, Row: f.StartLine, Message: "section has no columns declaration"})
			}
			continue
		}
		expected := ExpectedColumns[f.Layer]
		if !sameColumns(f.Columns, expected) {
			f.Fatal = append(f.Fatal, Diag{Code: "columns-mismatch", Severity: "error",
				Layer: f.Layer, Row: f.StartLine,
				Message: fmt.Sprintf("layer %s expects columns %v, got %v", f.Layer, expected, f.Columns)})
		}
	}
	return frags
}

func joinLines(lines []string, idxs []int) string {
	out := make([]string, 0, len(idxs))
	for _, i := range idxs {
		out = append(out, lines[i])
	}
	return strings.Join(out, "\n")
}

func splitCells(s string) []string {
	s = strings.ReplaceAll(s, ",", " ")
	fields := strings.Fields(s)
	return fields
}

func sameColumns(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range want {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func parseCell(tok, col, row int, layer string) Cell {
	cell := Cell{Raw: tok}
	if strings.EqualFold(tok, "GAP") || tok == "?" || tok == "" {
		cell.Diagnostic = "gap"
		return cell
	}
	decl := ""
	num := tok
	if m := inlineRe.FindStringSubmatch(tok); m != nil {
		num = m[1]
		decl = m[2]
	}
	unit := canonicalUnit(decl)
	cell.Declared = decl
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		cell.Diagnostic = "unconvertible:" + err.Error()
		return cell
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		cell.Diagnostic = "unconvertible:non-finite"
		return cell
	}
	conv, canon, ok := convertValue(v, col, unit, decl)
	if !ok {
		cell.Diagnostic = fmt.Sprintf("unconvertible:unit %q invalid for column %q", decl, col)
		return cell
	}
	cell.OK = true
	cell.Value = conv
	cell.Unit = canon
	return cell
}

func canonicalUnit(u string) string {
	u = strings.TrimSpace(u)
	switch u {
	case "s", "sec", "secs", "second", "seconds":
		return UnitSecond
	case "min", "mins", "minute", "minutes":
		return UnitMinute
	case "mg", "milligram", "milligrams":
		return UnitMg
	case "g", "gram", "grams":
		return UnitGram
	case "°C", "degC", "C", "celsius":
		return UnitCelsius
	case "K", "kelvin":
		return UnitKelvin
	case "%", "pct", "percent":
		return UnitPercent
	case "mg/min":
		return UnitMgPerMin
	case "%/min":
		return UnitPctPerMin
	case "°C/min", "C/min":
		return UnitCPerMin
	case "":
		return ""
	default:
		return u
	}
}

// convertValue normalizes into the canonical unit implied by the column.
// Gas/text columns never take this path.
func convertValue(v float64, col, unit, declared string) (float64, string, bool) {
	switch col {
	case "t":
		switch unit {
		case UnitSecond:
			return v, UnitSecond, true
		case UnitMinute:
			return v * 60, UnitSecond, true
		}
	case "m", "m0", "drift":
		switch unit {
		case UnitMg:
			return v, UnitMg, true
		case UnitGram:
			return v * 1000, UnitMg, true
		}
	case "T":
		switch unit {
		case UnitCelsius:
			return v, UnitCelsius, true
		case UnitKelvin:
			return v - 273.15, UnitCelsius, true
		}
	case "gas":
		return v, UnitGas, true
	}
	return 0, "", false
}
