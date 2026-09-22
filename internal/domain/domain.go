// Package domain contains the thermal-analysis domain model: the six raw
// layers, their parsed forms, units/coordinate bases and the parsing rules
// that keep unconvertible values in place instead of dropping them.
package domain

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// LayerKind identifies one of the six independently versioned raw layers.
type LayerKind string

const (
	SamplePoints    LayerKind = "sample_points"
	DriftLayer       LayerKind = "drift_correction"
	FurnaceLayer     LayerKind = "furnace_program"
	GasValveEvents  LayerKind = "gas_valve_events"
	BlankCurve      LayerKind = "blank_curve"
	InitialMass     LayerKind = "initial_mass"
)

// AllLayers lists the six layers in canonical (timeline) order.
func AllLayers() []LayerKind {
	return []LayerKind{SamplePoints, DriftLayer, FurnaceLayer, GasValveEvents, BlankCurve, InitialMass}
}

func (k LayerKind) Valid() bool {
	for _, x := range AllLayers() {
		if x == k {
			return true
		}
	}
	return false
}

// Coordinate / unit bases. Every derived quantity references one of these so
// that furnace time, furnace temperature and estimated sample temperature are
// never silently conflated.
const (
	BasisFurnaceTimeS  = "t_furnace_s" // furnace/elapsed clock, seconds
	BasisFurnaceTempC  = "T_furnace_C"
	BasisSampleTempC   = "T_sample_C" // estimated via lag model
	BasisBalanceMassMg = "m_balance_mg"
	BasisSampleMassMg  = "m_sample_mg"
)

// Field preserves the original textual representation together with the parsed
// value, its unit and per-field diagnostics. A nil Value means "kept, not
// convertible" — the point is never silently dropped to make a plot.
type Field struct {
	Raw         string   `json:"raw"`
	Value       *float64 `json:"value,omitempty"`
	Unit        string   `json:"unit,omitempty"`
	Diagnostics []string `json:"diagnostics,omitempty"`
}

func numberField(raw string, unit string) Field {
	raw = strings.TrimSpace(raw)
	f := Field{Raw: raw, Unit: unit}
	if raw == "" {
		f.Diagnostics = []string{"empty value retained at source position"}
		return f
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		f.Diagnostics = []string{fmt.Sprintf("unconvertible number %q retained at source position", raw)}
		return f
	}
	f.Value = &v
	return f
}

// OK reports whether the field was converted.
func (f Field) OK() bool { return f.Value != nil }

// SamplePoint is one row of the raw TG acquisition table.
type SamplePoint struct {
	Index  int   `json:"index"`
	TimeS  Field `json:"time_s"`
	TempC  Field `json:"temp_c"`
	MassMg Field `json:"mass_mg"`
}

type DriftCorrection struct {
	Basis          string `json:"basis"`
	RateMgPerMin   Field  `json:"rate_mg_per_min"`
	ReferenceTimeS Field  `json:"reference_time_s"`
	Note           string `json:"note,omitempty"`
}

type FurnaceSegment struct {
	Index  int    `json:"index"`
	Kind   string `json:"kind"` // ramp | hold
	StartS Field  `json:"start_s"`
	EndS   Field  `json:"end_s"`
	StartC Field  `json:"start_c"`
	EndC   Field  `json:"end_c"`
}

type FurnaceProgram struct {
	TempUnit string           `json:"temp_unit"`
	Segments []FurnaceSegment `json:"segments"`
}

type ValveEvent struct {
	Index    int    `json:"index"`
	TimeS    Field  `json:"time_s"`
	FromGas  string `json:"from_gas"`
	ToGas    string `json:"to_gas"`
	Flow     Field  `json:"flow"`
	FlowUnit string `json:"flow_unit"`
	Note     string `json:"note,omitempty"`
}

type BlankPoint struct {
	Index  int   `json:"index"`
	TimeS  Field `json:"time_s"`
	MassMg Field `json:"blank_mg"`
}

type InitialMassLayer struct {
	MassMg         Field  `json:"mass_mg"`
	Method         string `json:"method"`
	ReferenceTimeS Field  `json:"reference_time_s"`
	Note           string `json:"note,omitempty"`
}

// Layer is the parsed form of one raw layer revision.
type Layer struct {
	Kind        LayerKind
	SourceFile  string
	Samples     []SamplePoint
	Drift       *DriftCorrection
	Furnace     *FurnaceProgram
	Valves      []ValveEvent
	Blank       []BlankPoint
	InitialMass *InitialMassLayer
	Diagnostics []string // layer/row level diagnostics; accepted fragment retained
}

// FatalFormatError marks an import that cannot even be committed as a fragment.
type FatalFormatError struct{ Msg string }

func (e *FatalFormatError) Error() string { return e.Msg }

// ParseLayer parses one layer's raw JSON data. Structural problems return a
// fatal error; row-level value problems are collected as diagnostics on the
// retained fragment so an import can be partial instead of all-or-nothing.
func ParseLayer(kind LayerKind, sourceFile string, data json.RawMessage) (*Layer, error) {
	if !kind.Valid() {
		return nil, &FatalFormatError{Msg: fmt.Sprintf("unknown layer kind %q", kind)}
	}
	var generic any
	if err := json.Unmarshal(data, &generic); err != nil {
		return nil, &FatalFormatError{Msg: fmt.Sprintf("layer %s: invalid JSON: %v", kind, err)}
	}
	rows, isArray := generic.([]any)
	obj, isObj := generic.(map[string]any)
	if !isArray && !isObj {
		return nil, &FatalFormatError{Msg: fmt.Sprintf("layer %s: data must be object or array", kind)}
	}
	l := &Layer{Kind: kind, SourceFile: sourceFile}
	str := func(m map[string]any, key string) string {
		if v, ok := m[key]; ok {
			return fmt.Sprintf("%v", v)
		}
		return ""
	}
	switch kind {
	case SamplePoints:
		if !isArray {
			return nil, &FatalFormatError{Msg: "sample_points.data must be an array of rows"}
		}
		for i, r := range rows {
			m, ok := r.(map[string]any)
			if !ok {
				l.Diagnostics = append(l.Diagnostics, fmt.Sprintf("row %d: non-object row skipped from parsed layer but retained in raw", i))
				continue
			}
			l.Samples = append(l.Samples, SamplePoint{
				Index:  i,
				TimeS:  numberField(str(m, "time_s"), "s"),
				TempC:  numberField(str(m, "temp_c"), "degC"),
				MassMg: numberField(str(m, "mass_mg"), "mg"),
			})
		}
	case DriftLayer:
		if !isObj {
			return nil, &FatalFormatError{Msg: "drift_correction.data must be an object"}
		}
		l.Drift = &DriftCorrection{
			Basis:          str(obj, "basis"),
			RateMgPerMin:   numberField(str(obj, "rate_mg_per_min"), "mg/min"),
			ReferenceTimeS: numberField(str(obj, "reference_time_s"), "s"),
			Note:           str(obj, "note"),
		}
		if l.Drift.Basis == "" {
			l.Diagnostics = append(l.Diagnostics, "drift_correction.basis missing")
		}
	case FurnaceLayer:
		if !isObj {
			return nil, &FatalFormatError{Msg: "furnace_program.data must be an object"}
		}
		fp := &FurnaceProgram{TempUnit: str(obj, "temp_unit")}
		if fp.TempUnit == "" {
			fp.TempUnit = "C"
		}
		segsRaw, _ := obj["segments"].([]any)
		for i, r := range segsRaw {
			m, ok := r.(map[string]any)
			if !ok {
				l.Diagnostics = append(l.Diagnostics, fmt.Sprintf("segment %d malformed", i))
				continue
			}
			fp.Segments = append(fp.Segments, FurnaceSegment{
				Index:  i,
				Kind:   str(m, "kind"),
				StartS: numberField(str(m, "start_s"), "s"),
				EndS:   numberField(str(m, "end_s"), "s"),
				StartC: numberField(str(m, "start_c"), fp.TempUnit),
				EndC:   numberField(str(m, "end_c"), fp.TempUnit),
			})
		}
		l.Furnace = fp
	case GasValveEvents:
		if !isArray {
			return nil, &FatalFormatError{Msg: "gas_valve_events.data must be an array"}
		}
		for i, r := range rows {
			m, ok := r.(map[string]any)
			if !ok {
				l.Diagnostics = append(l.Diagnostics, fmt.Sprintf("valve row %d malformed", i))
				continue
			}
			l.Valves = append(l.Valves, ValveEvent{
				Index:    i,
				TimeS:    numberField(str(m, "time_s"), "s"),
				FromGas:  str(m, "from_gas"),
				ToGas:    str(m, "to_gas"),
				Flow:     numberField(str(m, "flow"), str(m, "flow_unit")),
				FlowUnit: str(m, "flow_unit"),
			})
		}
	case BlankCurve:
		if !isArray {
			return nil, &FatalFormatError{Msg: "blank_curve.data must be an array"}
		}
		for i, r := range rows {
			m, ok := r.(map[string]any)
			if !ok {
				l.Diagnostics = append(l.Diagnostics, fmt.Sprintf("blank row %d malformed", i))
				continue
			}
			l.Blank = append(l.Blank, BlankPoint{
				Index:  i,
				TimeS:  numberField(str(m, "time_s"), "s"),
				MassMg: numberField(str(m, "blank_mg"), "mg"),
			})
		}
	case InitialMass:
		if !isObj {
			return nil, &FatalFormatError{Msg: "initial_mass.data must be an object"}
		}
		l.InitialMass = &InitialMassLayer{
			MassMg:         numberField(str(obj, "mass_mg"), "mg"),
			Method:         str(obj, "method"),
			ReferenceTimeS: numberField(str(obj, "reference_time_s"), "s"),
			Note:           str(obj, "note"),
		}
		if !l.InitialMass.MassMg.OK() {
			l.Diagnostics = append(l.Diagnostics, "initial mass not numeric; retained at source position")
		}
	}
	return l, nil
}

// LayerRaw is one revision of one raw layer exactly as submitted.
type LayerRaw struct {
	Kind       LayerKind       `json:"kind"`
	SourceFile string          `json:"source_file"`
	Data       json.RawMessage `json:"data"`
}

// CanonicalJSON re-encodes JSON deterministically (sorted object keys) so that
// content ids are stable regardless of input whitespace/key order.
func CanonicalJSON(raw json.RawMessage) json.RawMessage {
	var v any
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return raw
	}
	out, err := marshalSorted(v)
	if err != nil {
		return raw
	}
	return out
}

func marshalSorted(v any) (json.RawMessage, error) {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		for i := 1; i < len(keys); i++ {
			for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
				keys[j-1], keys[j] = keys[j], keys[j-1]
			}
		}
		var b strings.Builder
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			b.Write(kb)
			b.WriteByte(':')
			vb, err := marshalSorted(t[k])
			if err != nil {
				return nil, err
			}
			b.Write(vb)
		}
		b.WriteByte('}')
		return json.RawMessage(b.String()), nil
	case []any:
		var b strings.Builder
		b.WriteByte('[')
		for i, e := range t {
			if i > 0 {
				b.WriteByte(',')
			}
			eb, err := marshalSorted(e)
			if err != nil {
				return nil, err
			}
			b.Write(eb)
		}
		b.WriteByte(']')
		return json.RawMessage(b.String()), nil
	default:
		return json.Marshal(v)
	}
}
