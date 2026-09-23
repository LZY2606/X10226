// Package tga implements the domain layer of the thermogravimetric review
// system: layered, immutable imports, unit-aware values and the stage
// decomposition pipeline (drift/blank correction, gap-aware DTG, overlapping
// stage candidates, atmosphere-delay timeline and residue calculation chains).
package tga

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// AlgoVersion is stamped on every derived value. Bumping it makes every
// derived content hash differ, which is how rule revisions are audited.
const AlgoVersion = "tga-pipeline/1.0.0"

// Layer names (also the stable machine-readable kind of an input document).
const (
	LayerRawPoints        = "raw_points"
	LayerDriftCorrection  = "drift_correction"
	LayerFurnaceProgram   = "furnace_program"
	LayerGasEvents        = "gas_events"
	LayerBlankCurve       = "blank_curve"
	LayerInitialMass      = "initial_mass"
	LayerParams           = "params"
)

// KnownFormats are the accepted envelope format strings.
const (
	FmtPoints     = "tga-points/v1"
	FmtDrift      = "tga-drift/v1"
	FmtFurnace    = "tga-furnace/v1"
	FmtGas        = "tga-gas-events/v1"
	FmtBlank      = "tga-blank/v1"
	FmtInitMass   = "tga-initial-mass/v1"
	FmtParams     = "tga-params/v1"
	FmtArchive    = "tga-archive/v1"
)

// Diagnostic records a value-level import problem without discarding the raw
// representation. Fatal diagnostics reject the whole layer; non-fatal ones
// leave the offending value in place (OK=false) so it is never silently
// dropped "to make the plot work".
type Diagnostic struct {
	Position string `json:"position"`
	Message  string `json:"message"`
	Fatal    bool   `json:"fatal"`
}

// Value preserves the original textual representation alongside the parsed,
// normalized value and its unit / coordinate basis.
type Value struct {
	Text string  `json:"text"`
	Unit string  `json:"unit"`
	V    float64 `json:"v"`
	OK   bool    `json:"ok"`
}

// Point is one raw sampling point. Every field keeps its original token.
type Point struct {
	T    Value `json:"t"`
	M    Value `json:"m"`
	Temp Value `json:"temp"`
}

// RawDoc is the parsed form of any input layer.
type RawDoc struct {
	ID         string       `json:"id,omitempty"`
	Layer      string       `json:"layer"`
	Format     string       `json:"format"`
	Version    int          `json:"version"`
	Parent     string       `json:"parent,omitempty"`
	Points     []Point      `json:"points,omitempty"`
	Segments   []Seg        `json:"segments,omitempty"`
	Gas        []GasEvent   `json:"gas,omitempty"`
	InitialM   *Value       `json:"initial_mass,omitempty"`
	Params     *Params      `json:"params,omitempty"`
	Diags      []Diagnostic `json:"diagnostics,omitempty"`
}

// Seg is a piecewise-linear furnace segment or a drift/blank segment.
type Seg struct {
	T0 Value `json:"t0"`
	T1 Value `json:"t1"`
	V0 Value `json:"v0"`
	V1 Value `json:"v1"`
}

// GasEvent is a gas valve event in furnace time.
type GasEvent struct {
	T    Value  `json:"t"`
	Gas  string `json:"gas"`
	Flow Value  `json:"flow"`
}

// Params is the versioned algorithm parameter set.
type Params struct {
	SmoothWindowS   float64 `json:"smooth_window_s"`
	MinProminence   float64 `json:"min_prominence_mg_per_s"`
	StageThreshold  float64 `json:"stage_threshold_frac"`
	MaxGapS         float64 `json:"max_gap_s"`
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"`
	TempLagS        float64 `json:"temp_lag_s"`
}

// DefaultParams used when the first project has no parameter revision.
func DefaultParams() Params {
	return Params{
		SmoothWindowS:   30,
		MinProminence:   0.000010,
		StageThreshold:  0.10,
		MaxGapS:         5,
		AtmosphereDelayS: 45,
		TempLagS:        8,
	}
}

func parseValue(path, rawUnit string, v any) Value {
	switch x := v.(type) {
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		if err != nil {
			return Value{Text: x, Unit: rawUnit, OK: false}
		}
		return Value{Text: x, Unit: rawUnit, V: f, OK: true}
	case float64:
		return Value{Text: strconv.FormatFloat(x, 'g', -1, 64), Unit: rawUnit, V: x, OK: true}
	case json.Number:
		f, err := x.Float64()
		if err != nil {
			return Value{Text: x.String(), Unit: rawUnit, OK: false}
		}
		return Value{Text: x.String(), Unit: rawUnit, V: f, OK: true}
	default:
		return Value{Text: fmt.Sprintf("%v", v), Unit: rawUnit, OK: false}
	}
}

// normalizeUnit converts mass and temperature units into the canonical
// coordinate basis (mg / C). Time is always seconds.
func normalizeUnit(path, kind, unit string, v float64, diags *[]Diagnostic, fatal bool) (float64, string) {
	switch kind {
	case "time":
		switch unit {
		case "s", "sec", "seconds":
			return v, "s"
		case "min", "minutes":
			return v * 60, "min"
		}
	case "mass":
		switch unit {
		case "mg":
			return v, "mg"
		case "g":
			return v * 1000, "g"
		case "ug", "\u00b5g":
			return v / 1000, "ug"
		}
	case "temp":
		switch unit {
		case "C", "\u2103", "celsius":
			return v, "C"
		case "K", "kelvin":
			return v - 273.15, "K"
		}
	case "flow":
		switch unit {
		case "mL/min", "sccm":
			return v, unit
		}
	}
	*diags = append(*diags, Diagnostic{Position: path, Message: fmt.Sprintf("unknown or incompatible unit %q for %s", unit, kind), Fatal: fatal})
	return v, unit
}

func decodeNum(path, kind, rawUnit string, v any, diags *[]Diagnostic) Value {
	val := parseValue(path, rawUnit, v)
	if !val.OK {
		*diags = append(*diags, Diagnostic{Position: path, Message: fmt.Sprintf("unparseable %s value: %q", kind, val.Text)})
		return val
	}
	f, conv := normalizeUnit(path, kind, val.Unit, val.V, diags, false)
	if conv != val.Unit {
		val.V = f
		val.Unit = conv + "->" + canonicalUnit(kind)
	}
	return val
}

func canonicalUnit(kind string) string {
	switch kind {
	case "time":
		return "s"
	case "mass":
		return "mg"
	case "temp":
		return "C"
	case "flow":
		return "mL/min"
	}
	return ""
}

func asStringMap(path string, v any, diags *[]Diagnostic) map[string]any {
	m, ok := v.(map[string]any)
	if !ok {
		*diags = append(*diags, Diagnostic{Position: path, Message: "expected an object", Fatal: true})
		return nil
	}
	return m
}

func get(m map[string]any, key string) any { return m[key] }

func unitOf(m map[string]any, field, def string) string {
	if u, ok := m[field+"_unit"].(string); ok {
		return u
	}
	return def
}

// ParseDoc parses one layer envelope. The returned RawDoc always carries the
// diagnostics; a fatal diagnostic means the layer must be rejected (import
// half-failure), while value-level diagnostics are retained in place.
func ParseDoc(raw []byte) (*RawDoc, []Diagnostic, error) {
	var env map[string]any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&env); err != nil {
		return nil, nil, fmt.Errorf("invalid JSON: %w", err)
	}
	layer, _ := env["layer"].(string)
	format, _ := env["format"].(string)
	if layer == "" || format == "" {
		return nil, nil, fmt.Errorf("envelope requires layer and format")
	}
	doc := &RawDoc{Layer: layer, Format: format}
	if id, ok := env["id"].(string); ok {
		doc.ID = id
	}
	if n, ok := env["version"].(json.Number); ok {
		v, _ := n.Int64()
		doc.Version = int(v)
	}
	if p, ok := env["parent"].(string); ok {
		doc.Parent = p
	}
	var diags []Diagnostic
	switch {
	case layer == LayerRawPoints && format == FmtPoints:
		parsePoints(env, doc, &diags)
	case layer == LayerDriftCorrection && format == FmtDrift:
		parseSegs(env, doc, &diags, "drift", "mass")
	case layer == LayerBlankCurve && format == FmtBlank:
		parsePoints(env, doc, &diags)
	case layer == LayerFurnaceProgram && format == FmtFurnace:
		parseSegs(env, doc, &diags, "furnace", "temp")
	case layer == LayerGasEvents && format == FmtGas:
		parseGas(env, doc, &diags)
	case layer == LayerInitialMass && format == FmtInitMass:
		parseInitialMass(env, doc, &diags)
	case layer == LayerParams && format == FmtParams:
		parseParams(env, doc)
	default:
		return nil, nil, fmt.Errorf("unsupported layer/format: %s/%s", layer, format)
	}
	doc.Diags = diags
	return doc, diags, nil
}

func parsePoints(env map[string]any, doc *RawDoc, diags *[]Diagnostic) {
	arr, ok := env["points"].([]any)
	if !ok {
		*diags = append(*diags, Diagnostic{Position: "points", Message: "points must be an array", Fatal: true})
		return
	}
	tU := unitOf(env, "t", "s")
	mU := unitOf(env, "m", "mg")
	tempU := unitOf(env, "temp", "C")
	for i, item := range arr {
		m := asStringMap(fmt.Sprintf("points[%d]", i), item, diags)
		if m == nil {
			continue
		}
		p := Point{
			T:    decodeNum(fmt.Sprintf("points[%d].t", i), "time", tU, get(m, "t"), diags),
			M:    decodeNum(fmt.Sprintf("points[%d].m", i), "mass", mU, get(m, "m"), diags),
			Temp: decodeNum(fmt.Sprintf("points[%d].temp", i), "temp", tempU, get(m, "temp"), diags),
		}
		doc.Points = append(doc.Points, p)
	}
	if len(doc.Points) == 0 {
		*diags = append(*diags, Diagnostic{Position: "points", Message: "no points", Fatal: true})
	}
}

func parseSegs(env map[string]any, doc *RawDoc, diags *[]Diagnostic, kind, valKind string) {
	arr, ok := env["segments"].([]any)
	if !ok {
		*diags = append(*diags, Diagnostic{Position: "segments", Message: "segments must be an array", Fatal: true})
		return
	}
	for i, item := range arr {
		m := asStringMap(fmt.Sprintf("segments[%d]", i), item, diags)
		if m == nil {
			continue
		}
		vU := valUnit(env, valKind)
		s := Seg{
			T0: decodeNum(fmt.Sprintf("segments[%d].t0", i), "time", unitOf(env, "t", "s"), get(m, "t0"), diags),
			T1: decodeNum(fmt.Sprintf("segments[%d].t1", i), "time", unitOf(env, "t", "s"), get(m, "t1"), diags),
			V0: decodeNum(fmt.Sprintf("segments[%d].v0", i), valKind, vU, get(m, "v0"), diags),
			V1: decodeNum(fmt.Sprintf("segments[%d].v1", i), valKind, vU, get(m, "v1"), diags),
		}
		doc.Segments = append(doc.Segments, s)
	}
	if len(doc.Segments) == 0 {
		*diags = append(*diags, Diagnostic{Position: "segments", Message: "no segments", Fatal: true})
	}
}

func valUnit(env map[string]any, valKind string) string {
	switch valKind {
	case "mass":
		return unitOf(env, "v", "mg")
	case "temp":
		return unitOf(env, "v", "C")
	}
	return ""
}

func parseGas(env map[string]any, doc *RawDoc, diags *[]Diagnostic) {
	arr, ok := env["gas_events"].([]any)
	if !ok {
		*diags = append(*diags, Diagnostic{Position: "gas_events", Message: "gas_events must be an array", Fatal: true})
		return
	}
	for i, item := range arr {
		m := asStringMap(fmt.Sprintf("gas_events[%d]", i), item, diags)
		if m == nil {
			continue
		}
		gas, _ := m["gas"].(string)
		if gas == "" {
			*diags = append(*diags, Diagnostic{Position: fmt.Sprintf("gas_events[%d].gas", i), Message: "gas name required", Fatal: true})
		}
		ge := GasEvent{
			T:    decodeNum(fmt.Sprintf("gas_events[%d].t", i), "time", unitOf(env, "t", "s"), get(m, "t"), diags),
			Gas:  gas,
			Flow: decodeNum(fmt.Sprintf("gas_events[%d].flow", i), "flow", unitOf(env, "flow", "mL/min"), get(m, "flow"), diags),
		}
		doc.Gas = append(doc.Gas, ge)
	}
	if len(doc.Gas) == 0 {
		*diags = append(*diags, Diagnostic{Position: "gas_events", Message: "no gas events", Fatal: true})
	}
}

func parseInitialMass(env map[string]any, doc *RawDoc, diags *[]Diagnostic) {
	m := asStringMap("initial_mass", get(env, "initial_mass"), diags)
	if m == nil {
		return
	}
	v := decodeNum("initial_mass", "mass", unitOf(env, "m", "mg"), get(m, "value"), diags)
	doc.InitialM = &v
}

func parseParams(env map[string]any, doc *RawDoc) {
	b, _ := json.Marshal(get(env, "params"))
	p := DefaultParams()
	_ = json.Unmarshal(b, &p)
	doc.Params = &p
}

// HasFatal reports whether parsing diagnostics reject the layer.
func HasFatal(diags []Diagnostic) bool {
	for _, d := range diags {
		if d.Fatal {
			return true
		}
	}
	return false
}
