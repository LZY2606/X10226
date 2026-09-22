package ingest

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"

	"tga-archive/internal/model"
)

// ParseResult is the outcome of parsing one layered CSV payload.
type ParseResult struct {
	Kind         model.LayerKind
	Headers      map[string]string
	Samples      *model.Samples
	Blank        *model.Blank
	Furnace      *model.Furnace
	Gases        *model.Gases
	Drift        *model.Drift
	Initial      *model.Initial
	Diags        []model.Diagnostic
	AcceptedRows int
	RejectedRows int
}

const (
	hLayer = "layer"
	hTime  = "time_unit"
	hTemp  = "temp_unit"
	hMass  = "mass_unit"
)

func diagon(kind model.LayerKind, row int, field, code, msg, val string, sev model.Severity) model.Diagnostic {
	return model.Diagnostic{LayerKind: kind, Row: row, Field: field, Severity: sev, Code: code, Message: msg, Value: val}
}

// Parse decodes one layered CSV payload. Fatal format problems (unknown layer
// kind, missing header columns) are returned as error; per-row problems become
// diagnostics and keep the offending original text in place.
func Parse(r io.Reader) (*ParseResult, error) {
	raw, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	headers := map[string]string{}
	var body [][]string
	lines := strings.Split(string(raw), "\n")
	var bodyLines []string
	for _, ln := range lines {
		trim := strings.TrimSpace(ln)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "#") {
			kv := strings.TrimSpace(strings.TrimPrefix(trim, "#"))
			if i := strings.Index(kv, "="); i > 0 {
				headers[strings.TrimSpace(kv[:i])] = strings.TrimSpace(kv[i+1:])
			}
			continue
		}
		bodyLines = append(bodyLines, ln)
	}
	kind := model.LayerKind(headers[hLayer])
	if kind == "" {
		return nil, fmt.Errorf("missing # layer=<kind> header")
	}
	switch kind {
	case model.KindSamples, model.KindBlank, model.KindFurnace, model.KindGases, model.KindDrift, model.KindInitial:
	default:
		return nil, fmt.Errorf("unknown layer kind %q", kind)
	}
	if len(bodyLines) > 0 {
		cr := csv.NewReader(bytes.NewReader([]byte(strings.Join(bodyLines, "\n"))))
		cr.FieldsPerRecord = -1
		cr.TrimLeadingSpace = true
		body, err = cr.ReadAll()
		if err != nil {
			return nil, fmt.Errorf("malformed CSV: %w", err)
		}
	}
	res := &ParseResult{Kind: kind, Headers: headers}
	if len(body) == 0 {
		return nil, fmt.Errorf("layer %q has no data rows", kind)
	}
	header := body[0]
	idx := mapCols(header)
	rows := body[1:]
	switch kind {
	case model.KindSamples:
		parseSamples(res, idx, rows, headers)
	case model.KindBlank:
		parseBlank(res, idx, rows, headers)
	case model.KindFurnace:
		parseFurnace(res, idx, rows, headers)
	case model.KindGases:
		parseGases(res, idx, rows, headers)
	case model.KindDrift:
		parseDrift(res, idx, rows, headers)
	case model.KindInitial:
		parseInitial(res, idx, rows, headers)
	}
	return res, nil
}

func mapCols(header []string) map[string]int {
	idx := map[string]int{}
	for i, h := range header {
		idx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	return idx
}

func cell(row []string, idx map[string]int, name string) (string, bool) {
	i, ok := idx[name]
	if !ok || i >= len(row) {
		return "", false
	}
	return strings.TrimSpace(row[i]), true
}

func requireCols(idx map[string]int, cols ...string) error {
	var missing []string
	for _, c := range cols {
		if _, ok := idx[c]; !ok {
			missing = append(missing, c)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing required columns: %s", strings.Join(missing, ", "))
	}
	return nil
}

func atof(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	f, err := strconv.ParseFloat(s, 64)
	return f, err == nil
}
