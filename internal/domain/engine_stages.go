package domain

import (
	"encoding/json"
	"fmt"
)

func (b *engineBuilder) addRequiredRules() {
	b.rules = []RuleEvent{
		{Code: "stages_may_overlap", Status: "active", Basis: "overlapping peak windows can each be legal interpretations; stage losses are not forced to sum to 100%", Detail: map[string]any{"normalization": "mass loss is reported against separately versioned initial mass", "overlap_policy": b.params.OverlapStrategy}},
		{Code: "gaps_do_not_interpolate", Status: "active", Basis: "no mass, DTG, stage boundary, or blank correction is synthesized across a sampling gap", Detail: map[string]any{"sample_gap_threshold_s": b.params.SampleGapThreshold.Value, "blank_gap_threshold_s": b.params.BlankGapThreshold.Value}},
		{Code: "valve_time_is_not_reaction_time", Status: "active", Basis: "gas transport delay and temperature lag are represented separately from valve events", Detail: map[string]any{"timeline_coordinate": "furnace_time_s"}},
	}
}

func (b *engineBuilder) segments() [][]*Point {
	var groups [][]*Point
	var current []*Point
	for _, point := range b.points {
		if point.Corrected == nil {
			continue
		}
		if len(current) > 0 && current[len(current)-1].GapAfter {
			groups = append(groups, current)
			current = nil
		}
		current = append(current, point)
	}
	if len(current) > 0 {
		groups = append(groups, current)
	}
	return groups
}

func (b *engineBuilder) detectStages() {
	if len(b.points) < 3 {
		return
	}
	threshold := -mathAbs(b.params.PeakThreshold.Value)
	stageSequence := 0
	for _, group := range b.segments() {
		indexInGroup := map[int]int{}
		for i, point := range group {
			indexInGroup[point.Index] = i
		}
		var peaks []*Point
		for i := 1; i+1 < len(group); i++ {
			point := group[i]
			if point.DTG == nil || group[i-1].DTG == nil || group[i+1].DTG == nil {
				continue
			}
			if *point.DTG <= threshold && *point.DTG <= *group[i-1].DTG && *point.DTG <= *group[i+1].DTG {
				peaks = append(peaks, point)
			}
		}
		for peakOrder, peak := range peaks {
			stageSequence++
			b.addStageInterpretations(group, peak, peakOrder, len(peaks) > 1, stageSequence)
		}
		if len(group) > 0 {
			// A group boundary is a visible edge; the next group is connected only in time.
		}
	}
	b.addGapDifferenceCandidates()
}

func (b *engineBuilder) addStageInterpretations(group []*Point, peak *Point, peakOrder int, hasSiblingPeaks bool, stageSequence int) {
	specs := []struct {
		name, suffix, kind string
		radius             float64
	}{
		{"narrow", "narrow", "narrow_stage_candidate", 60},
		{"broad", "broad", "overlapping_shoulder_candidate", 120},
	}
	for specOrder, spec := range specs {
		startPoint := nearestPointAtOrBefore(group, peak.Time-spec.radius)
		endPoint := nearestPointAtOrAfter(group, peak.Time+spec.radius)
		if startPoint == nil {
			startPoint = group[0]
		}
		if endPoint == nil {
			endPoint = group[len(group)-1]
		}
		loss := *endPoint.Corrected - *startPoint.Corrected
		percent := b.percentInitial(loss)
		overlap, overlaps := b.atmosphereRelation(startPoint.Time, endPoint.Time)
		id := fmt.Sprintf("stage:s%02d:%s", stageSequence, spec.suffix)
		candidate := StageCandidate{ID: id, Kind: spec.kind, PeakTime: &peak.Time, StartObserved: startPoint.Time, EndObserved: endPoint.Time,
			StartID: correctedID(startPoint.Index), EndID: correctedID(endPoint.Index), MassLoss: &loss, PercentLoss: percent,
			AtmosphereOverlap: overlap, Status: "legal_candidate", Overlaps: overlaps}
		if spec.suffix == "broad" && !hasSiblingPeaks {
			candidate.Status = "legal_candidate"
		}
		b.stages = append(b.stages, candidate)
		_ = specOrder
		_ = peakOrder
		value := map[string]any{
			"peak_time_s": peak.Time, "start_observed_s": startPoint.Time, "end_observed_s": endPoint.Time,
			"start_point": candidate.StartID, "end_point": candidate.EndID, "mass_loss_mg": loss,
			"mass_loss_percent_of_initial_mass": percent, "atmosphere_relation": overlap, "overlapping_candidates": overlaps,
			"interpretation": spec.name, "sum_constraint": "not_required_to_equal_100_percent",
		}
		sources := []SourceRef{b.sourceRef(LayerRawSamples, "rows"), b.sourceRef(LayerAnalysisParameters, "peak_threshold")}
		if _, ok := b.sources[LayerGasValveEvents]; ok {
			sources = append(sources, b.sourceRef(LayerGasValveEvents, "events"))
		}
		b.addNode(ValueNode{ID: id, Key: id, Kind: "stage_candidate", Label: fmt.Sprintf("阶段候选 S%d/%s（峰 %.0f s）", stageSequence, spec.name, peak.Time),
			Value: value, Unit: "mixed", Basis: "observed corrected mass boundaries; no cross-gap synthesis", Status: candidate.Status,
			DirectSources: sources, DependsOnNodes: []string{candidate.StartID, candidate.EndID, dtgID(peak.Index)},
			Formula: "observed_mass(end) - observed_mass(start); percent = 100 * loss / independently versioned initial mass"})
	}
}

func nearestPointAtOrBefore(group []*Point, time float64) *Point {
	var best *Point
	for _, point := range group {
		if point.Time <= time && (best == nil || point.Time > best.Time) {
			best = point
		}
	}
	return best
}

func nearestPointAtOrAfter(group []*Point, time float64) *Point {
	var best *Point
	for _, point := range group {
		if point.Time >= time && (best == nil || point.Time < best.Time) {
			best = point
		}
	}
	return best
}

func (b *engineBuilder) percentInitial(loss float64) *float64 {
	initial, ok := b.parsed[LayerInitialMass].(ParsedInitialMass)
	if !ok || initial.Mass.Status != "ok" || initial.Mass.Value == 0 {
		return nil
	}
	value := 100 * loss / initial.Mass.Value
	return &value
}

func (b *engineBuilder) atmosphereRelation(start, end float64) (string, []string) {
	gas, ok := b.parsed[LayerGasValveEvents].(ParsedGasEvents)
	if !ok {
		return "no_atmosphere_source", nil
	}
	relations := []string{}
	overlapIDs := []string{}
	for i, event := range gas.Events {
		valve, _ := event.ValveAt.seconds()
		delay, _ := event.AtmosphereDelay.seconds()
		lag, _ := event.TemperatureLag.seconds()
		endValve, _ := event.WindowEndValveAt.seconds()
		effectiveStart := valve + delay
		effectiveEnd := endValve + delay
		reactionStart := effectiveStart + lag
		id := fmt.Sprintf("gas:event:%s", safeID(event.ID, i))
		if end < reactionStart || start > effectiveEnd {
			relations = append(relations, event.ToAtmosphere+":outside")
		} else if start >= reactionStart && end <= effectiveEnd {
			relations = append(relations, event.ToAtmosphere+":overlap")
			overlapIDs = append(overlapIDs, id)
		} else if b.windowCrossesGap(reactionStart, effectiveEnd) || b.windowCrossesGap(start, end) {
			relations = append(relations, event.ToAtmosphere+":unknown_across_gap")
			overlapIDs = append(overlapIDs, id)
		} else {
			relations = append(relations, event.ToAtmosphere+":partial")
			overlapIDs = append(overlapIDs, id)
		}
	}
	if len(overlapIDs) > 0 {
		return "partial_or_unknown", overlapIDs
	}
	return joinRelations(relations), nil
}

func joinRelations(values []string) string {
	out := ""
	for i, value := range values {
		if i > 0 {
			out += ";"
		}
		out += value
	}
	if out == "" {
		return "outside"
	}
	return out
}

func (b *engineBuilder) windowCrossesGap(start, end float64) bool {
	for _, point := range b.points {
		if point.GapAfter {
			next := b.points[point.Index+1]
			if start < next.Time && end > point.Time {
				return true
			}
		}
	}
	return false
}

func (b *engineBuilder) addGapDifferenceCandidates() {
	gapNumber := 0
	for _, point := range b.points {
		if !point.GapAfter {
			continue
		}
		gapNumber++
		next := b.points[point.Index+1]
		delta := *next.Corrected - *point.Corrected
		id := fmt.Sprintf("gap:difference:%03d", gapNumber)
		b.addNode(ValueNode{ID: id, Key: id, Kind: "gap_mass_difference", Label: fmt.Sprintf("缺口两侧质量差 %.0f–%.0f s", point.Time, next.Time),
			Value: map[string]any{"left_observed_s": point.Time, "right_observed_s": next.Time, "delta_mg": delta, "assigned_stage": nil, "interpolation": "not_performed"},
			Unit:  "mg", Status: "rejected_as_stage", DirectSources: []SourceRef{b.sourceRef(LayerRawSamples, fmt.Sprintf("rows[%d]", point.Index)), b.sourceRef(LayerRawSamples, fmt.Sprintf("rows[%d]", next.Index))},
			DependsOnNodes: []string{correctedID(point.Index), correctedID(next.Index)}, Formula: "observed endpoints may differ, but the difference is not assigned across a gap",
			Diagnostics: []Diagnostic{{Code: "gap_candidate_rejected", Severity: "info", Path: id, Message: "legal endpoint difference retained, but no unique stage decomposition exists inside the gap"}}})
		b.rules = append(b.rules, RuleEvent{Code: "gap_difference_not_a_stage", Status: "rejected", Unit: "mg", At: &point.Time,
			Basis:  "candidate crossing a sampling gap is retained as rejected; all within-gap interpretations remain unknown",
			Detail: map[string]any{"left_s": point.Time, "right_s": next.Time, "observed_delta_mg": delta, "rejected_subject": id}})
	}
}

func (b *engineBuilder) finalResidue() {
	initialParsed, ok := b.parsed[LayerInitialMass].(ParsedInitialMass)
	if !ok || len(b.points) == 0 {
		return
	}
	final := b.points[len(b.points)-1]
	if final.Corrected == nil {
		return
	}
	initial := initialParsed.Mass.Value
	rate := 100 * *final.Corrected / initial
	chainID := "residue:final:mass"
	b.addNode(ValueNode{ID: chainID, Key: "final_residual_mass", Kind: "residual_mass", Label: "最终残留质量",
		Value: map[string]any{"time_s": final.Time, "mass_mg": *final.Corrected, "source_point": correctedID(final.Index)}, Unit: "mg",
		Status: "ok", DirectSources: []SourceRef{b.sourceRef(LayerInitialMass, "mass"), b.sourceRef(LayerRawSamples, fmt.Sprintf("rows[%d]", final.Index))},
		DependsOnNodes: []string{correctedID(final.Index)}, Formula: "last observed corrected mass; no extrapolation after it"})
	b.addNode(ValueNode{ID: "residue:final:percent", Key: "final_residue_percent", Kind: "residue_rate", Label: "最终残留率",
		Value: map[string]any{"initial_mass_mg": initial, "final_mass_mg": *final.Corrected, "residue_percent": rate, "initial_mass_source": b.sourceRef(LayerInitialMass, "mass"), "final_mass_chain": chainID, "stage_percent_sum_constraint": "none"},
		Unit:  "%", Basis: "percent of versioned initial sample mass", Status: "ok",
		DirectSources: []SourceRef{b.sourceRef(LayerInitialMass, "mass")}, DependsOnNodes: []string{chainID},
		Formula: "residue_percent = 100 * final_corrected_mass_mg / initial_mass_mg; stage losses are not normalized to total 100%"})
	b.bindings["final_residual_mass"] = chainID
	b.bindings["final_residue_percent"] = "residue:final:percent"
}

func (b *engineBuilder) addRuleNodes() {
	for i, rule := range b.rules {
		raw, _ := jsonMarshal(rule)
		b.addNode(ValueNode{ID: fmt.Sprintf("rule:%03d:%s", i+1, ShortFingerprint(rule.Code, rule.At, rule.Detail)), Key: fmt.Sprintf("machine_rule_%03d", i+1), Kind: "machine_readable_rule",
			Label: rule.Code, Value: raw, Unit: rule.Unit, Status: rule.Status, Basis: rule.Basis,
			DirectSources: []SourceRef{{SourceID: rule.SourceID}}, Formula: rule.Basis})
	}
}

func mathAbs(value float64) float64 {
	if value < 0 {
		return -value
	}
	return value
}

func jsonMarshal(value any) ([]byte, error) {
	return json.Marshal(value)
}
