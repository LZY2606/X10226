package domain

import (
	"bufio"
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// ColSpec describes one expected column: key, accepted header synonyms, and
// quantity kind for normalization ("" means keep raw string).
type ColSpec struct {
	Key   string
	Names []string
	Kind  string
}

var specs = map[string][]ColSpec{
	SrcSamplePoints: {
		{"t", []string{"furnace_time", "time", "t", "炉时间"}, "time"},
		{"temp", []string{"temperature", "temp", "furnace_temp", "温度"}, "temp"},
		{"mass", []string{"mass", "apparent_mass", "m", "质量"}, "mass"},
	},
	SrcDrift: {
		{"t", []string{"t", "time", "furnace_time"}, "time"},
		{"drift", []string{"drift", "baseline_drift", "drift_mg", "漂移"}, "mass"},
	},
	SrcFurnace: {
		{"start", []string{"start_t", "start", "from_t"}, "time"},
		{"end", []string{"end_t", "end", "to_t"}, "time"},
		{"target", []string{"target_temp", "target", "setpoint", "rate_temp"}, "temp"},
		{"rate", []string{"rate", "heating_rate", "ramp"}, "rate"},
	},
	SrcGasValve: {
		{"t", []string{"t", "time", "switch_t", "valve_t"}, "time"},
		{"gas", []string{"gas", "atmosphere", "gas_name", "气氛"}, ""},
		{"flow", []string{"flow", "flow_rate"}, "flow"},
	},
	SrcBlank: {
		{"t", []string{"t", "time"}, "time"},
		{"blank", []string{"blank_mass", "blank", "baseline_mass"}, "mass"},
	},
	SrcInitialMass: {
		{"sample", []string{"sample", "sample_id", "name"}, ""},
		{"m0", []string{"initial_mass", "m0", "mass0", "初重"}, "mass"},
		{"basis", []string{"basis", "scale", "balance", "天平"}, ""},
	},
}

// ParseResult reports per-row acceptance for the partial-import semantics.
type ParseResult struct {
	Header   []string
	UnitDecl map[string]string
	Rows     []Row
	Accepted int
	Rejected int
	Fatal    string // non-recoverable format error
}

// ParseCSV parses one raw layer. Header units are taken from parenthesized
// annotations such as "mass (mg)". Invalid rows are rejected individually.
func ParseCSV(kind string, data []byte) ParseResult {
	res := ParseResult{UnitDecl: map[string]string{}}
	if len(bytes.TrimSpace(data)) == 0 {
		res.Fatal = "empty file"
		return res
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.FieldsPerRecord = -1
	r.TrimLeadingSpace = true
	r.LazyQuotes = true
	recs, err := r.ReadAll()
	if err != nil {
		// still try line-oriented recovery so the caller gets fragments
		res.Fatal = fmt.Sprintf("csv parse error: %v", err)
	}
	if len(recs) < 2 {
		if res.Fatal == "" {
			res.Fatal = "need header plus at least one data row"
		}
		return res
	}
	cols := specs[kind]
	if cols == nil {
		res.Fatal = "unknown source kind"
		return res
	}
	rawHeader := recs[0]
	idx := map[string]int{}
	for ci, h := range rawHeader {
		name, unit := splitHeaderUnit(strings.TrimSpace(h))
		key := matchCol(cols, name)
		if key == "" {
			continue
		}
		idx[key] = ci
		res.Header = append(res.Header, strings.TrimSpace(h))
		if unit != "" {
			res.UnitDecl[key] = unit
		}
	}
	var missing []string
	for _, c := range cols {
		if _, ok := idx[c.Key]; !ok {
			missing = append(missing, c.Names[0])
		}
	}
	if len(missing) > 0 {
		res.Fatal = "missing required columns: " + strings.Join(missing, ", ")
		return res
	}
	for li := 1; li < len(recs); li++ {
		rec := recs[li]
		if isBlankRecord(rec) {
			continue
		}
		row := Row{Line: li + 1, Raw: map[string]string{}, Cells: map[string]Q{}}
		var problems []string
		for _, c := range cols {
			ci := idx[c.Key]
			var text string
			if ci < len(rec) {
				text = strings.TrimSpace(rec[ci])
			}
			row.Raw[c.Key] = text
			if c.Kind == "" {
				if text == "" {
					problems = append(problems, c.Key+" empty")
				}
				continue
			}
			q := ParseQ(text, res.UnitDecl[c.Key], c.Kind)
			row.Cells[c.Key] = q
			if !q.Valid {
				problems = append(problems, fmt.Sprintf("%s: %s", c.Key, q.Diagnostic))
			}
		}
		if extra := validateRow(kind, row); extra != "" {
			problems = append(problems, extra)
		}
		if len(problems) > 0 {
			row.Accepted = false
			row.Diagnostic = strings.Join(problems, "; ")
			res.Rejected++
		} else {
			row.Accepted = true
			res.Accepted++
		}
		res.Rows = append(res.Rows, row)
	}
	if res.Accepted == 0 {
		res.Fatal = "no valid data rows parsed"
	}
	return res
}

func validateRow(kind string, row Row) string {
	switch kind {
	case SrcSamplePoints:
		t := row.Cells["t"]; m := row.Cells["mass"]
		if m.Valid && m.Value < 0 {
			return "negative apparent mass"
		}
		_ = t
	case SrcDrift:
		if d := row.Cells["drift"]; d.Valid && (d.Value > 1e5 || d.Value < -1e5) {
			return "drift value implausible"
		}
	case SrcFurnace:
		s, e := row.Cells["start"], row.Cells["end"]
		if s.Valid && e.Valid && e.Value < s.Value {
			return "furnace segment ends before it starts"
		}
		if r := row.Cells["rate"]; r.Valid && r.Value < 0 {
			return "negative heating rate"
		}
	case SrcInitialMass:
		if m := row.Cells["m0"]; m.Valid && m.Value <= 0 {
			return "initial mass must be positive"
		}
	case SrcGasValve:
		if g := strings.ToLower(strings.TrimSpace(row.Raw["gas"])); g != "" {
			switch g {
			case "n2", "ar", "air", "o2", "he", "co2":
			default:
				return "unsupported gas: " + row.Raw["gas"]
			}
		}
	}
	return ""
}

func matchCol(cols []ColSpec, name string) string {
	name = strings.ToLower(name)
	for _, c := range cols {
		for _, n := range c.Names {
			if name == strings.ToLower(n) {
				return c.Key
			}
		}
	}
	return ""
}

func splitHeaderUnit(h string) (string, string) {
	i := strings.IndexByte(h, '(')
	j := strings.IndexByte(h, ')')
	if i >= 0 && j > i {
		return strings.TrimSpace(h[:i]), strings.TrimSpace(h[i+1 : j])
	}
	// also accept "mass / mg"
	if k := strings.Index(h, "/"); k >= 0 {
		return strings.TrimSpace(h[:k]), strings.TrimSpace(h[k+1:])
	}
	return h, ""
}

func isBlankRecord(rec []string) bool {
	for _, f := range rec {
		if strings.TrimSpace(f) != "" {
			return false
		}
	}
	return true
}

// ReadText normalizes CRLF and verifies the payload is readable text.
func ReadText(r io.Reader) ([]byte, error) {
	br := bufio.NewReader(r)
	b, err := io.ReadAll(br)
	if err != nil {
		return nil, err
	}
	return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")), nil
}

// ParseScalarFloat is used for quick external checks/tests.
func ParseScalarFloat(s string) (float64, error) {
	return strconv.ParseFloat(strings.TrimSpace(s), 64)
}
