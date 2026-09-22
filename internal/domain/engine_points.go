package domain

import (
	"fmt"
	"math"
)

func (b *engineBuilder) defaultParams() {
	gap := 90.0
	blankGap := 180.0
	peak := 0.4
	noise := 0.005
	b.params = ParsedParameters{
		SampleGapThreshold: ParsedQuantity{Value: gap, Unit: "s", Status: "ok"},
		BlankGapThreshold:  ParsedQuantity{Value: blankGap, Unit: "s", Status: "ok"},
		PeakThreshold:      ParsedQuantity{Value: peak, Unit: "mg/min", Status: "ok"},
		NoiseThreshold:     ParsedQuantity{Value: noise, Unit: "mg/min", Status: "ok"},
		OverlapStrategy:    "retain_narrow_and_broad",
	}
}

func (b *engineBuilder) sourceRef(layer, path string) SourceRef {
	source := b.sources[layer]
	return SourceRef{SourceID: source.ID, Layer: layer, Version: source.Version, Path: path}
}

func (b *engineBuilder) addNode(node ValueNode) {
	node.AlgorithmVersion = AlgorithmVersion
	node.ParameterHash = b.hash
	if node.DirectSources == nil {
		node.DirectSources = []SourceRef{}
	}
	if node.Status == "" {
		node.Status = "ok"
	}
	b.nodes = append(b.nodes, node)
}

func (b *engineBuilder) buildPoints() {
	raw, ok := b.parsed[LayerRawSamples].(ParsedRawSamples)
	if !ok {
		return
	}
	for i, row := range raw.Rows {
		t, okTime := row.Time.seconds()
		m, okMass := row.Mass.mass()
		if !okTime || !okMass {
			b.addNode(ValueNode{
				ID:  "raw:point:" + b.sourceVersion(LayerRawSamples) + ":" + fmt.Sprintf("%03d", i),
				Key: fmt.Sprintf("raw_point_%03d", i), Kind: "raw_sample_point", Label: fmt.Sprintf("原始采样点 #%d", i+1),
				Value:  map[string]any{"time": row.Time, "mass": row.Mass, "temperature": row.Temperature},
				Status: "unconvertible", DirectSources: []SourceRef{b.sourceRef(LayerRawSamples, fmt.Sprintf("rows[%d]", i))},
				Diagnostics: []Diagnostic{{Code: "unconvertible_point", Severity: "warning", Path: fmt.Sprintf("rows[%d]", i), Message: "point retained at original location and not used for derivation"}},
			})
			continue
		}
		point := &Point{Index: i, Time: t, RawMass: m}
		if temp, okTemp := row.Temperature.temperature(); okTemp {
			point.Temperature = &temp
		}
		b.points = append(b.points, point)
		b.bindings[fmt.Sprintf("raw.point.%03d", i)] = pointID(i)
		b.addNode(ValueNode{
			ID: pointID(i), Key: fmt.Sprintf("raw_point_%03d", i), Kind: "raw_sample_point",
			Label: fmt.Sprintf("原始采样点 #%d（炉时间 %.0f s）", i+1, t),
			Value: map[string]any{"time_s": t, "time": row.Time, "mass_mg": m, "mass": row.Mass, "temperature_c": point.Temperature, "temperature": row.Temperature},
			Unit:  "mixed", Basis: "furnace time seconds; balance mass mg",
			DirectSources: []SourceRef{b.sourceRef(LayerRawSamples, fmt.Sprintf("rows[%d]", i))},
		})
	}
}

func pointID(index int) string     { return fmt.Sprintf("raw:point:v:%03d", index) }
func correctedID(index int) string { return fmt.Sprintf("corrected:point:%03d", index) }
func dtgID(index int) string       { return fmt.Sprintf("dtg:point:%03d", index) }

func (b *engineBuilder) sourceVersion(layer string) string {
	if source, ok := b.sources[layer]; ok {
		return fmt.Sprintf("%s-v%d", source.ID, source.Version)
	}
	return "missing"
}

func (b *engineBuilder) buildBlank() {
	blank, ok := b.parsed[LayerBlankCurve].(ParsedBlankCurve)
	if !ok {
		return
	}
	b.blank = blank.Rows
	for i, row := range blank.Rows {
		t, okTime := row.Time.seconds()
		m, okMass := row.Mass.mass()
		b.addNode(ValueNode{
			ID: fmt.Sprintf("blank:point:%03d", i), Key: fmt.Sprintf("blank_point_%03d", i), Kind: "blank_curve_point",
			Label: fmt.Sprintf("空白曲线点 %.0f s", t), Value: map[string]any{"time_s": t, "mass_mg": m, "time": row.Time, "mass": row.Mass},
			Status:        map[bool]string{true: "ok", false: "unconvertible"}[okTime && okMass],
			DirectSources: []SourceRef{b.sourceRef(LayerBlankCurve, fmt.Sprintf("rows[%d]", i))},
		})
	}
}

func (b *engineBuilder) blankAt(time float64, index int) (*float64, bool) {
	threshold := b.params.BlankGapThreshold.Value
	for i := 0; i+1 < len(b.blank); i++ {
		left, leftOK := b.blank[i].Time.seconds()
		right, rightOK := b.blank[i+1].Time.seconds()
		lm, lmOK := b.blank[i].Mass.mass()
		rm, rmOK := b.blank[i+1].Mass.mass()
		if !leftOK || !rightOK || !lmOK || !rmOK {
			continue
		}
		if time >= left && time <= right {
			if right-left > threshold {
				return nil, false
			}
			if right == left {
				value := lm
				return &value, true
			}
			value := lm + (rm-lm)*(time-left)/(right-left)
			return &value, true
		}
	}
	for _, row := range b.blank {
		t, ok := row.Time.seconds()
		m, okMass := row.Mass.mass()
		if ok && okMass && math.Abs(t-time) < 1e-9 {
			value := m
			return &value, true
		}
	}
	return nil, false
}

func (b *engineBuilder) applyCorrections() {
	if len(b.points) == 0 {
		return
	}
	var drift ParsedDrift
	if parsed, ok := b.parsed[LayerDriftCorrection].(ParsedDrift); ok {
		drift = parsed
	}
	for index, point := range b.points {
		offset := 0.0
		driftOffset := driftSlope(drift)*point.Time/60 + driftIntercept(drift)
		point.Drift = driftOffset
		blankValue, blankOK := b.blankAt(point.Time, index)
		if blankOK {
			point.Blank = blankValue
			offset += *blankValue
		}
		corrected := point.RawMass - offset - driftOffset
		point.Corrected = &corrected
		sources := []SourceRef{b.sourceRef(LayerRawSamples, fmt.Sprintf("rows[%d]", point.Index))}
		if _, ok := b.sources[LayerDriftCorrection]; ok {
			sources = append(sources, b.sourceRef(LayerDriftCorrection, "slope"), b.sourceRef(LayerDriftCorrection, "intercept"))
		}
		if blankOK {
			sources = append(sources, b.sourceRef(LayerBlankCurve, "rows"))
		}
		value := map[string]any{
			"time_s": point.Time, "raw_mass_mg": point.RawMass, "blank_mg": blankValue,
			"balance_drift_mg": driftOffset, "corrected_mass_mg": corrected,
			"formula": "corrected = observed_mass - interpolated_blank - (drift_slope_mg_per_min * furnace_time_min + drift_intercept_mg)",
		}
		status := "ok"
		diagnostics := []Diagnostic{}
		if !blankOK {
			status = "blank_missing"
			diagnostics = append(diagnostics, Diagnostic{Code: "blank_not_subtracted", Severity: "info", Path: fmt.Sprintf("rows[%d]", point.Index), Message: "blank correction unavailable; drift correction remains independently inspectable"})
		}
		b.addNode(ValueNode{ID: correctedID(point.Index), Key: fmt.Sprintf("corrected_point_%03d", point.Index), Kind: "corrected_point",
			Label: fmt.Sprintf("校正质量 %.0f s", point.Time), Value: value, Unit: "mg", Basis: "corrected sample mass", Status: status,
			DirectSources: sources, DependsOnNodes: []string{pointID(point.Index)}, Formula: value["formula"].(string), Diagnostics: diagnostics})
	}
}

func driftSlope(drift ParsedDrift) float64 {
	if drift.Slope.Status == "ok" {
		return drift.Slope.Value
	}
	return 0
}
func driftIntercept(drift ParsedDrift) float64 {
	if drift.Intercept.Status == "ok" {
		return drift.Intercept.Value
	}
	return 0
}
