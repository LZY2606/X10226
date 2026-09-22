package importer

import (
	"encoding/json"
	"fmt"
	"strconv"

	"tga-review/internal/domain"
	"tga-review/internal/ident"
)

type jsonBundle struct {
	Format  string                        `json:"format"`
	Version int                           `json:"version"`
	Layers  map[string]jsonTable         `json:"layers"`
}

// jsonTable mirrors the text format: columns like "t[s]" and row arrays whose
// cells are numbers (good) or strings (often the carried diagnostic value).
type jsonTable struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

func parseJSON(project, source string, payload []byte) (*Result, error) {
	var b jsonBundle
	if err := json.Unmarshal(payload, &b); err != nil {
		return nil, fmt.Errorf("invalid json: %v", err)
	}
	if b.Format != "tga-bundle" {
		return nil, fmt.Errorf("not a tga-bundle (format=%q)", b.Format)
	}
	if b.Version != 1 {
		return nil, fmt.Errorf("unsupported bundle version %d", b.Version)
	}
	res := &Result{Project: project, SourceRef: source}
	for _, layer := range domain.Layers {
		tbl, ok := b.Layers[layer]
		if !ok {
			continue
		}
		lines := tableToLines(tbl)
		if err := buildSectionFromLines(res, project, source, layer, lines); err != nil {
			rejectLayer(res, layer, err.Error())
			clearLayer(res, layer)
		}
	}
	finalizeAccepted(res)
	return res, nil
}

func buildSectionFromLines(res *Result, project, source, layer string, lines []string) error {
	if len(lines) < 1 {
		return fmt.Errorf("empty section %s", layer)
	}
	bid := ident.BatchID(project, source, []byte(layer+joinLines(lines)))
	cols, err := parseHeader(splitTokens(lines[0]))
	if err != nil {
		return fmt.Errorf("%s: %v", layer, err)
	}
	m := colMap(cols)
	for i, l := range lines[1:] {
		fields := splitTokens(l)
		ord := i + 1
		if err := buildRow(res, layer, bid, ord, m, cols, fields); err != nil {
			return err
		}
	}
	return nil
}

// tableToLines turns a json table into header + token lines. Cells containing
// spaces (e.g. gas names) are quoted; splitTokens respects quotes.
func tableToLines(t jsonTable) []string {
	lines := []string{joinCells(t.Columns)}
	for _, row := range t.Rows {
		cells := make([]string, len(row))
		for i, c := range row {
			switch v := c.(type) {
			case float64:
				cells[i] = strconv.FormatFloat(v, 'g', -1, 64)
			case string:
				if needsQuote(v) {
					cells[i] = quote(v)
				} else {
					cells[i] = v
				}
			case bool:
				if v {
					cells[i] = "true"
				} else {
					cells[i] = "false"
				}
			case nil:
				cells[i] = ""
			default:
				b, _ := json.Marshal(v)
				cells[i] = string(b)
			}
		}
		lines = append(lines, joinCells(cells))
	}
	return lines
}

func needsQuote(s string) bool {
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '"' {
			return true
		}
	}
	return false
}

func quote(s string) string {
	out := make([]rune, 0, len(s)+2)
	out = append(out, '"')
	for _, r := range s {
		if r == '"' {
			out = append(out, '\\')
		}
		out = append(out, r)
	}
	out = append(out, '"')
	return string(out)
}

func joinCells(cells []string) string {
	out := ""
	for i, c := range cells {
		if i > 0 {
			out += " "
		}
		out += c
	}
	return out
}

// splitTokens splits on whitespace but preserves quoted runs.
func splitTokens(l string) []string {
	var tokens []string
	var cur []rune
	inQ := false
	for _, r := range l {
		switch {
		case r == '"':
			inQ = !inQ
		case (r == ' ' || r == '\t') && !inQ:
			if len(cur) > 0 {
				tokens = append(tokens, string(cur))
				cur = cur[:0]
			}
		default:
			cur = append(cur, r)
		}
	}
	if len(cur) > 0 {
		tokens = append(tokens, string(cur))
	}
	return tokens
}
