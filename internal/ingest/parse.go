// Package ingest parses stratified TGA source submissions. It accepts a small,
// copy-pasteable block format and produces explicit format errors instead of
// silently coercing data. Raw cells are preserved verbatim.
package ingest

import (
	"fmt"
	"strings"

	"github.com/thermal/tga/internal/domain"
)

// Block separates one source layer in a batch submission:
//
//	# @layer samples
//	# units: time=s; temp=C; mass=mg
//	time,temp,mass
//	0,30,10.000
type Block struct {
	Name string
	Raw  string // original fragment, preserved even on failure
}

// ParsedLayer is the result of parsing one block.
type ParsedLayer struct {
	Layer       *domain.RawLayer
	Diagnostics []domain.Diagnostic
	// FatalError makes the whole layer part of a half-failed import.
	Fatal *LayerError
}

type LayerError struct {
	Code string
	Msg  string
}

func (e *LayerError) Error() string { return e.Code + ": " + e.Msg }

var requiredColumns = map[domain.Layer][]string{
	domain.LayerSamples:  {"time", "temp", "mass"},
	domain.LayerDrift:    {"time", "mass"},
	domain.LayerBlank:    {"time", "mass"},
	domain.LayerFurnace:  {"start", "end", "start_temp", "target_temp", "kind"},
	domain.LayerGasValve: {"time", "from", "to"},
	domain.LayerMass0:    {"time", "mass"},
}

// SplitBlocks cuts a batch submission into layer blocks.
func SplitBatch(text string) []Block {
	var blocks []Block
	var cur *Block
	for _, line := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "# @layer") {
			if cur != nil {
				blocks = append(blocks, *cur)
			}
			name := strings.TrimSpace(strings.TrimPrefix(trim, "# @layer"))
			name = strings.Fields(name)[0]
			cur = &Block{Name: name, Raw: line + "\n"}
			continue
		}
		if cur != nil && trim != "" {
			cur.Raw += line + "\n"
		}
	}
	if cur != nil {
		blocks = append(blocks, *cur)
	}
	return blocks
}

// ParseBatch parses all blocks. Unknown layers or structurally invalid layers
// are returned as failures (half failure), good layers as parsed results.
func ParseBatch(text string) (ok []ParsedLayer, failed []domain.FailedLayer, diags []domain.Diagnostic) {
	for _, b := range SplitBlocks(text) {
		layer := domain.Layer(b.Name)
		if !layer.Valid() {
			failed = append(failed, domain.FailedLayer{
				Layer: domain.Layer(b.Name), Code: "unknown_layer",
				Error: fmt.Sprintf("unknown source layer %q", b.Name), Raw: b.Raw,
			})
			continue
		}
		pl, err := ParseBlock(layer, b.Raw)
		if err != nil {
			failed = append(failed, domain.FailedLayer{
				Layer: layer, Code: err.Code, Error: err.Msg, Raw: b.Raw,
			})
			continue
		}
		ok = append(ok, pl)
		diags = append(diags, pl.Diagnostics...)
	}
	return ok, failed, diags
}

// ParseBlock parses one layer fragment.
func ParseBlock(layer domain.Layer, fragment string) (ParsedLayer, *LayerError) {
	units := domain.LayerUnits{Time: "s", Temp: "C", Mass: "mg"}
	var header []string
	var rows [][]string
	sawHeader := false
	for _, line := range strings.Split(strings.ReplaceAll(fragment, "\r\n", "\n"), "\n") {
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "# @layer") {
			continue
		}
		if strings.HasPrefix(trim, "#") {
			if strings.HasPrefix(strings.ToLower(trim), "# units:") {
				parseUnits(strings.TrimPrefix(trim, "# units:"), &units)
			}
			continue
		}
		cells := splitCSV(trim)
		if !sawHeader {
			header = normalizeHeader(cells)
			if err := checkHeader(layer, header); err != nil {
				return ParsedLayer{}, err
			}
			sawHeader = true
			continue
		}
		rows = append(rows, cells)
	}
	if !sawHeader {
		return ParsedLayer{}, &LayerError{Code: "missing_header", Msg: string(layer) + ": no header row"}
	}
	if len(rows) == 0 {
		return ParsedLayer{}, &LayerError{Code: "empty_layer", Msg: string(layer) + ": no data rows"}
	}
	pl := ParsedLayer{Layer: &domain.RawLayer{
		Layer: layer, Units: units, Header: header, Rows: rows,
	}}
	pl.Diagnostics = rowDiagnostics(layer, header, rows, units)
	return pl, nil
}

func parseUnits(s string, u *domain.LayerUnits) {
	for _, part := range strings.Split(s, ";") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		switch strings.TrimSpace(strings.ToLower(kv[0])) {
		case "time":
			u.Time = strings.TrimSpace(kv[1])
		case "temp":
			u.Temp = strings.TrimSpace(kv[1])
		case "mass":
			u.Mass = strings.TrimSpace(kv[1])
		}
	}
}

func splitCSV(line string) []string {
	var out []string
	for _, c := range strings.Split(line, ",") {
		out = append(out, strings.TrimSpace(c))
	}
	return out
}

func normalizeHeader(cells []string) []string {
	alias := map[string]string{
		"temperature": "temp", "t": "time", "m": "mass", "mass_mg": "mass",
		"time_s": "time", "temp_c": "temp", "time_min": "time",
		"start_time": "start", "end_time": "end", "to_gas": "to", "from_gas": "from",
		"start_temp_c": "start_temp", "target_temp_c": "target_temp",
	}
	out := make([]string, len(cells))
	for i, c := range cells {
		c = strings.ToLower(strings.TrimSpace(c))
		if a, ok := alias[c]; ok {
			c = a
		}
		out[i] = c
	}
	return out
}

func checkHeader(layer domain.Layer, header []string) *LayerError {
	present := map[string]bool{}
	for _, h := range header {
		present[h] = true
	}
	var missing []string
	for _, req := range requiredColumns[layer] {
		if !present[req] {
			missing = append(missing, req)
		}
	}
	if len(missing) > 0 {
		return &LayerError{Code: "bad_header", Msg: fmt.Sprintf("%s: missing columns %v", layer, missing)}
	}
	return nil
}

func rowDiagnostics(layer domain.Layer, header []string, rows [][]string, units domain.LayerUnits) []domain.Diagnostic {
	idx := map[string]int{}
	for i, h := range header {
		idx[h] = i
	}
	var diags []domain.Diagnostic
	add := func(r int, col, code, msg, raw string) {
		diags = append(diags, domain.Diagnostic{Layer: layer, Row: r + 1, Field: col, Code: code, Message: msg, Raw: raw})
	}
	checkNum := func(r int, col, unit string, dim domain.Dim) {
		i := idx[col]
		if i >= len(rows[r]) {
			add(r, col, "missing_cell", "missing cell", "")
			return
		}
		raw := rows[r][i]
		m := domain.ParseMeasurement(raw, unit, dim)
		if !m.Valid {
			add(r, col, "unconvertible_value", m.Diag, raw)
		}
	}
	for r := range rows {
		switch layer {
		case domain.LayerSamples:
			checkNum(r, "time", units.Time, domain.DimTime)
			checkNum(r, "temp", units.Temp, domain.DimTemp)
			checkNum(r, "mass", units.Mass, domain.DimMass)
		case domain.LayerDrift, domain.LayerBlank:
			checkNum(r, "time", units.Time, domain.DimTime)
			checkNum(r, "mass", units.Mass, domain.DimMass)
		case domain.LayerFurnace:
			checkNum(r, "start", units.Time, domain.DimTime)
			checkNum(r, "end", units.Time, domain.DimTime)
			checkNum(r, "start_temp", units.Temp, domain.DimTemp)
			checkNum(r, "target_temp", units.Temp, domain.DimTemp)
		case domain.LayerGasValve:
			checkNum(r, "time", units.Time, domain.DimTime)
		case domain.LayerMass0:
			checkNum(r, "time", units.Time, domain.DimTime)
			checkNum(r, "mass", units.Mass, domain.DimMass)
		}
	}
	return diags
}
