package ingest

import (
	"fmt"
	"strings"

	"tga-review/internal/domain"
)

var gapTokens = map[string]bool{"gap": true, "-": true, "na": true, "n/a": true, "missing": true, "null": true, "?": true}

func parseTextLayer(name string, lines []string) (any, []domain.Diagnostic, error) {
	opts, data := sectionOpts(lines)
	var diags []domain.Diagnostic

	switch name {
	case domain.LayerSamplePoints:
		layer := &domain.SamplePointsLayer{Basis: domain.CoordinateBasis{
			TimeOrigin: "furnace_program_start",
			TimeUnit:   optStr(opts, "time_unit", "s"),
			TempUnit:   optStr(opts, "temp_unit", "degC"),
			MassUnit:   optStr(opts, "mass_unit", "mg"),
		}}
		if len(data) == 0 {
			return nil, diags, fmt.Errorf("no sample points")
		}
		for i, l := range data {
			m, pos := kv(l)
			timeRaw, massRaw, tempRaw := get3(m, pos, "time", "mass", "temp")
			sp := domain.SamplePoint{Index: i, TimeRaw: timeRaw, MassRaw: massRaw, TempRaw: tempRaw}
			sp.Time = domain.ParseValue(timeRaw, "time")
			sp.Temp = domain.ParseValue(tempRaw, "temp")
			if gapTokens[strings.ToLower(strings.TrimSpace(massRaw))] {
				diags = append(diags, diag(domain.SevWarning, "sample_gap", "missing mass; point kept as a gap", name, i, "mass"))
			} else {
				sp.Mass = domain.ParseValue(massRaw, "mass")
				if !sp.Mass.Valid {
					diags = append(diags, diag(domain.SevWarning, "sample_gap", "unreadable mass; point kept as a gap: "+sp.Mass.Error, name, i, "mass"))
				}
			}
			sp.Present = sp.Mass.Valid
			if !sp.Time.Valid && sp.TimeRaw != "" {
				diags = append(diags, diag(domain.SevError, "unconvertible_value", "time: "+sp.Time.Error, name, i, "time"))
			}
			if !sp.Temp.Valid && sp.TempRaw != "" {
				diags = append(diags, diag(domain.SevError, "unconvertible_value", "temp: "+sp.Temp.Error, name, i, "temp"))
			}
			layer.Points = append(layer.Points, sp)
		}
		return layer, diags, nil

	case domain.LayerDriftCorr:
		layer := &domain.DriftCorrectionLayer{Model: "linear"}
		if len(data) == 0 {
			return nil, diags, fmt.Errorf("no drift model")
		}
		for _, l := range data {
			m, _ := kv(l)
			if v, ok := m["model"]; ok {
				layer.Model = v
			}
			if v, ok := m["slope"]; ok {
				layer.SlopeRaw = v
			}
			if v, ok := m["intercept"]; ok {
				layer.InterceptRaw = v
			}
			if v, ok := m["fit_rmse"]; ok {
				layer.FitRMSE = domain.ParseValue(v, "mass")
			}
			if v, ok := m["applied_to_blank"]; ok {
				layer.AppliedToBlank = v
			}
			if _, ok := m["anchor"]; ok {
				dp := domain.DriftPoint{TimeRaw: m["time"], MassRaw: m["mass"]}
				dp.Time = domain.ParseValue(m["time"], "time")
				dp.Mass = domain.ParseValue(m["mass"], "mass")
				layer.FitAnchors = append(layer.FitAnchors, dp)
			}
		}
		if layer.Model != "linear" {
			diags = append(diags, diag(domain.SevError, "unknown_drift_model", "only linear baseline drift is supported", name, 0, "model"))
		}
		layer.Slope = domain.ParseValue(layer.SlopeRaw, "rate")
		layer.Intercept = domain.ParseValue(layer.InterceptRaw, "mass")
		if !layer.Slope.Valid || !layer.Intercept.Valid {
			return nil, diags, fmt.Errorf("drift correction needs slope and intercept")
		}
		return layer, diags, nil

	case domain.LayerFurnaceProg:
		layer := &domain.FurnaceProgramLayer{}
		if len(data) == 0 {
			return nil, diags, fmt.Errorf("no furnace segments")
		}
		for i, l := range data {
			m, pos := kv(l)
			st, et, stmp := get3(m, pos, "start_t", "end_t", "start_temp")
			etmp := valOr(m, "end_temp", fourth(pos))
			seg := domain.FurnaceSegment{
				Index:        i,
				Name:         valOr(m, "name", ""),
				StartTRaw:    st,
				EndTRaw:      et,
				StartTempRaw: stmp,
				EndTempRaw:   etmp,
			}
			seg.StartT = domain.ParseValue(st, "time")
			seg.EndT = domain.ParseValue(et, "time")
			seg.StartTemp = domain.ParseValue(stmp, "temp")
			seg.EndTemp = domain.ParseValue(etmp, "temp")
			layer.Segments = append(layer.Segments, seg)
		}
		return layer, diags, nil

	case domain.LayerGasValves:
		layer := &domain.GasValvesLayer{}
		if len(data) == 0 {
			return nil, diags, fmt.Errorf("no gas events")
		}
		for i, l := range data {
			m, pos := kv(l)
			t := valOr(m, "time", first(pos))
			action := valOr(m, "action", second(pos))
			gas := valOr(m, "gas", third(pos))
			ge := domain.GasEvent{Index: i, TimeRaw: t, Action: action, Gas: gas, Time: domain.ParseValue(t, "time")}
			switch action {
			case "open", "close", "switch":
			default:
				diags = append(diags, diag(domain.SevError, "bad_action", "gas action must be open|close|switch", name, i, "action"))
			}
			layer.Events = append(layer.Events, ge)
		}
		return layer, diags, nil

	case domain.LayerBlankCurve:
		layer := &domain.BlankCurveLayer{Basis: domain.CoordinateBasis{
			TimeOrigin: "furnace_program_start",
			TimeUnit:   optStr(opts, "time_unit", "s"),
			MassUnit:   optStr(opts, "mass_unit", "mg"),
		}}
		if len(data) == 0 {
			return nil, diags, fmt.Errorf("no blank points")
		}
		for i, l := range data {
			m, pos := kv(l)
			t, ms := valOr(m, "time", first(pos)), valOr(m, "mass", second(pos))
			dp := domain.DriftPoint{Index: i, TimeRaw: t, MassRaw: ms}
			dp.Time = domain.ParseValue(t, "time")
			dp.Mass = domain.ParseValue(ms, "mass")
			layer.Points = append(layer.Points, dp)
		}
		return layer, diags, nil

	case domain.LayerInitialMass:
		layer := &domain.InitialMassLayer{}
		if len(data) == 0 {
			return nil, diags, fmt.Errorf("no initial mass")
		}
		for _, l := range data {
			m, _ := kv(l)
			if v, ok := m["mass"]; ok {
				layer.MassRaw = v
			}
			if v, ok := m["method"]; ok {
				layer.Method = v
			}
			if v, ok := m["parent_version"]; ok {
				layer.ParentVersion = v
			}
			if v, ok := m["reason"]; ok {
				layer.Reason = v
			}
		}
		layer.Mass = domain.ParseValue(layer.MassRaw, "mass")
		if !layer.Mass.Valid {
			return nil, diags, fmt.Errorf("initial mass unreadable: %s", layer.Mass.Error)
		}
		return layer, diags, nil
	}
	return nil, nil, fmt.Errorf("unknown layer %q", name)
}

func first(p []string) string  { return pick(p, 0) }
func second(p []string) string { return pick(p, 1) }
func third(p []string) string  { return pick(p, 2) }
func fourth(p []string) string { return pick(p, 3) }

func pick(p []string, i int) string {
	if i < len(p) {
		return p[i]
	}
	return ""
}

func valOr(m map[string]string, k, def string) string {
	if v, ok := m[k]; ok {
		return v
	}
	return def
}

func get3(m map[string]string, p []string, k1, k2, k3 string) (string, string, string) {
	return valOr(m, k1, first(p)), valOr(m, k2, second(p)), valOr(m, k3, third(p))
}
