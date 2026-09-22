package domain

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"tgaaudit/internal/units"
)

// Table is the pre-commit representation of a raw section.
type Table struct {
	SourceName string   `json:"source_name,omitempty"`
	Columns    []Column `json:"columns"`
	Rows       [][]Cell `json:"rows"`
}

// Bundle is a copy-pasteable import payload. Each section value is either a
// direct Table object or {"format":"csv","text":"...","source_name":"..."}.
type Bundle struct {
	BundleID    string                     `json:"bundle_id,omitempty"`
	Label       string                     `json:"label,omitempty"`
	Sections    map[string]json.RawMessage `json:"sections"`
	ParentHints map[string]string          `json:"parent_hints,omitempty"`
}

// tableWire allows both inline tables and embedded CSV text.
type tableWire struct {
	Format     string   `json:"format,omitempty"`
	SourceName string   `json:"source_name,omitempty"`
	Columns    []Column `json:"columns,omitempty"`
	Rows       [][]Cell `json:"rows,omitempty"`
	Text       string   `json:"text,omitempty"`
}

// ParseBundle decodes the outer envelope without touching section contents.
func ParseBundle(r io.Reader) (*Bundle, []string) {
	var diags []string
	dec := json.NewDecoder(r)
	var b Bundle
	if err := dec.Decode(&b); err != nil {
		return nil, []string{"bundle is not valid JSON: " + err.Error()}
	}
	if b.Sections == nil {
		diags = append(diags, "bundle contains no sections object")
	}
	return &b, diags
}

// DecodeTable resolves one section entry (inline table or CSV text).
func DecodeTable(raw json.RawMessage) (*Table, string) {
	var tw tableWire
	if err := json.Unmarshal(raw, &tw); err != nil {
		return nil, "section is not valid JSON: " + err.Error()
	}
	if strings.EqualFold(tw.Format, "csv") || tw.Text != "" {
		t, err := parseCSV(tw.Text)
		if err != nil {
			return nil, err.Error()
		}
		t.SourceName = tw.SourceName
		return t, ""
	}
	if len(tw.Columns) == 0 {
		return nil, "section has no columns"
	}
	return &Table{SourceName: tw.SourceName, Columns: tw.Columns, Rows: tw.Rows}, ""
}

// csvHeader describes a header cell like "mass[mg]" or "t_s[s]".
func parseCSV(text string) (*Table, error) {
	rd := csv.NewReader(strings.NewReader(text))
	rd.FieldsPerRecord = -1
	rd.TrimLeadingSpace = true
	all, err := rd.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("CSV parse error: %w", err)
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("CSV is empty")
	}
	header := all[0]
	cols := make([]Column, len(header))
	for i, h := range header {
		name, unit, basis := splitHeader(h)
		cols[i] = Column{Name: name, Unit: unit, Basis: basis}
	}
	rows := make([][]Cell, 0, len(all)-1)
	for _, rec := range all[1:] {
		if isBlankRecord(rec) {
			continue
		}
		row := make([]Cell, len(cols))
		for i := range cols {
			var v string
			if i < len(rec) {
				v = rec[i]
			}
			row[i] = Cell{Raw: v}
		}
		rows = append(rows, row)
	}
	return &Table{Columns: cols, Rows: rows}, nil
}

func isBlankRecord(rec []string) bool {
	for _, f := range rec {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

func splitHeader(h string) (name, unit, basis string) {
	h = strings.TrimSpace(h)
	open := strings.IndexByte(h, '[')
	if open < 0 || !strings.HasSuffix(h, "]") {
		return normalizeKey(h), "", ""
	}
	name = strings.TrimSpace(h[:open])
	rest := strings.TrimSuffix(h[open+1:], "]")
	parts := strings.SplitN(rest, ":", 2)
	unit = strings.TrimSpace(parts[0])
	if len(parts) == 2 {
		basis = strings.TrimSpace(parts[1])
	}
	return normalizeKey(name), unit, basis
}

// normalizeKey canonicalises the name part of a header (unit/basis already
// split off by parseCSV) to a column key.
func normalizeKey(s string) string {
	name := strings.ToLower(strings.TrimSpace(s))
	name = strings.ReplaceAll(name, " ", "_")
	if i := strings.IndexByte(name, '['); i >= 0 {
		name = name[:i]
	}
	switch name {
	case "time", "t", "furnace_time", "furnace_time_s":
		return "t"
	case "m", "mass", "sample_mass":
		return "mass"
	case "ts", "t_samp", "sample_temperature", "sample_temp":
		return "ts"
	case "tf", "t_furn", "furnace_temperature", "furnace_temp", "setpoint":
		return "tf"
	case "gas", "atmosphere", "gas_name":
		return "gas"
	case "event", "valve", "valve_event":
		return "event"
	case "slope", "drift_rate", "drift":
		return "slope"
	case "intercept":
		return "intercept"
	case "t0", "start":
		return "t0"
	case "t1", "end":
		return "t1"
	case "at":
		return "at"
	case "note", "comment":
		return "note"
	}
	return name
}

// colSpec declares the recognised columns of each section.
type colSpec struct {
	key      string
	kind     string // time | temp | mass | rate | text | number
	required bool
}

var sectionSpecs = map[string][]colSpec{
	KindSamples: {
		{"t", "time", true}, {"mass", "mass", true}, {"ts", "temp", false},
	},
	KindFurnace: {
		{"t", "time", true}, {"tf", "temp", true},
	},
	KindGas: {
		{"t", "time", true}, {"gas", "text", true}, {"event", "text", false},
	},
	KindBlank: {
		{"t", "time", true}, {"mass", "mass", true},
	},
	KindDrift: {
		{"t0", "time", true}, {"t1", "time", true},
		{"slope", "rate", true}, {"intercept", "mass", false},
	},
	KindInitialMass: {
		{"mass", "mass", true}, {"at", "time", false}, {"note", "text", false},
	},
}

// Validate converts a parsed table into a RawSection plus row statistics.
func Validate(kind string, t *Table) (*RawSection, int, int, []string) {
	spec, ok := sectionSpecs[kind]
	if !ok {
		return nil, 0, 0, []string{"unknown section kind: " + kind}
	}
	var diags []string
	keyIdx := map[string]int{}
	for ci, col := range t.Columns {
		key := normalizeKey(col.Name)
		if _, dup := keyIdx[key]; dup && key != "" {
			continue
		}
		keyIdx[key] = ci
	}
	for _, cs := range spec {
		idx, found := keyIdx[cs.key]
		if !found {
			if cs.required {
				return nil, 0, 0, []string{fmt.Sprintf("%s: required column %q is missing", kind, cs.key)}
			}
			continue
		}
		col := t.Columns[idx]
		if cs.kind == "time" && col.Unit != "" && !units.ValidUnit(col.Unit, "time") {
			return nil, 0, 0, []string{fmt.Sprintf("%s: column %q declares unsupported time unit %q", kind, cs.key, col.Unit)}
		}
		if cs.kind == "mass" && col.Unit != "" && !units.ValidUnit(col.Unit, "mass") {
			return nil, 0, 0, []string{fmt.Sprintf("%s: column %q declares unsupported mass unit %q", kind, cs.key, col.Unit)}
		}
		if cs.kind == "temp" && col.Unit != "" && !units.ValidUnit(col.Unit, "temperature") {
			return nil, 0, 0, []string{fmt.Sprintf("%s: column %q declares unsupported temperature unit %q", kind, cs.key, col.Unit)}
		}
	}
	if len(t.Rows) == 0 {
		return nil, 0, 0, []string{kind + ": section contains zero rows"}
	}

	out := &RawSection{Kind: kind, SourceName: t.SourceName, Columns: t.Columns, Rows: [][]Cell{}}
	accepted, rejected := 0, 0
	for ri, row := range t.Rows {
		rowOK := true
		for _, cs := range spec {
			idx, found := keyIdx[cs.key]
			if !found || idx >= len(row) {
				continue
			}
			cell := &row[idx]
			raw := strings.TrimSpace(cell.Raw)
			if raw == "" || strings.EqualFold(raw, "NA") || raw == "—" || raw == "-" {
				cell.Value = nil // explicit missing point; preserved
				continue
			}
			switch cs.kind {
			case "text":
				v, err := strconv.ParseFloat(raw, 64)
				_ = v
				if err == nil {
					diags = append(diags, fmt.Sprintf("%s row %d column %q: text value looks numeric", kind, ri+1, cs.key))
				}
				continue
			case "time", "mass", "temp", "rate", "number":
				v, clean, ok := units.ParseFloat(raw)
				if !ok {
					cell.Diag = fmt.Sprintf("%s: not a number (%q)", cs.key, clean)
					diags = append(diags, fmt.Sprintf("%s row %d column %q: %s", kind, ri+1, cs.key, cell.Diag))
					if cs.required {
						rowOK = false
					}
					continue
				}
				cell.Value = &v
			}
		}
		if !rowOK {
			rejected++
		} else {
			accepted++
		}
		out.Rows = append(out.Rows, row)
	}
	if accepted == 0 {
		return nil, accepted, rejected, append(diags, kind+": no usable rows after validation")
	}
	return out, accepted, rejected, diags
}

var _ = json.RawMessage(nil)
