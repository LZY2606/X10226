package tga

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// FormatError describes a structural problem in a raw layer payload.
// A layer with a FormatError is rejected as a whole; its raw bytes stay
// untouched and the rejection is recorded as an event by the caller.
type FormatError struct {
	Layer   string `json:"layer"`
	Message string `json:"message"`
}

func (e *FormatError) Error() string {
	return fmt.Sprintf("layer %s: %s", e.Layer, e.Message)
}

// massToMg converts a mass value to mg. ok=false means the unit is
// unknown and the value must stay in place with a diagnostic.
func massToMg(v float64, unit string) (float64, bool) {
	switch unit {
	case "mg":
		return v, true
	case "g":
		return v * 1000, true
	case "ug":
		return v / 1000, true
	}
	return 0, false
}

func timeToS(v float64, unit string) (float64, bool) {
	switch unit {
	case "s":
		return v, true
	case "min":
		return v * 60, true
	}
	return 0, false
}

func tempToC(v float64, unit string) (float64, bool) {
	switch unit {
	case "C":
		return v, true
	case "K":
		return v - 273.15, true
	}
	return 0, false
}

// tablePayload is the shared shape of row-based layers.
type tablePayload struct {
	Columns []string            `json:"columns"`
	Units   map[string]string   `json:"units"`
	Rows    [][]json.RawMessage `json:"rows"`
}

func cellNumber(raw json.RawMessage) (float64, bool) {
	var f float64
	if err := json.Unmarshal(raw, &f); err == nil {
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		v, err := strconv.ParseFloat(s, 64)
		if err == nil {
			return v, true
		}
	}
	return 0, false
}

func colIndex(p *tablePayload, name string) int {
	for i, c := range p.Columns {
		if c == name {
			return i
		}
	}
	return -1
}

// ParseSamplePoints normalizes the raw sample-point table. Rows whose
// values cannot be converted stay in the raw payload; the normalized
// output simply skips them and records a diagnostic per row.
func ParseSamplePoints(raw json.RawMessage) ([]Point, []Diagnostic, error) {
	var p tablePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, nil, &FormatError{Layer: LayerSamplePoints, Message: "not a JSON table: " + err.Error()}
	}
	ti, mi, tei := colIndex(&p, "t"), colIndex(&p, "mass"), colIndex(&p, "temp")
	if ti < 0 || mi < 0 || tei < 0 {
		return nil, nil, &FormatError{Layer: LayerSamplePoints, Message: "columns must include t, mass, temp"}
	}
	tu, mu, teu := p.Units["t"], p.Units["mass"], p.Units["temp"]
	var pts []Point
	var diags []Diagnostic
	for i, row := range p.Rows {
		loc := fmt.Sprintf("row %d", i)
		need := max3(ti, mi, tei)
		if len(row) <= need {
			diags = append(diags, Diagnostic{Layer: LayerSamplePoints, Location: loc, Message: "row too short, left in raw", Raw: string(mustJSON(row))})
			continue
		}
		tv, ok1 := cellNumber(row[ti])
		mv, ok2 := cellNumber(row[mi])
		tev, ok3 := cellNumber(row[tei])
		if !ok1 || !ok2 || !ok3 {
			diags = append(diags, Diagnostic{Layer: LayerSamplePoints, Location: loc, Message: "unconvertible cell, left in raw", Raw: string(mustJSON(row))})
			continue
		}
		t, ok1 := timeToS(tv, tu)
		m, ok2 := massToMg(mv, mu)
		tc, ok3 := tempToC(tev, teu)
		if !ok1 || !ok2 || !ok3 {
			diags = append(diags, Diagnostic{Layer: LayerSamplePoints, Location: loc, Message: fmt.Sprintf("unknown unit (t=%q mass=%q temp=%q), left in raw", tu, mu, teu), Raw: string(mustJSON(row))})
			continue
		}
		pts = append(pts, Point{T: t, Mass: m, TempC: tc})
	}
	if len(pts) == 0 {
		return nil, diags, &FormatError{Layer: LayerSamplePoints, Message: "no convertible rows"}
	}
	return pts, diags, nil
}

// DriftModel is the parsed balance-drift correction.
type DriftModel struct {
	Kind      string    `json:"kind"` // "linear" | "knots"
	Intercept float64   `json:"intercept_mg"`
	SlopeMgS  float64   `json:"slope_mg_per_s"`
	Knots     []XYPoint `json:"knots,omitempty"` // t(s) -> offset(mg)
}

// Offset returns the drift offset in mg at time t (seconds).
func (d DriftModel) Offset(t float64) float64 {
	switch d.Kind {
	case "linear":
		return d.Intercept + d.SlopeMgS*t
	case "knots":
		if len(d.Knots) == 0 {
			return 0
		}
		if t <= d.Knots[0].X {
			return d.Knots[0].Y
		}
		for i := 1; i < len(d.Knots); i++ {
			if t <= d.Knots[i].X {
				x0, y0 := d.Knots[i-1].X, d.Knots[i-1].Y
				x1, y1 := d.Knots[i].X, d.Knots[i].Y
				if x1 == x0 {
					return y1
				}
				return y0 + (y1-y0)*(t-x0)/(x1-x0)
			}
		}
		return d.Knots[len(d.Knots)-1].Y
	}
	return 0
}

// ParseDrift parses the balance-drift layer.
func ParseDrift(raw json.RawMessage) (DriftModel, []Diagnostic, error) {
	var head struct {
		Model    string `json:"model"`
		UnitMass string `json:"unit_mass"`
		UnitTime string `json:"unit_time"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return DriftModel{}, nil, &FormatError{Layer: LayerDrift, Message: "not a JSON object: " + err.Error()}
	}
	toMg := func(v float64) (float64, bool) { return massToMg(v, head.UnitMass) }
	switch head.Model {
	case "linear":
		var p struct {
			Intercept float64 `json:"intercept"`
			Slope     float64 `json:"slope"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return DriftModel{}, nil, &FormatError{Layer: LayerDrift, Message: err.Error()}
		}
		ic, ok1 := toMg(p.Intercept)
		sl, ok2 := toMg(p.Slope)
		if !ok1 || !ok2 {
			return DriftModel{}, nil, &FormatError{Layer: LayerDrift, Message: "unknown mass unit " + strconv.Quote(head.UnitMass)}
		}
		per := 1.0
		switch head.UnitTime {
		case "min":
			per = 60
		case "s":
		default:
			return DriftModel{}, nil, &FormatError{Layer: LayerDrift, Message: "unknown time unit " + strconv.Quote(head.UnitTime)}
		}
		return DriftModel{Kind: "linear", Intercept: ic, SlopeMgS: sl / per}, nil, nil
	case "knots":
		var p struct {
			Knots [][]float64 `json:"knots"`
		}
		if err := json.Unmarshal(raw, &p); err != nil {
			return DriftModel{}, nil, &FormatError{Layer: LayerDrift, Message: err.Error()}
		}
		if len(p.Knots) == 0 {
			return DriftModel{}, nil, &FormatError{Layer: LayerDrift, Message: "knots model needs at least one knot"}
		}
		var knots []XYPoint
		for i, k := range p.Knots {
			if len(k) != 2 {
				return DriftModel{}, nil, &FormatError{Layer: LayerDrift, Message: fmt.Sprintf("knot %d must be [t, offset]", i)}
			}
			t, ok1 := timeToS(k[0], head.UnitTime)
			m, ok2 := toMg(k[1])
			if !ok1 || !ok2 {
				return DriftModel{}, nil, &FormatError{Layer: LayerDrift, Message: fmt.Sprintf("knot %d has unknown unit", i)}
			}
			knots = append(knots, XYPoint{X: t, Y: m})
		}
		return DriftModel{Kind: "knots", Knots: knots}, nil, nil
	}
	return DriftModel{}, nil, &FormatError{Layer: LayerDrift, Message: "unknown model " + strconv.Quote(head.Model)}
}

// ParseFurnace parses the furnace program.
func ParseFurnace(raw json.RawMessage) ([]FurnSeg, []Diagnostic, error) {
	var p struct {
		Segments []FurnSeg `json:"segments"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, nil, &FormatError{Layer: LayerFurnace, Message: "not a JSON object: " + err.Error()}
	}
	if len(p.Segments) == 0 {
		return nil, nil, &FormatError{Layer: LayerFurnace, Message: "no segments"}
	}
	for i, s := range p.Segments {
		if s.T1 <= s.T0 {
			return nil, nil, &FormatError{Layer: LayerFurnace, Message: fmt.Sprintf("segment %d has non-positive duration", i)}
		}
		if s.Kind != "ramp" && s.Kind != "hold" {
			return nil, nil, &FormatError{Layer: LayerFurnace, Message: fmt.Sprintf("segment %d has unknown kind %q", i, s.Kind)}
		}
	}
	return p.Segments, nil, nil
}

// TempAt evaluates the furnace program at time t.
func TempAt(segs []FurnSeg, t float64) float64 {
	for _, s := range segs {
		if t >= s.T0 && t <= s.T1 {
			if s.Kind == "hold" {
				return s.HoldC
			}
			return s.T0C + s.RateCPerMin*(t-s.T0)/60
		}
	}
	if len(segs) > 0 {
		last := segs[len(segs)-1]
		if last.Kind == "hold" {
			return last.HoldC
		}
		return last.T0C + last.RateCPerMin*(last.T1-last.T0)/60
	}
	return 0
}

// ParseGasEvents parses the gas-valve event layer.
func ParseGasEvents(raw json.RawMessage) ([]GasEvent, []Diagnostic, error) {
	var p struct {
		Events []GasEvent `json:"events"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, nil, &FormatError{Layer: LayerGasEvents, Message: "not a JSON object: " + err.Error()}
	}
	if len(p.Events) == 0 {
		return nil, nil, &FormatError{Layer: LayerGasEvents, Message: "no events"}
	}
	for i, e := range p.Events {
		if e.To == "" {
			return nil, nil, &FormatError{Layer: LayerGasEvents, Message: fmt.Sprintf("event %d missing target gas field \"to\"", i)}
		}
		if e.T < 0 {
			return nil, nil, &FormatError{Layer: LayerGasEvents, Message: fmt.Sprintf("event %d has negative time", i)}
		}
	}
	return p.Events, nil, nil
}

// ParseBlank parses the blank (empty-pan) mass-loss curve.
func ParseBlank(raw json.RawMessage) ([]XYPoint, []Diagnostic, error) {
	var p tablePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, nil, &FormatError{Layer: LayerBlank, Message: "not a JSON table: " + err.Error()}
	}
	ti, li := colIndex(&p, "t"), colIndex(&p, "loss")
	if ti < 0 || li < 0 {
		return nil, nil, &FormatError{Layer: LayerBlank, Message: "columns must include t, loss"}
	}
	tu, lu := p.Units["t"], p.Units["loss"]
	var pts []XYPoint
	var diags []Diagnostic
	for i, row := range p.Rows {
		loc := fmt.Sprintf("row %d", i)
		if len(row) <= max(ti, li) {
			diags = append(diags, Diagnostic{Layer: LayerBlank, Location: loc, Message: "row too short, left in raw", Raw: string(mustJSON(row))})
			continue
		}
		tv, ok1 := cellNumber(row[ti])
		lv, ok2 := cellNumber(row[li])
		if !ok1 || !ok2 {
			diags = append(diags, Diagnostic{Layer: LayerBlank, Location: loc, Message: "unconvertible cell, left in raw", Raw: string(mustJSON(row))})
			continue
		}
		t, ok1 := timeToS(tv, tu)
		l, ok2 := massToMg(lv, lu)
		if !ok1 || !ok2 {
			diags = append(diags, Diagnostic{Layer: LayerBlank, Location: loc, Message: "unknown unit, left in raw", Raw: string(mustJSON(row))})
			continue
		}
		pts = append(pts, XYPoint{X: t, Y: l})
	}
	if len(pts) == 0 {
		return nil, diags, &FormatError{Layer: LayerBlank, Message: "no convertible rows"}
	}
	return pts, diags, nil
}

// InitialMass is the parsed sample initial-mass layer.
type InitialMass struct {
	ValueMg float64 `json:"value_mg"`
	Basis   string  `json:"basis"`
}

// ParseInitialMass parses the initial-mass layer.
func ParseInitialMass(raw json.RawMessage) (InitialMass, []Diagnostic, error) {
	var p struct {
		Value float64 `json:"value"`
		Unit  string  `json:"unit"`
		Basis string  `json:"basis"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return InitialMass{}, nil, &FormatError{Layer: LayerInitialMass, Message: "not a JSON object: " + err.Error()}
	}
	v, ok := massToMg(p.Value, p.Unit)
	if !ok {
		return InitialMass{}, nil, &FormatError{Layer: LayerInitialMass, Message: "unknown mass unit " + strconv.Quote(p.Unit)}
	}
	if v <= 0 {
		return InitialMass{}, nil, &FormatError{Layer: LayerInitialMass, Message: "initial mass must be positive"}
	}
	if p.Basis == "" {
		p.Basis = "as_received"
	}
	return InitialMass{ValueMg: v, Basis: p.Basis}, nil, nil
}

func mustJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("null")
	}
	return b
}

func max3(a, b, c int) int {
	m := a
	if b > m {
		m = b
	}
	if c > m {
		m = c
	}
	return m
}
