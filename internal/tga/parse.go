package tga

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Layer kinds recognised by the import layer.
const (
	KindRawPoints    = "raw_points"
	KindDrift        = "drift_correction"
	KindFurnace      = "furnace_program"
	KindGas          = "gas_events"
	KindBlank        = "blank_curve"
	KindInitialMass  = "initial_mass"
)

// Kinds lists all layer kinds in canonical order.
var Kinds = []string{KindRawPoints, KindDrift, KindFurnace, KindGas, KindBlank, KindInitialMass}

// Record is one imported row. Fields keeps the original string
// representation per column; normalisation happens only in the pipeline.
type Record struct {
	Seq    int               `json:"seq"`
	Fields map[string]string `json:"fields"`
	Raw    string            `json:"raw"`
	OK     bool              `json:"ok"`
	Diag   string            `json:"diag,omitempty"`
}

// numericUnitColumns maps column name -> unit for import-time number checks.
var numericUnitColumns = map[string]string{
	"time_s": "s", "time_min": "min",
	"temp_C": "C", "temp_K": "K",
	"mass_mg": "mg", "mass_g": "g",
	"drift_mg": "mg",
	"setpoint_C": "C",
	"flow_mL_min": "mL_min",
	"mass_pct": "pct",
}

// ParseCSV parses a headered CSV layer. Rows with structural or numeric
// format errors are kept as not-ok records with diagnostics (partial import).
func ParseCSV(raw string) ([]Record, []Diagnostic) {
	var diags []Diagnostic
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	var rows []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			rows = append(rows, l)
		}
	}
	if len(rows) == 0 {
		return nil, []Diagnostic{{Where: "file", Raw: "", Reason: "empty payload"}}
	}
	header := strings.Split(strings.TrimSpace(rows[0]), ",")
	for i := range header {
		header[i] = strings.TrimSpace(header[i])
	}
	var recs []Record
	for i, line := range rows[1:] {
		rec := Record{Seq: i, Fields: map[string]string{}, Raw: line, OK: true}
		cols := strings.Split(line, ",")
		if len(cols) != len(header) {
			rec.OK = false
			rec.Diag = fmt.Sprintf("column count %d, want %d", len(cols), len(header))
			diags = append(diags, Diagnostic{Where: fmt.Sprintf("row %d", i+2), Raw: line, Reason: rec.Diag})
		}
		for j, h := range header {
			if j < len(cols) {
				rec.Fields[h] = strings.TrimSpace(cols[j])
			}
		}
		if rec.OK {
			for col, v := range rec.Fields {
				if _, numeric := numericUnitColumns[col]; numeric {
					if _, err := strconv.ParseFloat(v, 64); err != nil {
						rec.OK = false
						rec.Diag = fmt.Sprintf("column %s: %q is not a number", col, v)
						diags = append(diags, Diagnostic{Where: fmt.Sprintf("row %d", i+2), Raw: line, Reason: rec.Diag})
						break
					}
				}
			}
		}
		recs = append(recs, rec)
	}
	return recs, diags
}

// ParseInitialMass parses a JSON initial-mass layer:
// {"mass": "12.340", "unit": "mg", "basis": "as-received"}.
func ParseInitialMass(raw string) ([]Record, []Diagnostic) {
	var doc map[string]string
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		return []Record{{Seq: 0, Fields: map[string]string{}, Raw: raw, OK: false,
			Diag: "invalid JSON: " + err.Error()}},
			[]Diagnostic{{Where: "file", Raw: raw, Reason: "invalid JSON"}}
	}
	rec := Record{Seq: 0, Fields: doc, Raw: raw, OK: true}
	if _, err := strconv.ParseFloat(doc["mass"], 64); err != nil {
		rec.OK = false
		rec.Diag = fmt.Sprintf("mass %q is not a number", doc["mass"])
	}
	if doc["unit"] == "" {
		rec.OK = false
		rec.Diag = "missing unit"
	}
	return []Record{rec}, nil
}

// Parse dispatches on layer kind.
func Parse(kind, raw string) ([]Record, []Diagnostic, error) {
	switch kind {
	case KindRawPoints, KindDrift, KindFurnace, KindGas, KindBlank:
		r, d := ParseCSV(raw)
		return r, d, nil
	case KindInitialMass:
		r, d := ParseInitialMass(raw)
		return r, d, nil
	}
	return nil, nil, fmt.Errorf("unknown layer kind %q", kind)
}
