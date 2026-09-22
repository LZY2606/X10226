package domain

import "fmt"

func (b *engineBuilder) markGapsAndNoise() {
	if len(b.points) < 2 {
		return
	}
	threshold := b.params.SampleGapThreshold.Value
	for i := 0; i+1 < len(b.points); i++ {
		left := b.points[i]
		right := b.points[i+1]
		delta := right.Time - left.Time
		if delta > threshold {
			left.GapAfter = true
			b.rules = append(b.rules, RuleEvent{
				Code: "sampling_gap_no_crossing_interpolation", Status: "observed", Unit: "s",
				Basis: fmt.Sprintf("delta_t %.1f s exceeds threshold %.1f s", delta, threshold),
				At:    &left.Time, Detail: map[string]any{"left_point": pointID(i), "right_point": pointID(i + 1), "delta_s": delta, "threshold_s": threshold},
				SourceID: b.sourceID(LayerAnalysisParameters),
			})
		}
		if left.Corrected != nil && right.Corrected != nil && *right.Corrected > *left.Corrected {
			right.NonMonotonic = true
			b.rules = append(b.rules, RuleEvent{Code: "nonmonotonic_mass_retained", Status: "observed", Unit: "mg",
				Basis: "positive mass step is retained rather than smoothed away", At: &right.Time,
				Detail: map[string]any{"left_point": correctedID(left.Index), "right_point": correctedID(right.Index), "increase_mg": *right.Corrected - *left.Corrected, "noise_threshold_mg_per_min": b.params.NoiseThreshold.Value}})
		}
	}
}

func (b *engineBuilder) sourceID(layer string) string {
	if source, ok := b.sources[layer]; ok {
		return source.ID
	}
	return ""
}

func (b *engineBuilder) calculateDTG() {
	for i, point := range b.points {
		if point.Corrected == nil {
			continue
		}
		if i == 0 {
			b.addUnknownDTG(point, "first_point_no_forward_difference", "DTG needs an adjacent sample on the same side of a gap")
			continue
		}
		prev := b.points[i-1]
		if prev.GapAfter || prev.Corrected == nil {
			b.addUnknownDTG(point, "gap_prevents_dtg", "no derivative crosses the sampling gap")
			if i+1 < len(b.points) {
				next := b.points[i+1]
				if !point.GapAfter && next.Corrected != nil {
					b.addUnknownDTG(point, "segment_edge_no_crossing_dtg", "the first point after a gap is not differentiated across the gap")
				}
			}
			continue
		}
		if i+1 >= len(b.points) {
			b.addUnknownDTG(point, "last_point_no_backward_difference", "DTG needs an adjacent sample on the same side of a gap")
			continue
		}
		next := b.points[i+1]
		if point.GapAfter || next.Corrected == nil {
			b.addUnknownDTG(point, "gap_prevents_dtg", "no derivative crosses the sampling gap")
			continue
		}
		dtg := (*next.Corrected - *prev.Corrected) / ((next.Time - prev.Time) / 60)
		point.DTG = &dtg
		status := "ok"
		var diagnostics []Diagnostic
		if dtg > b.params.NoiseThreshold.Value {
			status = "nonmonotonic_noise"
			diagnostics = append(diagnostics, Diagnostic{Code: "positive_dtg_retained", Severity: "info", Path: fmt.Sprintf("rows[%d]", point.Index), Message: "positive DTG is retained and not smoothed or interpolated"})
		}
		b.addNode(ValueNode{ID: dtgID(point.Index), Key: fmt.Sprintf("dtg_point_%03d", point.Index), Kind: "dtg_point",
			Label: fmt.Sprintf("DTG %.0f s", point.Time), Value: map[string]any{"time_s": point.Time, "dtg_mg_per_min": dtg, "left": correctedID(prev.Index), "right": correctedID(next.Index)},
			Unit: "mg/min", Status: status, DirectSources: []SourceRef{b.sourceRef(LayerRawSamples, fmt.Sprintf("rows[%d]", prev.Index)), b.sourceRef(LayerRawSamples, fmt.Sprintf("rows[%d]", next.Index))},
			DependsOnNodes: []string{correctedID(prev.Index), correctedID(next.Index)},
			Formula:        "central difference: (corrected_mass[t+] - corrected_mass[t-]) / ((t+ - t-) / 60)", Diagnostics: diagnostics})
		b.bindings[fmt.Sprintf("dtg.point.%03d", point.Index)] = dtgID(point.Index)
	}
}

func (b *engineBuilder) addUnknownDTG(point *Point, code, reason string) {
	b.addNode(ValueNode{ID: dtgID(point.Index), Key: fmt.Sprintf("dtg_point_%03d", point.Index), Kind: "dtg_point",
		Label: fmt.Sprintf("DTG %.0f s（未知）", point.Time), Value: map[string]any{"time_s": point.Time, "dtg_mg_per_min": nil, "unknown_reason": reason},
		Unit: "mg/min", Status: "unknown", DirectSources: []SourceRef{b.sourceRef(LayerRawSamples, fmt.Sprintf("rows[%d]", point.Index))},
		DependsOnNodes: []string{correctedID(point.Index)}, Formula: "finite difference only inside one observed segment",
		Diagnostics: []Diagnostic{{Code: code, Severity: "info", Path: fmt.Sprintf("rows[%d]", point.Index), Message: reason}}})
	b.bindings[fmt.Sprintf("dtg.point.%03d", point.Index)] = dtgID(point.Index)
}

func (b *engineBuilder) furnaceAndGas() {
	if furnace, ok := b.parsed[LayerFurnaceProgram].(ParsedFurnace); ok {
		for i, segment := range furnace.Segments {
			start, _ := segment.Start.seconds()
			end, _ := segment.End.seconds()
			st, _ := segment.StartTemp.temperature()
			et, _ := segment.EndTemp.temperature()
			b.addNode(ValueNode{ID: fmt.Sprintf("furnace:segment:%03d", i), Key: fmt.Sprintf("furnace_segment_%03d", i), Kind: "furnace_program_segment",
				Label: fmt.Sprintf("炉程序 %s %.0f–%.0f s", segment.Kind, start, end),
				Value: map[string]any{"start_s": start, "end_s": end, "start_c": st, "end_c": et, "kind": segment.Kind, "ramp_c_per_min": func() float64 {
					if end <= start {
						return 0
					}
					return (et - st) / ((end - start) / 60)
				}()},
				Unit: "mixed", Basis: "furnace time and program temperature", DirectSources: []SourceRef{b.sourceRef(LayerFurnaceProgram, fmt.Sprintf("segments[%d]", i))}})
		}
	}
	gas, ok := b.parsed[LayerGasValveEvents].(ParsedGasEvents)
	if !ok {
		return
	}
	for i, event := range gas.Events {
		valve, _ := event.ValveAt.seconds()
		delay, _ := event.AtmosphereDelay.seconds()
		lag, _ := event.TemperatureLag.seconds()
		end, _ := event.WindowEndValveAt.seconds()
		effectiveStart := valve + delay
		effectiveEnd := end + delay
		reactionStart := effectiveStart + lag
		b.rules = append(b.rules, RuleEvent{Code: "valve_time_is_not_reaction_time", Status: "ok", Unit: "s", At: &valve,
			Basis:    "effective atmosphere interval adds transport delay; sample reaction evidence can additionally lag in temperature",
			Detail:   map[string]any{"gas_event_id": event.ID, "valve_at_s": valve, "atmosphere_delay_s": delay, "temperature_lag_s": lag, "effective_atmosphere_start_s": effectiveStart, "reaction_evidence_start_s": reactionStart, "effective_atmosphere_end_s": effectiveEnd},
			SourceID: b.sourceID(LayerGasValveEvents)})
		b.addNode(ValueNode{ID: fmt.Sprintf("gas:event:%s", safeID(event.ID, i)), Key: fmt.Sprintf("gas_event_%s", safeID(event.ID, i)), Kind: "atmosphere_window",
			Label: fmt.Sprintf("气氛 %s→%s（阀 %.0f s）", event.FromAtmosphere, event.ToAtmosphere, valve),
			Value: map[string]any{"gas_event_id": event.ID, "valve_at_s": valve, "effective_atmosphere_start_s": effectiveStart, "reaction_evidence_start_s": reactionStart, "effective_atmosphere_end_s": effectiveEnd, "from": event.FromAtmosphere, "to": event.ToAtmosphere},
			Unit:  "s", Basis: "furnace time; valve time plus atmosphere and temperature delays",
			DirectSources: []SourceRef{b.sourceRef(LayerGasValveEvents, fmt.Sprintf("events[%d]", i))}, Formula: "reaction_evidence_start = valve_at + atmosphere_delay + temperature_lag"})
		b.bindings["gas.event."+event.ID] = fmt.Sprintf("gas:event:%s", safeID(event.ID, i))
	}
}

func safeID(id string, fallback int) string {
	if id == "" {
		return fmt.Sprintf("%03d", fallback)
	}
	out := make([]rune, 0, len(id))
	for _, char := range id {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '_' || char == '-' {
			out = append(out, char)
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}
