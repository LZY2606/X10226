package domain

import (
	"fmt"
	"strconv"
	"strings"
)

// Parsed layer payloads (normalized views are derived later; these structs
// keep values together with their original units).

type RawPoint struct {
	Line int     `json:"line"`
	Time float64 `json:"time"`
	Temp float64 `json:"temp"`
	Mass float64 `json:"mass"`
}

type RawPointsDoc struct {
	TimeUnit string     `json:"time_unit"`
	TempUnit string     `json:"temp_unit"`
	MassUnit string     `json:"mass_unit"`
	Points   []RawPoint `json:"points"`
}

type DriftPoint struct {
	Line   int     `json:"line"`
	Time   float64 `json:"time"`
	Offset float64 `json:"offset"` // balance drift offset
}

type DriftDoc struct {
	TimeUnit string       `json:"time_unit"`
	MassUnit string       `json:"mass_unit"`
	Points   []DriftPoint `json:"points"`
}

type FurnaceSegment struct {
	Line   int     `json:"line"`
	Start  float64 `json:"start"`  // segment start time
	Ramp   float64 `json:"ramp"`   // per minute, toward target
	Target float64 `json:"target"` // target temperature
	Hold   float64 `json:"hold"`   // hold time at target after ramp
}

type FurnaceDoc struct {
	TimeUnit string           `json:"time_unit"`
	TempUnit string           `json:"temp_unit"`
	Start    float64          `json:"start_temp"`
	Segments []FurnaceSegment `json:"segments"`
}

type GasEvent struct {
	Line int     `json:"line"`
	Time float64 `json:"time"`
	Gas  string  `json:"gas"`
	Flow float64 `json:"flow"`
}

type GasDoc struct {
	TimeUnit string     `json:"time_unit"`
	FlowUnit string     `json:"flow_unit"`
	Events   []GasEvent `json:"events"`
}

type BlankPoint struct {
	Line int     `json:"line"`
	Temp float64 `json:"temp"`
	Mass float64 `json:"mass"`
}

type BlankDoc struct {
	TempUnit string       `json:"temp_unit"`
	MassUnit string       `json:"mass_unit"`
	Points   []BlankPoint `json:"points"`
}

type InitialMassDoc struct {
	PanTare    Quantity `json:"pan_tare"`
	SampleMass Quantity `json:"sample_mass"`
	Basis      string   `json:"basis"` // coordinate basis, e.g. as-received / dry
}

// ParseLayer parses raw text of a layer into a typed document. Format errors
// are returned as error (rejecting the submission); per-value conversion
// problems are recorded as diagnostics while the raw text stays untouched.
func ParseLayer(kind LayerKind, raw string) (any, []Diagnostic, error) {
	lines := strings.Split(raw, "\n")
	var header, body []string
	for i, ln := range lines {
		ln = strings.TrimSpace(ln)
		if ln == "" {
			continue
		}
		if strings.HasPrefix(ln, "#") {
			header = append(header, ln)
			continue
		}
		_ = i
		body = append(body, ln)
	}
	units := map[string]string{}
	for _, h := range header {
		for _, tok := range strings.Fields(strings.TrimPrefix(h, "#")) {
			if k, v, ok := strings.Cut(tok, "="); ok {
				units[k] = v
			}
		}
	}
	if len(body) == 0 {
		return nil, nil, fmt.Errorf("empty layer document")
	}
	// body[0] is the column header row.
	rows := body[1:]
	if len(rows) == 0 && kind != LayerInitialMass {
		return nil, nil, fmt.Errorf("no data rows")
	}
	switch kind {
	case LayerRawPoints:
		return parseRawPoints(units, rows)
	case LayerDrift:
		return parseDrift(units, rows)
	case LayerFurnace:
		return parseFurnace(units, rows)
	case LayerGasEvents:
		return parseGas(units, rows)
	case LayerBlank:
		return parseBlank(units, rows)
	case LayerInitialMass:
		return parseInitialMass(append(body[:0:0], body...))
	}
	return nil, nil, fmt.Errorf("unknown layer kind %q", kind)
}

func fields(row string) []string {
	parts := strings.Split(row, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func num(s string) (float64, error) { return strconv.ParseFloat(s, 64) }

func parseRawPoints(units map[string]string, rows []string) (any, []Diagnostic, error) {
	doc := &RawPointsDoc{
		TimeUnit: def(units, "time_unit", UnitTime),
		TempUnit: def(units, "temp_unit", UnitTemp),
		MassUnit: def(units, "mass_unit", UnitMass),
	}
	var diags []Diagnostic
	diags = append(diags, checkUnits(doc.TimeUnit, doc.TempUnit, doc.MassUnit)...)
	for i, r := range rows {
		f := fields(r)
		if len(f) != 3 {
			return nil, nil, fmt.Errorf("raw_points row %d: want 3 columns time,temp,mass got %d", i+1, len(f))
		}
		t, err1 := num(f[0])
		tp, err2 := num(f[1])
		m, err3 := num(f[2])
		if err1 != nil || err2 != nil || err3 != nil {
			return nil, nil, fmt.Errorf("raw_points row %d: unparsable number", i+1)
		}
		doc.Points = append(doc.Points, RawPoint{Line: i + 1, Time: t, Temp: tp, Mass: m})
	}
	return doc, diags, nil
}

func parseDrift(units map[string]string, rows []string) (any, []Diagnostic, error) {
	doc := &DriftDoc{TimeUnit: def(units, "time_unit", UnitTime), MassUnit: def(units, "mass_unit", "ug")}
	var diags []Diagnostic
	for i, r := range rows {
		f := fields(r)
		if len(f) != 2 {
			return nil, nil, fmt.Errorf("drift row %d: want time,offset", i+1)
		}
		t, e1 := num(f[0])
		o, e2 := num(f[1])
		if e1 != nil || e2 != nil {
			return nil, nil, fmt.Errorf("drift row %d: unparsable number", i+1)
		}
		doc.Points = append(doc.Points, DriftPoint{Line: i + 1, Time: t, Offset: o})
	}
	return doc, diags, nil
}

func parseFurnace(units map[string]string, rows []string) (any, []Diagnostic, error) {
	doc := &FurnaceDoc{TimeUnit: def(units, "time_unit", UnitTime), TempUnit: def(units, "temp_unit", UnitTemp)}
	var diags []Diagnostic
	for i, r := range rows {
		f := fields(r)
		if len(f) != 4 {
			return nil, nil, fmt.Errorf("furnace row %d: want start,ramp,target,hold", i+1)
		}
		var v [4]float64
		for j := range v {
			x, err := num(f[j])
			if err != nil {
				return nil, nil, fmt.Errorf("furnace row %d: unparsable number", i+1)
			}
			v[j] = x
		}
		doc.Segments = append(doc.Segments, FurnaceSegment{Line: i + 1, Start: v[0], Ramp: v[1], Target: v[2], Hold: v[3]})
	}
	return doc, diags, nil
}

func parseGas(units map[string]string, rows []string) (any, []Diagnostic, error) {
	doc := &GasDoc{TimeUnit: def(units, "time_unit", UnitTime), FlowUnit: def(units, "flow_unit", UnitFlow)}
	var diags []Diagnostic
	for i, r := range rows {
		f := fields(r)
		if len(f) != 3 {
			return nil, nil, fmt.Errorf("gas_events row %d: want time,gas,flow", i+1)
		}
		t, e1 := num(f[0])
		fl, e2 := num(f[2])
		if e1 != nil || e2 != nil {
			return nil, nil, fmt.Errorf("gas_events row %d: unparsable number", i+1)
		}
		doc.Events = append(doc.Events, GasEvent{Line: i + 1, Time: t, Gas: f[1], Flow: fl})
	}
	return doc, diags, nil
}

func parseBlank(units map[string]string, rows []string) (any, []Diagnostic, error) {
	doc := &BlankDoc{TempUnit: def(units, "temp_unit", UnitTemp), MassUnit: def(units, "mass_unit", UnitMass)}
	var diags []Diagnostic
	for i, r := range rows {
		f := fields(r)
		if len(f) != 2 {
			return nil, nil, fmt.Errorf("blank row %d: want temp,mass", i+1)
		}
		tp, e1 := num(f[0])
		m, e2 := num(f[1])
		if e1 != nil || e2 != nil {
			return nil, nil, fmt.Errorf("blank row %d: unparsable number", i+1)
		}
		doc.Points = append(doc.Points, BlankPoint{Line: i + 1, Temp: tp, Mass: m})
	}
	return doc, diags, nil
}

// parseInitialMass accepts key=value lines, e.g.
//
//	pan_tare_mg=845.12
//	sample_mass_mg=10.243
//	basis=as-received
func parseInitialMass(rows []string) (any, []Diagnostic, error) {
	doc := &InitialMassDoc{}
	var diags []Diagnostic
	kv := map[string]string{}
	for _, r := range rows {
		k, v, ok := strings.Cut(r, "=")
		if !ok {
			return nil, nil, fmt.Errorf("initial_mass: want key=value lines, got %q", r)
		}
		kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	parseQ := func(key string) (Quantity, error) {
		raw, ok := kv[key]
		if !ok {
			return Quantity{}, fmt.Errorf("initial_mass: missing %s", key)
		}
		// allow "10.243" (unit implied by key suffix) or "10.243 mg"
		if strings.HasSuffix(key, "_mg") {
			f, err := num(raw)
			if err == nil {
				return Quantity{Value: f, Unit: "mg"}, nil
			}
			parts := strings.Fields(raw)
			if len(parts) == 2 {
				if f, err := num(parts[0]); err == nil {
					return Quantity{Value: f, Unit: parts[1]}, nil
				}
			}
			return Quantity{}, fmt.Errorf("initial_mass: bad %s value %q", key, raw)
		}
		return Quantity{}, nil
	}
	var err error
	if doc.PanTare, err = parseQ("pan_tare_mg"); err != nil {
		return nil, nil, err
	}
	if doc.SampleMass, err = parseQ("sample_mass_mg"); err != nil {
		return nil, nil, err
	}
	doc.Basis = kv["basis"]
	if doc.Basis == "" {
		diags = append(diags, Diagnostic{Code: "missing_basis", Where: "basis", Message: "no coordinate basis declared; assumed as-received"})
		doc.Basis = "as-received"
	}
	if _, err := ToMilligrams(doc.SampleMass.Value, doc.SampleMass.Unit); err != nil {
		diags = append(diags, Diagnostic{Code: "unknown_unit", Where: "sample_mass_mg", Message: err.Error()})
	}
	return doc, diags, nil
}

func def(m map[string]string, k, d string) string {
	if v, ok := m[k]; ok && v != "" {
		return v
	}
	return d
}

func checkUnits(timeU, tempU, massU string) []Diagnostic {
	var diags []Diagnostic
	if _, err := ToSeconds(1, timeU); err != nil {
		diags = append(diags, Diagnostic{Code: "unknown_unit", Where: "time_unit", Message: err.Error()})
	}
	if _, err := ToCelsius(1, tempU); err != nil {
		diags = append(diags, Diagnostic{Code: "unknown_unit", Where: "temp_unit", Message: err.Error()})
	}
	if _, err := ToMilligrams(1, massU); err != nil {
		diags = append(diags, Diagnostic{Code: "unknown_unit", Where: "mass_unit", Message: err.Error()})
	}
	return diags
}
