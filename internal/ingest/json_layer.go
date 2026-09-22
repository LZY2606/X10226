package ingest

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"tga-review/internal/domain"
)

// jsonVal converts a generic decoded JSON value into its original textual
// representation. nil becomes the empty string.
func jsonVal(v any) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		return strconv.FormatBool(t)
	case float64:
		return strconv.FormatFloat(t, 'g', -1, 64)
	case json.Number:
		return t.String()
	default:
		b, _ := json.Marshal(t)
		return string(b)
	}
}

func jfield(m map[string]any, key string) any {
	if v, ok := m[key]; ok {
		return v
	}
	return nil
}

func basisOf(m map[string]any) domain.CoordinateBasis {
	b := domain.CoordinateBasis{TimeOrigin: "furnace_program_start", TimeUnit: "s", TempUnit: "degC", MassUnit: "mg"}
	if raw, ok := m["basis"].(map[string]any); ok {
		if s, ok := raw["time_origin"].(string); ok && s != "" {
			b.TimeOrigin = s
		}
		if s, ok := raw["time_unit"].(string); ok && s != "" {
			b.TimeUnit = s
		}
		if s, ok := raw["temp_unit"].(string); ok && s != "" {
			b.TempUnit = s
		}
		if s, ok := raw["mass_unit"].(string); ok && s != "" {
			b.MassUnit = s
		}
	}
	return b
}

// quantity parses one field to a Value and records a diagnostic when invalid.
func quantity(m map[string]any, key, category, layer string, idx int, diags *[]domain.Diagnostic) domain.Value {
	v := domain.ParseValue(jsonVal(jfield(m, key)), category)
	if !v.Valid && v.Raw != "" {
		*diags = append(*diags, diag(domain.SevError, "unconvertible_value",
			fmt.Sprintf("%s: %s", key, v.Error), layer, idx, key))
	}
	return v
}

func jindex(m map[string]any, fallback int) int {
	if v, ok := m["index"].(float64); ok {
		return int(v)
	}
	return fallback
}

func parseJSONLayer(name string, body json.RawMessage) (any, []domain.Diagnostic, error) {
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(string(body)))
	dec.UseNumber()
	if err := dec.Decode(&m); err != nil {
		return nil, nil, fmt.Errorf("decode %s: %w", name, err)
	}
	var diags []domain.Diagnostic

	switch name {
	case domain.LayerSamplePoints:
		raw, _ := m["points"].([]any)
		if raw == nil {
			return nil, diags, fmt.Errorf("missing points array")
		}
		layer := &domain.SamplePointsLayer{Basis: basisOf(m)}
		for i, item := range raw {
			pm, ok := item.(map[string]any)
			if !ok {
				diags = append(diags, diag(domain.SevError, "bad_point", "point is not an object", name, i, ""))
				continue
			}
			idx := jindex(pm, i)
			timeRaw, massRaw := jsonVal(jfield(pm, "time")), jsonVal(jfield(pm, "mass"))
			tempRaw := jsonVal(jfield(pm, "temp"))
			sp := domain.SamplePoint{Index: idx, TimeRaw: timeRaw, MassRaw: massRaw, TempRaw: tempRaw}
			sp.Time = domain.ParseValue(timeRaw, "time")
			sp.Mass = domain.ParseValue(massRaw, "mass")
			sp.Temp = domain.ParseValue(tempRaw, "temp")
			if !sp.Time.Valid && sp.TimeRaw != "" {
				diags = append(diags, diag(domain.SevError, "unconvertible_value", "time: "+sp.Time.Error, name, idx, "time"))
			}
			if !sp.Temp.Valid && sp.TempRaw != "" {
				diags = append(diags, diag(domain.SevError, "unconvertible_value", "temp: "+sp.Temp.Error, name, idx, "temp"))
			}
			sp.Present = sp.Mass.Valid
			if !sp.Present {
				diags = append(diags, diag(domain.SevWarning, "sample_gap",
					"missing or unreadable mass; point kept as a gap", name, idx, "mass"))
			}
			layer.Points = append(layer.Points, sp)
		}
		return layer, diags, nil

	case domain.LayerDriftCorr:
		layer := &domain.DriftCorrectionLayer{
			Model:        jsonVal(jfield(m, "model")),
			SlopeRaw:     jsonVal(jfield(m, "slope")),
			InterceptRaw: jsonVal(jfield(m, "intercept")),
		}
		if layer.Model == "" {
			layer.Model = "linear"
		}
		if layer.Model != "linear" {
			diags = append(diags, diag(domain.SevError, "unknown_drift_model",
				"only linear baseline drift is supported", name, 0, "model"))
		}
		layer.Slope = quantity(m, "slope", "rate", name, 0, &diags)
		layer.Intercept = quantity(m, "intercept", "mass", name, 0, &diags)
		layer.FitRMSE = quantity(m, "fit_rmse", "mass", name, 0, &diags)
		layer.AppliedToBlank = jsonVal(jfield(m, "applied_to_blank"))
		if anchors, ok := m["fit_anchors"].([]any); ok {
			for i, item := range anchors {
				am, _ := item.(map[string]any)
				if am == nil {
					continue
				}
				dp := domain.DriftPoint{Index: jindex(am, i), TimeRaw: jsonVal(jfield(am, "time")), MassRaw: jsonVal(jfield(am, "mass"))}
				dp.Time = quantity(am, "time", "time", name, i, &diags)
				dp.Mass = quantity(am, "mass", "mass", name, i, &diags)
				layer.FitAnchors = append(layer.FitAnchors, dp)
			}
		}
		return layer, diags, nil

	case domain.LayerFurnaceProg:
		raw, _ := m["segments"].([]any)
		if raw == nil {
			return nil, diags, fmt.Errorf("missing segments array")
		}
		layer := &domain.FurnaceProgramLayer{}
		for i, item := range raw {
			sm, ok := item.(map[string]any)
			if !ok {
				diags = append(diags, diag(domain.SevError, "bad_segment", "segment is not an object", name, i, ""))
				continue
			}
			seg := domain.FurnaceSegment{
				Index:        jindex(sm, i),
				Name:         jsonVal(jfield(sm, "name")),
				StartTRaw:    jsonVal(jfield(sm, "start_t")),
				EndTRaw:      jsonVal(jfield(sm, "end_t")),
				StartTempRaw: jsonVal(jfield(sm, "start_temp")),
				EndTempRaw:   jsonVal(jfield(sm, "end_temp")),
			}
			seg.StartT = quantity(sm, "start_t", "time", name, i, &diags)
			seg.EndT = quantity(sm, "end_t", "time", name, i, &diags)
			seg.StartTemp = quantity(sm, "start_temp", "temp", name, i, &diags)
			seg.EndTemp = quantity(sm, "end_temp", "temp", name, i, &diags)
			layer.Segments = append(layer.Segments, seg)
		}
		return layer, diags, nil

	case domain.LayerGasValves:
		raw, _ := m["events"].([]any)
		if raw == nil {
			return nil, diags, fmt.Errorf("missing events array")
		}
		layer := &domain.GasValvesLayer{}
		for i, item := range raw {
			em, ok := item.(map[string]any)
			if !ok {
				diags = append(diags, diag(domain.SevError, "bad_event", "gas event is not an object", name, i, ""))
				continue
			}
			ge := domain.GasEvent{
				Index:   jindex(em, i),
				TimeRaw: jsonVal(jfield(em, "time")),
				Action:  jsonVal(jfield(em, "action")),
				Gas:     jsonVal(jfield(em, "gas")),
			}
			ge.Time = quantity(em, "time", "time", name, i, &diags)
			switch ge.Action {
			case "open", "close", "switch":
			default:
				diags = append(diags, diag(domain.SevError, "bad_action",
					"gas action must be open|close|switch", name, i, "action"))
			}
			layer.Events = append(layer.Events, ge)
		}
		return layer, diags, nil

	case domain.LayerBlankCurve:
		raw, _ := m["points"].([]any)
		if raw == nil {
			return nil, diags, fmt.Errorf("missing points array")
		}
		layer := &domain.BlankCurveLayer{Basis: basisOf(m)}
		for i, item := range raw {
			pm, ok := item.(map[string]any)
			if !ok {
				diags = append(diags, diag(domain.SevError, "bad_point", "blank point is not an object", name, i, ""))
				continue
			}
			dp := domain.DriftPoint{Index: jindex(pm, i), TimeRaw: jsonVal(jfield(pm, "time")), MassRaw: jsonVal(jfield(pm, "mass"))}
			dp.Time = quantity(pm, "time", "time", name, i, &diags)
			dp.Mass = quantity(pm, "mass", "mass", name, i, &diags)
			layer.Points = append(layer.Points, dp)
		}
		return layer, diags, nil

	case domain.LayerInitialMass:
		layer := &domain.InitialMassLayer{
			MassRaw:       jsonVal(jfield(m, "mass")),
			Method:        jsonVal(jfield(m, "method")),
			ParentVersion: jsonVal(jfield(m, "parent_version")),
			Reason:        jsonVal(jfield(m, "reason")),
		}
		layer.Mass = quantity(m, "mass", "mass", name, 0, &diags)
		if layer.Mass.Raw == "" {
			return nil, diags, fmt.Errorf("initial mass is required")
		}
		return layer, diags, nil
	}
	return nil, nil, fmt.Errorf("unknown layer %q", name)
}
