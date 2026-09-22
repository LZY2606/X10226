package domain

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

type ParsedQuantity struct {
	Label      string
	Value      float64
	Raw        any
	RawUnit    string
	Unit       string
	Basis      string
	Status     string
	Diagnostic *Diagnostic
}

func ParseQuantity(path string, raw json.RawMessage) ParsedQuantity {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return badQuantity(path, raw, "invalid_quantity", err.Error())
	}
	out := ParsedQuantity{Status: "ok", Basis: stringField(fields["basis"])}
	if labelRaw, ok := fields["label"]; ok {
		_ = json.Unmarshal(labelRaw, &out.Label)
	}
	if rawValue, ok := fields["raw"]; ok {
		out.Raw = json.RawMessage(rawValue)
	} else if v, ok := fields["value"]; ok {
		out.Raw = json.RawMessage(v)
	}
	if v, ok := fields["value"]; ok {
		parsed, okValue := decodeNumber(v)
		if !okValue {
			return badQuantity(path, raw, "unconvertible_value", "numeric value is required in the derived layer")
		}
		out.Value = parsed
	} else {
		return badQuantity(path, raw, "missing_value", "value is absent")
	}
	rawUnit := stringField(fields["unit"])
	out.RawUnit = rawUnit
	out.Unit, out.Status = canonicalUnit(rawUnit)
	if out.Status != "ok" {
		d := Diagnostic{Code: "unconvertible_unit", Severity: "warning", Path: path, Message: fmt.Sprintf("unit %q is retained but has no derived normalization", rawUnit), Raw: rawUnit}
		out.Diagnostic = &d
	}
	return out
}

func decodeNumber(raw json.RawMessage) (float64, bool) {
	var number float64
	if err := json.Unmarshal(raw, &number); err == nil && !math.IsNaN(number) && !math.IsInf(number, 0) {
		return number, true
	}
	var text string
	if err := json.Unmarshal(raw, &text); err != nil {
		return 0, false
	}
	number, err := strconv.ParseFloat(strings.TrimSpace(text), 64)
	if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
		return 0, false
	}
	return number, true
}

func badQuantity(path string, raw json.RawMessage, code, message string) ParsedQuantity {
	var value any
	_ = json.Unmarshal(raw, &value)
	return ParsedQuantity{Raw: value, Status: code, Diagnostic: &Diagnostic{Code: code, Severity: "warning", Path: path, Message: message, Raw: value}}
}

func stringField(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var value string
	if err := json.Unmarshal(raw, &value); err == nil {
		return value
	}
	return ""
}

func canonicalUnit(unit string) (string, string) {
	switch strings.TrimSpace(unit) {
	case "s", "sec", "second", "seconds":
		return "s", "ok"
	case "min", "minute", "minutes":
		return "min", "ok"
	case "mg":
		return "mg", "ok"
	case "mg/min":
		return "mg/min", "ok"
	case "°C", "C", "degC", "celsius":
		return "°C", "ok"
	case "":
		return "", "missing_unit"
	default:
		return unit, "unconvertible_unit"
	}
}

func (q ParsedQuantity) seconds() (float64, bool) {
	switch q.Unit {
	case "s":
		return q.Value, true
	case "min":
		return q.Value * 60, true
	default:
		return 0, false
	}
}

func (q ParsedQuantity) mass() (float64, bool) {
	return q.Value, q.Unit == "mg"
}

func (q ParsedQuantity) temperature() (float64, bool) {
	return q.Value, q.Unit == "°C"
}

type ParsedRawPoint struct {
	Time        ParsedQuantity
	Temperature ParsedQuantity
	Mass        ParsedQuantity
}

type ParsedRawSamples struct {
	Rows []ParsedRawPoint
}

type ParsedBlankPoint struct {
	Time ParsedQuantity
	Mass ParsedQuantity
}

type ParsedBlankCurve struct {
	Rows []ParsedBlankPoint
}

type ParsedDrift struct {
	Slope     ParsedQuantity
	Intercept ParsedQuantity
	Model     string
}

type ParsedFurnaceSegment struct {
	Start     ParsedQuantity
	End       ParsedQuantity
	StartTemp ParsedQuantity
	EndTemp   ParsedQuantity
	Kind      string
}

type ParsedFurnace struct {
	Segments []ParsedFurnaceSegment
}

type ParsedGasEvent struct {
	ID               string
	ValveAt          ParsedQuantity
	FromAtmosphere   string
	ToAtmosphere     string
	AtmosphereDelay  ParsedQuantity
	TemperatureLag   ParsedQuantity
	WindowEndValveAt ParsedQuantity
}

type ParsedGasEvents struct {
	Events []ParsedGasEvent
}

type ParsedInitialMass struct {
	Mass   ParsedQuantity
	Method string
}

type ParsedParameters struct {
	SampleGapThreshold ParsedQuantity
	PeakThreshold      ParsedQuantity
	OverlapStrategy    string
	BlankGapThreshold  ParsedQuantity
	NoiseThreshold     ParsedQuantity
}

type ParsedBundle struct {
	Raw         ParsedRawSamples
	Blank       ParsedBlankCurve
	Drift       ParsedDrift
	Furnace     ParsedFurnace
	Gas         ParsedGasEvents
	Initial     ParsedInitialMass
	Params      ParsedParameters
	Diagnostics []Diagnostic
}

func ParseLayer(layer string, raw json.RawMessage) (any, []Diagnostic) {
	var diagnostics []Diagnostic
	add := func(d *Diagnostic, path string, q ParsedQuantity) {
		if d != nil {
			copy := *d
			if copy.Path == "" {
				copy.Path = path
			}
			diagnostics = append(diagnostics, copy)
		}
	}
	switch layer {
	case LayerRawSamples:
		var content RawSamplesContent
		mustUnmarshal(raw, &content)
		out := ParsedRawSamples{}
		for i, row := range content.Rows {
			prefix := fmt.Sprintf("rows[%d]", i)
			t := ParseQuantity(prefix+".time", mustMarshal(row.Time))
			m := ParseQuantity(prefix+".mass", mustMarshal(row.Mass))
			temp := ParseQuantity(prefix+".temperature", mustMarshal(row.Temperature))
			add(t.Diagnostic, prefix+".time", t)
			add(m.Diagnostic, prefix+".mass", m)
			add(temp.Diagnostic, prefix+".temperature", temp)
			out.Rows = append(out.Rows, ParsedRawPoint{Time: t, Temperature: temp, Mass: m})
		}
		return out, diagnostics
	case LayerBlankCurve:
		var content BlankCurveContent
		mustUnmarshal(raw, &content)
		out := ParsedBlankCurve{}
		for i, row := range content.Rows {
			prefix := fmt.Sprintf("rows[%d]", i)
			t := ParseQuantity(prefix+".time", mustMarshal(row.Time))
			m := ParseQuantity(prefix+".mass", mustMarshal(row.Mass))
			add(t.Diagnostic, prefix+".time", t)
			add(m.Diagnostic, prefix+".mass", m)
			out.Rows = append(out.Rows, ParsedBlankPoint{Time: t, Mass: m})
		}
		return out, diagnostics
	case LayerDriftCorrection:
		var content DriftContent
		mustUnmarshal(raw, &content)
		s := ParseQuantity("slope", mustMarshal(content.Slope))
		b := ParseQuantity("intercept", mustMarshal(content.Intercept))
		add(s.Diagnostic, "slope", s)
		add(b.Diagnostic, "intercept", b)
		return ParsedDrift{Slope: s, Intercept: b, Model: content.Model}, diagnostics
	case LayerFurnaceProgram:
		var content FurnaceContent
		mustUnmarshal(raw, &content)
		out := ParsedFurnace{}
		for i, segment := range content.Segments {
			prefix := fmt.Sprintf("segments[%d]", i)
			st := ParseQuantity(prefix+".start", mustMarshal(segment.Start))
			en := ParseQuantity(prefix+".end", mustMarshal(segment.End))
			stt := ParseQuantity(prefix+".start_temp", mustMarshal(segment.StartTemp))
			et := ParseQuantity(prefix+".end_temp", mustMarshal(segment.EndTemp))
			out.Segments = append(out.Segments, ParsedFurnaceSegment{Start: st, End: en, StartTemp: stt, EndTemp: et, Kind: segment.Kind})
		}
		return out, diagnostics
	case LayerGasValveEvents:
		var content GasEventsContent
		mustUnmarshal(raw, &content)
		out := ParsedGasEvents{}
		for i, event := range content.Events {
			prefix := fmt.Sprintf("events[%d]", i)
			v := ParseQuantity(prefix+".valve_at", mustMarshal(event.ValveAt))
			d := ParseQuantity(prefix+".atmosphere_delay", mustMarshal(event.AtmosphereDelay))
			l := ParseQuantity(prefix+".temperature_lag", mustMarshal(event.TemperatureLag))
			e := ParseQuantity(prefix+".window_end_valve_at", mustMarshal(event.WindowEndValveAt))
			out.Events = append(out.Events, ParsedGasEvent{ID: event.ID, ValveAt: v, FromAtmosphere: event.FromAtmosphere, ToAtmosphere: event.ToAtmosphere, AtmosphereDelay: d, TemperatureLag: l, WindowEndValveAt: e})
		}
		return out, diagnostics
	case LayerInitialMass:
		var content InitialMassContent
		mustUnmarshal(raw, &content)
		m := ParseQuantity("mass", mustMarshal(content.Mass))
		return ParsedInitialMass{Mass: m, Method: content.Method}, diagnostics
	case LayerAnalysisParameters:
		var content AnalysisParameters
		mustUnmarshal(raw, &content)
		return ParsedParameters{SampleGapThreshold: ParseQuantity("sample_gap_threshold", mustMarshal(content.SampleGapThreshold)), PeakThreshold: ParseQuantity("peak_threshold", mustMarshal(content.PeakThreshold)), OverlapStrategy: content.OverlapStrategy, BlankGapThreshold: ParseQuantity("blank_gap_threshold", mustMarshal(content.BlankGapThreshold)), NoiseThreshold: ParseQuantity("noise_threshold", mustMarshal(content.NoiseThreshold))}, diagnostics
	default:
		return nil, []Diagnostic{{Code: "unknown_layer", Severity: "error", Path: "layer", Message: "unknown source layer: " + layer}}
	}
}

func mustUnmarshal(raw json.RawMessage, target any) { _ = json.Unmarshal(raw, target) }
func mustMarshal(value any) json.RawMessage         { raw, _ := json.Marshal(value); return raw }
