// Package tga implements parsing (raw layer text -> normalized domain
// series) and the decomposition pipeline. Raw text is always kept by
// the caller; this package only produces the normalized, derived view
// plus diagnostics for anything it cannot convert.
package tga

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Diagnostic describes one unconvertible or malformed value. The raw
// value stays in place in the stored raw text; it is only excluded
// from the normalized series.
type Diagnostic struct {
	Layer  string `json:"layer"`
	Line   int    `json:"line"`
	Column string `json:"column"`
	Value  string `json:"value"`
	Reason string `json:"reason"`
}

func (d Diagnostic) String() string {
	return fmt.Sprintf("%s:%d col=%s value=%q: %s", d.Layer, d.Line, d.Column, d.Value, d.Reason)
}

// Normalized domain series (canonical units: s, degC, mg).
type SamplePoint struct {
	TS    float64 `json:"t_s"`
	TempC float64 `json:"temp_c"`
	Mass  float64 `json:"mass_mg"`
}
type DriftPoint struct {
	TS    float64 `json:"t_s"`
	Drift float64 `json:"drift_mg"`
}
type FurnacePoint struct {
	TS       float64 `json:"t_s"`
	Setpoint float64 `json:"setpoint_c"`
}
type GasEvent struct {
	TS    float64 `json:"t_s"`
	Valve string  `json:"valve"`
	Gas   string  `json:"gas"`
}
type BlankPoint struct {
	TS    float64 `json:"t_s"`
	Blank float64 `json:"blank_mg"`
}
type InitialMass struct {
	ValueMg       float64 `json:"value_mg"`
	UncertaintyMg float64 `json:"uncertainty_mg"`
	Note          string  `json:"note"`
}

// ParsedLayer is the normalized result for one layer.
type ParsedLayer struct {
	Layer       string
	Samples     []SamplePoint
	Drift       []DriftPoint
	Furnace     []FurnacePoint
	Gas         []GasEvent
	Blank       []BlankPoint
	Mass        *InitialMass
	Diagnostics []Diagnostic
}

// Section is one "== layer: <name>" block of a batch submission.
type Section struct {
	Layer string
	Text  string
	Err   string // set when the section header itself is invalid
}

var knownLayers = map[string]bool{
	"sample_points": true, "drift_correction": true, "furnace_program": true,
	"gas_events": true, "blank_curve": true, "initial_mass": true,
}

// SplitBatch splits a pasted batch submission into per-layer sections.
// Sections with unknown layer names are kept with Err set so the
// caller can record a partial import instead of rejecting everything.
func SplitBatch(text string) []Section {
	var out []Section
	var cur *Section
	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimSpace(cur.Text)
			out = append(out, *cur)
		}
	}
	for _, line := range strings.Split(text, "\n") {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "==") {
			flush()
			cur = &Section{}
			name := strings.TrimSpace(strings.TrimPrefix(trim, "=="))
			name = strings.TrimPrefix(name, "layer:")
			name = strings.TrimSpace(name)
			if !knownLayers[name] {
				cur.Layer = name
				cur.Err = fmt.Sprintf("unknown layer %q", name)
			} else {
				cur.Layer = name
			}
			continue
		}
		if cur == nil {
			if trim != "" {
				cur = &Section{Layer: "", Err: "content before first layer header"}
			} else {
				continue
			}
		}
		cur.Text += line + "\n"
	}
	flush()
	return out
}

// unit conversion tables
var timeToS = map[string]float64{"s": 1, "min": 60, "h": 3600}
var massToMg = map[string]float64{"mg": 1, "g": 1000, "ug": 0.001}

func convertTemp(v float64, unit string) (float64, bool) {
	switch unit {
	case "C":
		return v, true
	case "K":
		return v - 273.15, true
	}
	return 0, false
}

// colSpec parses a header column like "time_s" or "mass_g".
type colSpec struct {
	quantity string // time | temp | mass | drift | blank | setpoint | valve | gas | note | uncertainty
	unit     string
}

func parseHeader(header string) ([]colSpec, error) {
	parts := strings.Split(strings.TrimSpace(header), ",")
	specs := make([]colSpec, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "valve" || p == "gas" || p == "note" {
			specs = append(specs, colSpec{quantity: p})
			continue
		}
		idx := strings.LastIndex(p, "_")
		if idx <= 0 {
			return nil, fmt.Errorf("bad column header %q (want quantity_unit)", p)
		}
		specs = append(specs, colSpec{quantity: p[:idx], unit: p[idx+1:]})
	}
	return specs, nil
}

// ParseLayer parses one layer's raw text into normalized series.
// Format errors (bad numbers, missing columns) return an error and
// the layer is rejected as a whole; unit problems only produce
// diagnostics and skip the offending cells.
func ParseLayer(layer, text string) (*ParsedLayer, error) {
	pl := &ParsedLayer{Layer: layer}
	lines := []string{}
	for _, l := range strings.Split(text, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") {
			continue
		}
		lines = append(lines, l)
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("layer %s: empty content", layer)
	}
	specs, err := parseHeader(lines[0])
	if err != nil {
		return nil, fmt.Errorf("layer %s: %v", layer, err)
	}
	rows := [][]string{}
	for i, l := range lines[1:] {
		cells := strings.Split(l, ",")
		if len(cells) != len(specs) {
			return nil, fmt.Errorf("layer %s line %d: got %d cells, want %d", layer, i+2, len(cells), len(specs))
		}
		for j := range cells {
			cells[j] = strings.TrimSpace(cells[j])
		}
		rows = append(rows, cells)
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("layer %s: header but no data rows", layer)
	}

	num := func(rowIdx, colIdx int) (float64, bool) {
		v := rows[rowIdx][colIdx]
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			pl.Diagnostics = append(pl.Diagnostics, Diagnostic{
				Layer: layer, Line: rowIdx + 2, Column: specs[colIdx].quantity + "_" + specs[colIdx].unit,
				Value: v, Reason: "not a number"})
			return 0, false
		}
		return f, true
	}
	// convNum converts a numeric cell to canonical units; ok=false with
	// diagnostic when the unit is unknown (value left in raw only).
	convNum := func(rowIdx, colIdx int) (float64, bool) {
		sp := specs[colIdx]
		f, ok := num(rowIdx, colIdx)
		if !ok {
			return 0, false
		}
		bad := func(what string) (float64, bool) {
			pl.Diagnostics = append(pl.Diagnostics, Diagnostic{
				Layer: layer, Line: rowIdx + 2, Column: sp.quantity + "_" + sp.unit,
				Value: rows[rowIdx][colIdx], Reason: fmt.Sprintf("cannot convert %s unit %q", what, sp.unit)})
			return 0, false
		}
		switch sp.quantity {
		case "time":
			k, ok := timeToS[sp.unit]
			if !ok {
				return bad("time")
			}
			return f * k, true
		case "mass", "drift", "blank", "uncertainty":
			k, ok := massToMg[sp.unit]
			if !ok {
				return bad("mass")
			}
			return f * k, true
		case "temp", "setpoint":
			c, ok := convertTemp(f, sp.unit)
			if !ok {
				return bad("temperature")
			}
			return c, true
		}
		return f, true
	}

	colOf := func(q string) int {
		for i, sp := range specs {
			if sp.quantity == q {
				return i
			}
		}
		return -1
	}
	need := func(qs ...string) error {
		for _, q := range qs {
			if colOf(q) < 0 {
				return fmt.Errorf("layer %s: missing %s column", layer, q)
			}
		}
		return nil
	}

	switch layer {
	case "sample_points":
		if err := need("time", "temp", "mass"); err != nil {
			return nil, err
		}
		ci, ct, cm := colOf("time"), colOf("temp"), colOf("mass")
		for r := range rows {
			t, ok1 := convNum(r, ci)
			tp, ok2 := convNum(r, ct)
			m, ok3 := convNum(r, cm)
			if ok1 && ok2 && ok3 {
				pl.Samples = append(pl.Samples, SamplePoint{TS: t, TempC: tp, Mass: m})
			}
		}
		sort.Slice(pl.Samples, func(i, j int) bool { return pl.Samples[i].TS < pl.Samples[j].TS })
	case "drift_correction":
		if err := need("time", "drift"); err != nil {
			return nil, err
		}
		ci, cd := colOf("time"), colOf("drift")
		for r := range rows {
			t, ok1 := convNum(r, ci)
			d, ok2 := convNum(r, cd)
			if ok1 && ok2 {
				pl.Drift = append(pl.Drift, DriftPoint{TS: t, Drift: d})
			}
		}
		sort.Slice(pl.Drift, func(i, j int) bool { return pl.Drift[i].TS < pl.Drift[j].TS })
	case "furnace_program":
		if err := need("time", "setpoint"); err != nil {
			return nil, err
		}
		ci, cs := colOf("time"), colOf("setpoint")
		for r := range rows {
			t, ok1 := convNum(r, ci)
			s, ok2 := convNum(r, cs)
			if ok1 && ok2 {
				pl.Furnace = append(pl.Furnace, FurnacePoint{TS: t, Setpoint: s})
			}
		}
		sort.Slice(pl.Furnace, func(i, j int) bool { return pl.Furnace[i].TS < pl.Furnace[j].TS })
	case "gas_events":
		if err := need("time", "valve", "gas"); err != nil {
			return nil, err
		}
		ci, cv, cg := colOf("time"), colOf("valve"), colOf("gas")
		for r := range rows {
			t, ok := convNum(r, ci)
			if ok {
				pl.Gas = append(pl.Gas, GasEvent{TS: t, Valve: rows[r][cv], Gas: rows[r][cg]})
			}
		}
		sort.Slice(pl.Gas, func(i, j int) bool { return pl.Gas[i].TS < pl.Gas[j].TS })
	case "blank_curve":
		if err := need("time", "blank"); err != nil {
			return nil, err
		}
		ci, cb := colOf("time"), colOf("blank")
		for r := range rows {
			t, ok1 := convNum(r, ci)
			b, ok2 := convNum(r, cb)
			if ok1 && ok2 {
				pl.Blank = append(pl.Blank, BlankPoint{TS: t, Blank: b})
			}
		}
		sort.Slice(pl.Blank, func(i, j int) bool { return pl.Blank[i].TS < pl.Blank[j].TS })
	case "initial_mass":
		if err := need("mass"); err != nil {
			return nil, err
		}
		cm := colOf("mass")
		cu := colOf("uncertainty")
		cn := colOf("note")
		m, ok := convNum(0, cm)
		if !ok {
			return nil, fmt.Errorf("layer initial_mass: mass value not convertible")
		}
		im := &InitialMass{ValueMg: m}
		if cu >= 0 {
			if u, ok := convNum(0, cu); ok {
				im.UncertaintyMg = u
			}
		}
		if cn >= 0 {
			im.Note = rows[0][cn]
		}
		pl.Mass = im
	}
	return pl, nil
}
