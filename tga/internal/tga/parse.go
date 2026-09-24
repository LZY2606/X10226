package tga

import (
	"fmt"
	"strconv"
	"strings"
)

// Import states.
const (
	ImportOK      = "ok"
	ImportPartial = "partial" // import half-failure: some rows kept, some rejected
	ImportFailed  = "failed"  // nothing usable
)

// sectionSchemas declares per-section table columns as
// field -> declared unit. A unit of "" means a non-quantity token (kept as-is).
var sectionSchemas = map[StreamKind]map[string]string{
	StreamPoints: {
		"time":         "s",
		"furnace_temp": "C",
		"mass":         "mg",
	},
	StreamBlank: {
		"time": "s",
		"mass": "mg",
	},
	StreamFurnace: {
		"time":      "s",
		"setpoint":  "C",
	},
	StreamGas: {
		"time":  "s",
		"valve": "",
		"gas":   "",
	},
}

// keyUnits lists scalar keys accepted under [sample] / [drift].
var keyUnits = map[string]string{
	"initial_mass":         "mg",
	"initial_mass_revised": "mg",
	"drift_rate":           "ug/min",
	"anchor_time":          "s",
}

// ParseImport parses one verbatim import text into a layered submission.
// importKey disambiguates rows from different submissions (e.g. seq).
// The raw text is always retained; rejected rows are retained too.
func ParseImport(text, importKey string) *Submission {
	sub := &Submission{
		Text:     text,
		Sections: map[StreamKind]string{},
		Rows:     map[StreamKind][]*Row{},
	}
	var current StreamKind
	var header map[string]string
	var headerCols []string
	var lastTime map[string]float64
	seenSection := map[StreamKind]bool{}

	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	for idx, line := range lines {
		lineno := idx + 1
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") || strings.HasPrefix(trim, "//") {
			continue
		}
		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			name := StreamKind(strings.ToLower(strings.TrimSpace(trim[1 : len(trim)-1])))
			if !validStream(name) {
				sub.addDiag(lineno, "", DiagError, "UNKNOWN_SECTION",
					"unknown section: "+trim)
				current = ""
				continue
			}
			current = name
			seenSection[name] = true
			sub.Sections[name] += line + "\n"
			header = nil
			headerCols = nil
			if lastTime == nil {
				lastTime = map[string]float64{}
			}
			continue
		}
		if current == "" {
			sub.addDiag(lineno, "", DiagError, "OUTSIDE_SECTION",
				"line outside any section: "+trim)
			continue
		}
		sub.Sections[current] += line + "\n"

		switch current {
		case StreamSample, StreamDrift:
			sub.parseKeyRow(current, lineno, line, importKey)
		default:
			if header == nil {
				h, cols, d := parseHeader(trim, current)
				if d != nil {
					d.LineNo = lineno
					d.Stream = string(current)
					sub.Diagnostics = append(sub.Diagnostics, *d)
					header = map[string]string{}
					headerCols = cols
				} else {
					header = h
					headerCols = cols
				}
				continue
			}
			sub.parseDataRow(current, lineno, line, importKey, header, headerCols, lastTime)
		}
	}

	for _, st := range AllStreams {
		if seenSection[st] && len(sub.Rows[st]) == 0 {
			sub.addDiag(0, string(st), DiagError, "EMPTY_SECTION",
				"section ["+string(st)+"] produced no usable rows")
		}
	}
	sub.State = sub.classify()
	return sub
}

func (s *Submission) parseKeyRow(st StreamKind, lineno int, line, importKey string) {
	eq := strings.IndexByte(line, '=')
	row := &Row{ID: rowID(st, lineno, line, importKey), Stream: st, LineNo: lineno,
		Raw: strings.TrimSpace(line), Fields: map[string]string{}, Values: map[string]Q{}}
	s.Rows[st] = append(s.Rows[st], row)
	if eq < 0 {
		row.Severity = DiagError
		row.Diagnostic = "expected key = value"
		s.addDiag(lineno, string(st), DiagError, "BAD_KEYVALUE", row.Diagnostic)
		return
	}
	key := strings.ToLower(strings.TrimSpace(line[:eq]))
	rhs := strings.TrimSpace(line[eq+1:])
	row.Fields["key"] = key
	if st == StreamSample && key == "sample_id" {
		row.Fields["value"] = rhs
		return
	}
	unit, known := keyUnits[key]
	if !known {
		row.Severity = DiagError
		row.Diagnostic = "unknown key: " + key
		s.addDiag(lineno, string(st), DiagError, "UNKNOWN_KEY", row.Diagnostic)
		return
	}
	valTok, unitTok := splitValueUnit(rhs)
	if unitTok == "" {
		unitTok = unit
	}
	q := parseQ(valTok, unitTok)
	row.Values[key] = q
	if !q.Valid {
		row.Severity = DiagError
		row.Diagnostic = key + ": " + q.Diagnostic
		s.addDiag(lineno, string(st), DiagError, "BAD_VALUE", row.Diagnostic)
	}
}

func (s *Submission) parseDataRow(st StreamKind, lineno int, line, importKey string,
	header map[string]string, cols []string, lastTime map[string]float64) {
	fields := splitCSV(line)
	row := &Row{ID: rowID(st, lineno, line, importKey), Stream: st, LineNo: lineno,
		Raw: strings.TrimSpace(line), Fields: map[string]string{}, Values: map[string]Q{}}
	s.Rows[st] = append(s.Rows[st], row)

	if len(header) == 0 {
		row.Severity = DiagError
		row.Diagnostic = "data row after unparseable header"
		s.addDiag(lineno, string(st), DiagError, "NO_HEADER", row.Diagnostic)
		return
	}
	if len(fields) != len(cols) {
		row.Severity = DiagError
		row.Diagnostic = fmt.Sprintf("expected %d fields, got %d", len(cols), len(fields))
		s.addDiag(lineno, string(st), DiagError, "FIELD_COUNT", row.Diagnostic)
		return
	}
	anyErr := false
	for i, col := range cols {
		unit := header[col]
		tok := strings.TrimSpace(fields[i])
		if unit == "" {
			row.Fields[col] = tok
			continue
		}
		q := parseQ(tok, unit)
		row.Values[col] = q
		if !q.Valid {
			anyErr = true
			s.addDiag(lineno, string(st), DiagError, "BAD_VALUE",
				col+": "+q.Diagnostic)
		}
	}
	if anyErr {
		row.Severity = DiagError
		row.Diagnostic = "one or more values could not be converted; row retained"
		return
}
	// Monotonic-time check on quantity tables.
	if tq, ok := row.Values["time"]; ok && tq.Valid {
		if prev, seen := lastTime[string(st)]; seen {
			switch {
			case tq.Value < prev-1e-9:
				row.Severity = DiagError
				row.Diagnostic = "time decreases relative to previous row"
				s.addDiag(lineno, string(st), DiagError, "TIME_ORDER", row.Diagnostic)
			case gap := tq.Value - prev; gap > hugeGap(st):
				row.Severity = DiagWarning
				row.Diagnostic = fmt.Sprintf("sampling gap of %.0f s; no interpolation will cross it", gap)
				s.addDiag(lineno, string(st), DiagWarning, "SAMPLING_GAP", row.Diagnostic)
			}
		}
		lastTime[string(st)] = tq.Value
	}
}

func hugeGap(st StreamKind) float64 {
	// Parser-level warning threshold; the engine applies the parameterised one.
	return 120
}

func parseHeader(line string, st StreamKind) (map[string]string, []string, *Diag) {
	fields := splitCSV(line)
	schema := sectionSchemas[st]
	h := map[string]string{}
	cols := make([]string, 0, len(fields))
	for _, f := range fields {
		name, unit := parseColHeader(f)
		if _, known := schema[name]; !known {
			return nil, cols, &Diag{Level: DiagError, Code: "UNKNOWN_COLUMN",
				Msg: "unknown column in [" + string(st) + "]: " + f}
		}
		h[name] = unit
		cols = append(cols, name)
	}
	required := requiredCols(st)
	for _, r := range required {
		if _, ok := h[r]; !ok {
			return nil, cols, &Diag{Level: DiagError, Code: "MISSING_COLUMN",
				Msg: "[" + string(st) + "] header missing column: " + r}
		}
	}
	return h, cols, nil
}

func requiredCols(st StreamKind) []string {
	switch st {
	case StreamPoints:
		return []string{"time", "mass"}
	case StreamBlank:
		return []string{"time", "mass"}
	case StreamFurnace:
		return []string{"time", "setpoint"}
	case StreamGas:
		return []string{"time", "gas"}
	}
	return nil
}

func parseColHeader(f string) (name, unit string) {
	f = strings.TrimSpace(f)
	if open := strings.IndexByte(f, '['); open >= 0 {
		name = strings.ToLower(strings.TrimSpace(f[:open]))
		unit = strings.TrimSpace(f[open+1 : len(f)-1])
		return
	}
	// unit declared after whitespace, e.g. "time[s]" already handled; accept
	// "mass mg" style.
	parts := strings.Fields(f)
	name = strings.ToLower(parts[0])
	if len(parts) > 1 {
		unit = strings.TrimSpace(parts[1])
	}
	return
}

func splitCSV(line string) []string {
	var out []string
	for _, p := range strings.Split(line, ",") {
		out = append(out, strings.TrimSpace(p))
	}
	return out
}

func splitValueUnit(s string) (val, unit string) {
	s = strings.TrimSpace(s)
	i := strings.IndexAny(s, " \t")
	if i < 0 {
		return s, ""
	}
	return s[:i], strings.TrimSpace(s[i+1:])
}

func rowID(st StreamKind, lineno int, raw, importKey string) string {
	return "row_" + shortHash(fmt.Sprintf("%s|%d|%s|%s", importKey, lineno, st, raw))
}

func validStream(s StreamKind) bool {
	for _, st := range AllStreams {
		if st == s {
			return true
		}
	}
	return false
}

func (s *Submission) addDiag(lineno int, stream, level, code, msg string) {
	s.Diagnostics = append(s.Diagnostics, Diag{LineNo: lineno, Stream: stream,
		Level: level, Code: code, Msg: msg})
}

func (s *Submission) classify() string {
	errs, usable := 0, 0
	for _, rows := range s.Rows {
		for _, r := range rows {
			if r.Severity == DiagError {
				errs++
			} else {
				usable++
			}
		}
}
	for _, d := range s.Diagnostics {
		if d.Level == DiagError {
			errs++
		}
}
	for _, rows := range s.Rows {
		usable += len(rows)
	}
	if usable == 0 {
		return ImportFailed
	}
	if errs > 0 {
		return ImportPartial
	}
	return ImportOK
}

// RowsByKind is a convenience for tests.
func (s *Submission) CleanRows(st StreamKind) []*Row {
	var out []*Row
	for _, r := range s.Rows[st] {
		if r.Severity != DiagError {
			out = append(out, r)
		}
}
	return out
}

// atoi helper kept for generated fixtures.
func atoi(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}
