package tga

import (
	"fmt"
	"strconv"
	"strings"
)

// ---------------------------------------------------------------------------
// Unit handling. Raw text is preserved; conversion happens here, in the
// derived layer. Unconvertible values stay in place with a diagnostic.
// ---------------------------------------------------------------------------

// unitDef maps a spelled unit to (normalised unit, dimension, scale, offset).
// value_norm = value_raw*scale + offset.
type unitDef struct {
	norm   string
	dim    string
	scale  float64
	offset float64
}

var unitTable = map[string]unitDef{
	"mg":     {"mg", "mass", 1, 0},
	"g":      {"mg", "mass", 1000, 0},
	"ug":     {"mg", "mass", 0.001, 0},
	"C":      {"C", "temp", 1, 0},
	"degC":   {"C", "temp", 1, 0},
	"K":      {"C", "temp", 1, -273.15},
	"s":      {"s", "time", 1, 0},
	"sec":    {"s", "time", 1, 0},
	"min":    {"s", "time", 60, 0},
	"h":      {"s", "time", 3600, 0},
	"mL/min": {"mL/min", "flow", 1, 0},
	"sccm":   {"mL/min", "flow", 1, 0},
	"L/min":  {"mL/min", "flow", 1000, 0},
}

// ParseQuantity converts raw text such as "12.5 mg" or "673.15 K" into
// a normalised Quantity of the wanted dimension ("mass", "temp",
// "time", "flow"). On failure the returned Quantity keeps Raw with
// OK=false and a diagnostic explains why; nothing is dropped.
func ParseQuantity(raw, wantDim, basis, where string) (Quantity, *Diagnostic) {
	q := Quantity{Raw: raw, Basis: basis}
	fields := strings.Fields(strings.TrimSpace(raw))
	if len(fields) == 0 {
		d := Diagnostic{Code: "quantity.empty", Message: "empty quantity", Where: where}
		return q, &d
	}
	num, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		d := Diagnostic{Code: "quantity.number", Message: fmt.Sprintf("cannot parse number in %q", raw), Where: where}
		return q, &d
	}
	unit := ""
	if len(fields) > 1 {
		unit = fields[1]
	}
	def, ok := unitTable[unit]
	if !ok {
		d := Diagnostic{Code: "quantity.unit", Message: fmt.Sprintf("unknown unit %q in %q", unit, raw), Where: where}
		return q, &d
	}
	if def.dim != wantDim {
		d := Diagnostic{Code: "quantity.dimension", Message: fmt.Sprintf("unit %q is %s, want %s", unit, def.dim, wantDim), Where: where}
		return q, &d
	}
	q.Value = num*def.scale + def.offset
	q.Unit = def.norm
	q.OK = true
	return q, nil
}

// ---------------------------------------------------------------------------
// Layer text format.
//
// A layer document is small and copyable:
//
//	layer: raw_samples
//	basis: furnace-clock
//	columns: time[s], mass[mg], temp[C]
//	0, 12.500, 30.1
//	1, 12.499, 30.3
//
// Lines starting with '#' are comments. Every format problem is an
// explicit error with a line number; nothing is silently skipped.
// ---------------------------------------------------------------------------

// LayerDoc is one parsed layer document: the typed payload plus the
// diagnostics collected while parsing.
type LayerDoc struct {
	Layer       string
	Basis       string
	Payload     any // one of the *Layer structs
	Diagnostics []Diagnostic
}

// ParseLayer parses a single layer document. Hard format errors
// (unknown layer, missing header, bad row) are returned as error;
// unconvertible values stay in the payload with diagnostics.
func ParseLayer(text string) (*LayerDoc, error) {
	var (
		layer, basis string
		cols         []colSpec
		rows         [][]string
	)
	seenHeader := false
	for n, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		where := fmt.Sprintf("line %d", n+1)
		switch {
		case strings.HasPrefix(line, "layer:"):
			layer = strings.TrimSpace(strings.TrimPrefix(line, "layer:"))
			if !validLayer(layer) {
				return nil, fmt.Errorf("%s: unknown layer %q (want one of %s)", where, layer, strings.Join(LayerNames, ", "))
			}
			seenHeader = true
		case strings.HasPrefix(line, "basis:"):
			basis = strings.TrimSpace(strings.TrimPrefix(line, "basis:"))
		case strings.HasPrefix(line, "columns:"):
			spec := strings.TrimSpace(strings.TrimPrefix(line, "columns:"))
			for _, c := range strings.Split(spec, ",") {
				cols = append(cols, parseColSpec(strings.TrimSpace(c)))
			}
		default:
			if !seenHeader {
				return nil, fmt.Errorf("%s: data before 'layer:' header", where)
			}
			if len(cols) == 0 {
				return nil, fmt.Errorf("%s: data before 'columns:' header", where)
			}
			fields := strings.Split(line, ",")
			for i := range fields {
				fields[i] = strings.TrimSpace(fields[i])
			}
			if len(fields) != len(cols) {
				return nil, fmt.Errorf("%s: got %d fields, want %d (%s)", where, len(fields), len(cols), line)
			}
			rows = append(rows, fields)
		}
	}
	if layer == "" {
		return nil, fmt.Errorf("missing 'layer:' header")
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("missing 'columns:' header")
	}
	if basis == "" {
		basis = "furnace-clock"
	}
	doc := &LayerDoc{Layer: layer, Basis: basis}
	payload, err := buildPayload(layer, cols, rows, basis, &doc.Diagnostics)
	if err != nil {
		return nil, err
	}
	doc.Payload = payload
	return doc, nil
}

func validLayer(name string) bool {
	for _, n := range LayerNames {
		if n == name {
			return true
		}
	}
	return false
}

// colSpec is one column: a field name plus its declared unit, e.g.
// "mass[mg]" or a plain token for strings ("gas").
type colSpec struct {
	name string
	unit string // "" => plain string column
}

func parseColSpec(s string) colSpec {
	if i := strings.Index(s, "["); i >= 0 && strings.HasSuffix(s, "]") {
		return colSpec{name: s[:i], unit: s[i+1 : len(s)-1]}
	}
	return colSpec{name: s}
}

// quantityOf converts one cell using its declared unit. The declared
// unit determines the dimension; the cell may still spell its own
// unit ("1.5 min") which must be convertible to the declared one.
func quantityOf(cell string, col colSpec, basis, where string, diags *[]Diagnostic) Quantity {
	raw := cell
	if col.unit != "" && len(strings.Fields(cell)) == 1 {
		raw = cell + " " + col.unit
	}
	dim := unitTable[col.unit].dim
	q, d := ParseQuantity(raw, dim, basis, where)
	if d != nil {
		*diags = append(*diags, *d)
	}
	return q
}

func buildPayload(layer string, cols []colSpec, rows [][]string, basis string, diags *[]Diagnostic) (any, error) {
	colIdx := func(name string) int {
		for i, c := range cols {
			if c.name == name {
				return i
			}
		}
		return -1
	}
	need := func(names ...string) error {
		for _, n := range names {
			if colIdx(n) < 0 {
				return fmt.Errorf("layer %s: missing column %q", layer, n)
			}
		}
		return nil
	}
	q := func(row []string, name, where string) Quantity {
		i := colIdx(name)
		return quantityOf(row[i], cols[i], basis, where, diags)
	}
	s := func(row []string, name string) string {
		return row[colIdx(name)]
	}
	switch layer {
	case LayerRawSamples:
		if err := need("time", "mass", "temp"); err != nil {
			return nil, err
		}
		out := &RawSamplesLayer{}
		for i, r := range rows {
			where := fmt.Sprintf("row %d", i+1)
			out.Points = append(out.Points, RawSample{
				Time: q(r, "time", where),
				Mass: q(r, "mass", where),
				Temp: q(r, "temp", where),
			})
		}
		return out, nil
	case LayerBalanceDrift:
		if err := need("time", "drift"); err != nil {
			return nil, err
		}
		out := &BalanceDriftLayer{}
		for i, r := range rows {
			where := fmt.Sprintf("row %d", i+1)
			out.Points = append(out.Points, DriftPoint{
				Time: q(r, "time", where),
				Drift: q(r, "drift", where),
			})
		}
		return out, nil
	case LayerFurnaceProgram:
		if err := need("startTime", "startTemp", "endTime", "endTemp"); err != nil {
			return nil, err
		}
		out := &FurnaceProgramLayer{}
		for i, r := range rows {
			where := fmt.Sprintf("row %d", i+1)
			out.Segments = append(out.Segments, FurnaceSegment{
				StartTime: q(r, "startTime", where),
				StartTemp: q(r, "startTemp", where),
				EndTime:   q(r, "endTime", where),
				EndTemp:   q(r, "endTemp", where),
			})
		}
		return out, nil
	case LayerGasEvents:
		if err := need("time", "valve", "gas", "flow"); err != nil {
			return nil, err
		}
		out := &GasEventsLayer{}
		for i, r := range rows {
			where := fmt.Sprintf("row %d", i+1)
			out.Events = append(out.Events, GasEvent{
				Time:  q(r, "time", where),
				Valve: s(r, "valve"),
				Gas:   s(r, "gas"),
				Flow:  q(r, "flow", where),
			})
		}
		return out, nil
	case LayerBlankCurve:
		if err := need("time", "mass"); err != nil {
			return nil, err
		}
		out := &BlankCurveLayer{}
		for i, r := range rows {
			where := fmt.Sprintf("row %d", i+1)
			out.Points = append(out.Points, BlankPoint{
				Time: q(r, "time", where),
				Mass: q(r, "mass", where),
			})
		}
		return out, nil
	case LayerInitialMass:
		if err := need("mass"); err != nil {
			return nil, err
		}
		if len(rows) != 1 {
			return nil, fmt.Errorf("layer %s: want exactly 1 row, got %d", layer, len(rows))
		}
		return &InitialMassLayer{Mass: q(rows[0], "mass", "row 1")}, nil
	}
	return nil, fmt.Errorf("unknown layer %q", layer)
}

// SplitBatch splits a multi-layer batch document. Layers are
// separated by lines containing only "---". Each part is parsed
// independently so one broken layer never sinks the others.
func SplitBatch(text string) []string {
	var parts []string
	var cur strings.Builder
	flush := func() {
		if strings.TrimSpace(cur.String()) != "" {
			parts = append(parts, cur.String())
		}
		cur.Reset()
	}
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "---" {
			flush()
			continue
		}
		cur.WriteString(line)
		cur.WriteString("\n")
	}
	flush()
	return parts
}
