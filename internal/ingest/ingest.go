package ingest

import (
	"encoding/csv"
	"unicode/utf8"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

// Result of parsing one layer fragment.
type Result struct {
	Layer    domain.SourceLayer
	Payload  map[string]any
	Diag     []domain.Diag
	Salvaged bool // true if at least one row was quarantined but layer kept
	Fatal    bool // true if the fragment must be rejected wholesale
}

// Parse dispatches on the layer kind and text format (csv|json).
func Parse(layer domain.SourceLayer, format, text string) Result {
	switch strings.ToLower(format) {
	case "json":
		return parseJSON(layer, text)
	case "csv", "":
		return parseCSV(layer, text)
	default:
		return fatal(layer, fmt.Sprintf("unsupported format %q", format))
	}
}

func fatal(layer domain.SourceLayer, msg string) Result {
	return Result{Layer: layer, Fatal: true, Diag: []domain.Diag{{Code: "E_FORMAT", Message: msg, Severity: "error"}}}
}

func parseJSON(layer domain.SourceLayer, text string) Result {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" || trimmed[0] != '{' && trimmed[0] != '[' {
		return fatal(layer, "json payload must be a JSON object or array")
	}
	var raw any
	dec := json.NewDecoder(strings.NewReader(text))
	dec.UseNumber()
	if err := dec.Decode(&raw); err != nil {
		return fatal(layer, "invalid json: "+err.Error())
	}
	switch layer {
	case domain.LayerSamples:
		return parseSamplesJSON(layer, raw)
	case domain.LayerDrift:
		return parseDriftJSON(layer, raw)
	case domain.LayerFurnace:
		return parseFurnaceJSON(layer, raw)
	case domain.LayerValves:
		return parseValvesJSON(layer, raw)
	case domain.LayerBlank:
		return parseBlankJSON(layer, raw)
	case domain.LayerInitMass:
		return parseInitMassJSON(layer, raw)
	}
	return fatal(layer, "unknown layer")
}

// readCSV parses tabular text into header + records, tolerant of trailing
// spaces and missing trailing columns.
func readCSV(text string) ([]string, [][]string, error) {
	r := csv.NewReader(strings.NewReader(strings.TrimSpace(text)))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, nil, err
	}
	if len(records) < 2 {
		return nil, nil, fmt.Errorf("expected header plus at least one row")
	}
	header := records[0]
	for i := range header {
		header[i] = strings.ToLower(strings.TrimSpace(header[i]))
	}
	if len(header) > 0 {
		header[0] = strings.TrimPrefix(header[0], "\uFEFF")
	}
	return header, records[1:], nil
}

func colIndex(header []string, names ...string) int {
	for _, n := range names {
		for i, h := range header {
			if h == n {
				return i
			}
		}
	}
	return -1
}

func cell(rec []string, i int) string {
	if i < 0 || i >= len(rec) {
		return ""
	}
	return strings.TrimSpace(rec[i])
}

// parseQ converts a token into a domain quantity in canonical units.
// Failures are non-fatal: the original token remains on the point.
func parseQ(token string, canonical domain.Unit, row int, field string) (domain.Q, domain.Diag) {
	q := domain.Q{Unit: canonical, Original: token}
	token = strings.TrimSpace(token)
	if token == "" {
		d := domain.Diag{Code: "E_EMPTY", Row: row, Field: field, Severity: "error", Message: field + " is empty"}
		return q, d
	}
	num, unit, err := splitNumUnit(token)
	if err != nil {
		q.Note = "unconvertible"
		d := domain.Diag{Code: "E_CONVERT", Row: row, Field: field, Severity: "error", Message: err.Error()}
		return q, d
	}
	if unit == "" {
		// bare numbers adopt the canonical unit declared by the column header
		unit = string(canonical)
	}
	converted, err := convertUnit(num, unit, canonical)
	if err != nil {
		q.Note = "unconvertible"
		d := domain.Diag{Code: "E_UNIT", Row: row, Field: field, Severity: "error", Message: err.Error()}
		return q, d
	}
	q.Value = converted
	q.Valid = true
	return q, domain.Diag{}
}

// numRunes are the characters allowed in the numeric prefix.
const numRunes = "0123456789+-.,eE "

func splitNumUnit(token string) (float64, string, error) {
	token = strings.TrimSpace(token)
	i := 0
	for i < len(token) {
		r, size := utf8.DecodeRuneInString(token[i:])
		if !strings.ContainsRune(numRunes, r) {
			break
		}
		i += size
	}
	numPart := strings.TrimSpace(token[:i])
	unitPart := strings.TrimSpace(token[i:])
	if numPart == "" {
		return 0, "", fmt.Errorf("no numeric prefix in %q", token)
	}
	// Guard against a trailing exponent-only prefix like "1e".
	if _, err := strconv.ParseFloat(strings.ReplaceAll(numPart, ",", ""), 64); err != nil {
		return 0, unitPart, fmt.Errorf("bad number %q", numPart)
	}
	v, err := strconv.ParseFloat(strings.ReplaceAll(numPart, ",", ""), 64)
	if err != nil {
		return 0, unitPart, fmt.Errorf("bad number %q", numPart)
	}
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0, unitPart, fmt.Errorf("non-finite number %q", numPart)
	}
	return v, unitPart, nil
}
