// Package ingest parses the small, copy-pasteable layered text format. It
// preserves every original representation; normalization to canonical units is
// left to the derived layer. Unconvertible values stay in place with a
// diagnostic and are never dropped to make plots draw.
package ingest

import (
	"fmt"
	"strconv"
	"strings"

	"tgasys/internal/domain"
	"tgasys/internal/quant"
)

// Cell is a parsed input cell: raw text plus a status/diagnostic.
type Cell struct {
	Raw   string      `json:"raw"`
	Q     quant.Q     `json:"q"`
}

// Row is one preserved input record.
type Row struct {
	Ordinal int      `json:"ordinal"`
	Ref     string   `json:"ref"` // stable row key: first column raw text
	Cells   []Cell   `json:"cells"`
	Status  string   `json:"status"`
	Diag    string   `json:"diag,omitempty"`
}

// Layer is one parsed stratum of an import bundle.
type Layer struct {
	Name    domain.Layer `json:"name"`
	Columns []string     `json:"columns"`
	Rows    []Row        `json:"rows"`
	OK      bool         `json:"ok"`
	Reason  string       `json:"reason,omitempty"` // set when whole layer is rejected (half failure)
}

// Bundle is the result of parsing an import payload.
type Bundle struct {
	Layers []*Layer `json:"layers"`
}

type colSpec struct {
	name string
	kind string // quant kind: mass|temp|time|rate|enum|text
}

var schemas = map[domain.Layer][]colSpec{
	domain.LayerSample: {
		{"time_s", "time"}, {"mass_mg", "mass"}, {"temp_c", "temp"},
	},
	domain.LayerDrift: {
		{"method", "text"}, {"slope_mg_per_s", "text"}, {"intercept_mg", "text"}, {"note", "text"},
	},
	domain.LayerProgram: {
		{"start_s", "time"}, {"end_s", "time"}, {"rate_c_per_min", "rate"}, {"target_temp_c", "temp"},
	},
	domain.LayerGas: {
		{"at_s", "time"}, {"from_gas", "text"}, {"to_gas", "text"}, {"flow_sccm", "text"},
	},
	domain.LayerBlank: {
		{"time_s", "time"}, {"blank_mass_mg", "mass"},
	},
	domain.LayerInitial: {
		{"at_s", "time"}, {"mass_mg", "mass"}, {"note", "text"},
	},
	domain.LayerParams: {
		{"key", "text"}, {"value", "text"},
	},
}

// ParseBundle splits a payload into @layer sections and parses each.
func ParseBundle(text string) (*Bundle, error) {
	b := &Bundle{}
	var cur *Layer
	var have bool
	lineNo := 0
	for _, line := range strings.Split(text, "\n") {
		lineNo++
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "//") || (strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "# @layer")) {
			continue
		}
		if strings.HasPrefix(trimmed, "# @layer") {
			rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "# @layer"))
			name, cols, err := parseHeader(rest)
			if err != nil {
				cur = &Layer{OK: false, Reason: fmt.Sprintf("line %d: %v", lineNo, err)}
				b.Layers = append(b.Layers, cur)
				have = true
				continue
			}
			cur = &Layer{Name: domain.Layer(name), Columns: cols, OK: true}
			b.Layers = append(b.Layers, cur)
			have = true
			continue
		}
		if !have {
			return nil, fmt.Errorf("line %d: data before any # @layer header", lineNo)
		}
		if cur == nil || !cur.OK {
			continue
		}
		fields := splitCSV(trimmed)
		spec, known := schemas[cur.Name]
		if !known {
			cur.OK = false
			cur.Reason = fmt.Sprintf("unknown layer %q", cur.Name)
			continue
		}
		if len(fields) > len(spec) {
			cur.OK = false
			cur.Reason = fmt.Sprintf("line %d: %d fields, expected %d", lineNo, len(fields), len(spec))
			continue
		}
		row := Row{Ordinal: len(cur.Rows) + 1, Status: "ok"}
		var diags []string
		for i, f := range fields {
			c := Cell{Raw: f}
			switch spec[i].kind {
			case "text", "enum":
				c.Q = quant.Q{Raw: f, Unit: "text", Status: quant.StatusOK, Value: 0}
			default:
				c.Q = quant.Parse(f, spec[i].kind)
				if c.Q.Status == quant.StatusInvalid {
					row.Status = "invalid"
					diags = append(diags, c.Q.Diagnostic)
				} else if c.Q.Status == quant.StatusUnknown {
					if row.Status == "ok" {
						row.Status = "unknown"
					}
				}
			}
			row.Cells = append(row.Cells, c)
		}
		if len(fields) > 0 {
			row.Ref = row.Cells[0].Raw
		}
		row.Diag = strings.Join(diags, "; ")
		cur.Rows = append(cur.Rows, row)
	}
	return b, nil
}

func parseHeader(rest string) (string, []string, error) {
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return "", nil, fmt.Errorf("empty layer header")
	}
	name := parts[0]
	if _, ok := schemas[domain.Layer(name)]; !ok {
		return "", nil, fmt.Errorf("unknown layer %q", name)
	}
	cols := []string{}
	for _, p := range parts[1:] {
		if strings.HasPrefix(p, "columns:") {
			c := strings.TrimPrefix(p, "columns:")
			if c != "" {
				cols = strings.Split(c, ",")
			}
		}
	}
	spec := schemas[domain.Layer(name)]
	if len(cols) != len(spec) {
		return "", nil, fmt.Errorf("layer %s requires columns:%s", name, joinNames(spec))
	}
	for i, cn := range cols {
		if cn != spec[i].name {
			return "", nil, fmt.Errorf("layer %s column %d must be %q, got %q", name, i+1, spec[i].name, cn)
		}
	}
	return name, cols, nil
}

func joinNames(spec []colSpec) string {
	ns := make([]string, len(spec))
	for i, s := range spec {
		ns[i] = s.name
	}
	return strings.Join(ns, ",")
}

// Split respecting no quotes complexity: support simple double-quoted fields.
func splitCSV(line string) []string {
	var out []string
	var cur strings.Builder
	inQuote := false
	for _, r := range line {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ',' && !inQuote:
			out = append(out, strings.TrimSpace(cur.String()))
			cur.Reset()
		default:
			cur.WriteRune(r)
		}
	}
	out = append(out, strings.TrimSpace(cur.String()))
	return out
}

// AsFloat is a small helper for text-encoded numeric parameter rows.
func AsFloat(s string) (float64, error) {
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, err
	}
	return v, nil
}
