package core

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"tgaview/internal/store"
	"tgaview/internal/tga"
)

// PipelineParams are the adjustable computation parameters. A parameter
// revision changes the fingerprint and produces new derived identities;
// old results stay viewable but stale.
type PipelineParams struct {
	GapThresholdMin float64 `json:"gap_threshold_min"`
	NoiseBandMg     float64 `json:"noise_band_mg"`
	DTGWindow       int     `json:"dtg_window"`
	AtmosDelayMin   float64 `json:"atmos_delay_min"`
	AtmosTauMin     float64 `json:"atmos_tau_min"`
	MinRateMgMin    float64 `json:"min_rate_mg_min"`
	OnsetRatio      float64 `json:"onset_ratio"`
	OverlapRatio    float64 `json:"overlap_ratio"`
	InitialGas      string  `json:"initial_gas"`
}

// DefaultParams returns the case baseline parameters.
func DefaultParams() PipelineParams {
	return PipelineParams{
		GapThresholdMin: 1.5,
		NoiseBandMg:     0.004,
		DTGWindow:       3,
		AtmosDelayMin:   1.2,
		AtmosTauMin:     2.5,
		MinRateMgMin:    0.02,
		OnsetRatio:      0.08,
		OverlapRatio:    0.35,
		InitialGas:      "N2",
	}
}

// derivedID builds a stable, content-addressed identity for a derived value.
func derivedID(slot string, inputs []string, params any) string {
	fp := tga.Fingerprint(map[string]any{
		"algo":   tga.AlgoVersion,
		"inputs": inputs,
		"params": params,
	})
	return fmt.Sprintf("der/%s/%s", slot, fp)
}

// emitDerived appends a derived.computed event unless an identical derived
// value already exists (idempotent recompute). Returns the derived ID.
func emitDerived(st *store.Store, state *State, slot string, params map[string]any, inputs []string, body any) (string, bool, error) {
	id := derivedID(slot, inputs, params)
	if _, ok := state.Derived[id]; ok {
		return id, false, nil
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", false, err
	}
	p := DerivedPayload{
		ID:               id,
		Kind:             slot,
		AlgoVersion:      tga.AlgoVersion,
		ParamFingerprint: tga.Fingerprint(params),
		Params:           params,
		Inputs:           inputs,
		Body:             raw,
	}
	if _, err := st.Append(EvDerivedComputed, id, p); err != nil {
		return "", false, err
	}
	return id, true, nil
}

func emitJudgment(st *store.Store, derivedID, rule string, data any) error {
	raw, err := json.Marshal(data)
	if err != nil {
		return err
	}
	_, err = st.Append(EvJudgment, derivedID, JudgmentPayload{Rule: rule, DerivedID: derivedID, Data: raw})
	return err
}

// latestSrc returns the newest version of a source, or nil.
func latestSrc(state *State, kind string) *SourceVersion {
	for _, id := range state.SourceOrder {
		s := state.Sources[id]
		if s.Kind == kind {
			return s.Latest()
		}
	}
	return nil
}

// ParseDriftRate reads "drift_rate,<value>,<unit>" from the drift source.
func ParseDriftRate(raw string) (float64, string, error) {
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "param") {
			continue
		}
		f := strings.Split(ln, ",")
		if len(f) != 3 || strings.TrimSpace(f[0]) != "drift_rate" {
			continue
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(f[1]), 64)
		if err != nil {
			return 0, "", fmt.Errorf("drift_rate value %q not numeric", f[1])
		}
		unit := strings.TrimSpace(f[2])
		scale := 0.0
		switch unit {
		case "mg/min":
			scale = 1
		case "ug/min":
			scale = 0.001
		case "mg/s":
			scale = 60
		default:
			return 0, "", fmt.Errorf("drift_rate unit %q has no conversion; value left unconverted", unit)
		}
		return v * scale, fmt.Sprintf("%s,%s", strings.TrimSpace(f[1]), unit), nil
	}
	return 0, "", fmt.Errorf("no drift_rate row found")
}

// ParseInitialMass reads "value,unit" from the initial_mass source.
func ParseInitialMass(raw string) (float64, string, error) {
	for _, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "value") {
			continue
		}
		f := strings.Split(ln, ",")
		if len(f) != 2 {
			return 0, "", fmt.Errorf("initial_mass row %q: want value,unit", ln)
		}
		v, err := strconv.ParseFloat(strings.TrimSpace(f[0]), 64)
		if err != nil {
			return 0, "", fmt.Errorf("initial mass %q not numeric", f[0])
		}
		unit := strings.TrimSpace(f[1])
		cv, ok := tga.Convert("mass", unit, v)
		if !ok {
			return 0, "", fmt.Errorf("initial mass unit %q not convertible; value left unconverted", unit)
		}
		return cv, ln, nil
	}
	return 0, "", fmt.Errorf("no initial mass row found")
}

// ParseValveEvents parses "time_s,valve,gas,flow_mL_min" rows.
func ParseValveEvents(raw string) ([]tga.ValveEvent, error) {
	var out []tga.ValveEvent
	for i, ln := range strings.Split(raw, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "time") {
			continue
		}
		f := strings.Split(ln, ",")
		if len(f) != 4 {
			return nil, fmt.Errorf("gas_valve_events:%d: want 4 columns, got %d", i+1, len(f))
		}
		t, err := strconv.ParseFloat(strings.TrimSpace(f[0]), 64)
		if err != nil {
			return nil, fmt.Errorf("gas_valve_events:%d: time %q not numeric", i+1, f[0])
		}
		flow, err := strconv.ParseFloat(strings.TrimSpace(f[3]), 64)
		if err != nil {
			return nil, fmt.Errorf("gas_valve_events:%d: flow %q not numeric", i+1, f[3])
		}
		out = append(out, tga.ValveEvent{
			TimeMin:   t / 60,
			Valve:     strings.TrimSpace(f[1]),
			Gas:       strings.TrimSpace(f[2]),
			FlowMlMin: flow,
		})
	}
	return out, nil
}

// RunPipeline computes the full derived chain from the latest source
// versions and appends derived + judgment events. Missing sources simply
// skip their dependent steps (supports partial imports).
func RunPipeline(st *store.Store, p PipelineParams) error {
	events, err := st.Events(0)
	if err != nil {
		return err
	}
	state := Build(events)

	paramsMap := func() map[string]any {
		b, _ := json.Marshal(p)
		var m map[string]any
		json.Unmarshal(b, &m)
		return m
	}

	// 1. normalized sample series
	srcSample := latestSrc(state, SrcSamplePoints)
	if srcSample == nil {
		return fmt.Errorf("source %s is required", SrcSamplePoints)
	}
	sampleCols, _, sampleDiags, err := tga.ParseCSV(srcSample.Raw, SrcSamplePoints, map[string]string{
		"time": "time", "temp": "temp", "mass": "mass",
	})
	if err != nil {
		return err
	}
	series := &tga.Series{
		Time: sampleCols["time"], Temp: sampleCols["temp"], Mass: sampleCols["mass"], Diags: sampleDiags,
	}
	inSample := []string{SrcRef(srcSample.SourceID, srcSample.Version)}
	normID, _, err := emitDerived(st, state, "series.sample", paramsMap(), inSample, series)
	if err != nil {
		return err
	}

	// 2. normalized blank (optional)
	blankID := ""
	var blankCols map[string][]float64
	if srcBlank := latestSrc(state, SrcBlankCurve); srcBlank != nil {
		cols, _, diags, err := tga.ParseCSV(srcBlank.Raw, SrcBlankCurve, map[string]string{
			"temp": "temp", "mass": "mass",
		})
		if err != nil {
			return err
		}
		blankCols = cols
		inBlank := []string{SrcRef(srcBlank.SourceID, srcBlank.Version)}
		blankID, _, err = emitDerived(st, state, "series.blank", paramsMap(), inBlank,
			&tga.Series{Temp: cols["temp"], Mass: cols["mass"], Diags: diags})
		if err != nil {
			return err
		}
	}

	// 3. corrected TG: drift + blank, per segment (gaps never interpolated)
	srcDrift := latestSrc(state, SrcDriftCorrection)
	if srcDrift == nil {
		return fmt.Errorf("source %s is required", SrcDriftCorrection)
	}
	rate, rateRaw, err := ParseDriftRate(srcDrift.Raw)
	if err != nil {
		return err
	}
	segs, gapJudgments := tga.SplitSegments(series, p.GapThresholdMin)
	corrected := tga.ApplyDrift(series, rate)
	corrInputs := []string{DerRef(normID), SrcRef(srcDrift.SourceID, srcDrift.Version)}
	var blankDiags []tga.Diagnostic
	if blankCols != nil {
		corrected, blankDiags = tga.SubtractBlank(corrected, blankCols["temp"], blankCols["mass"], segs)
		corrInputs = append(corrInputs, DerRef(blankID))
	}
	corrected.Diags = append(corrected.Diags, blankDiags...)
	corrBody := map[string]any{
		"series": corrected, "segments": segs, "drift_rate_mg_min": rate, "drift_raw": rateRaw,
	}
	corrID, corrNew, err := emitDerived(st, state, "tg.corrected", paramsMap(), corrInputs, corrBody)
	if err != nil {
		return err
	}
	if corrNew {
		for _, g := range gapJudgments {
			if err := emitJudgment(st, corrID, g.Rule, g); err != nil {
				return err
			}
		}
		for _, nj := range tga.DetectNonMonotonic(corrected, segs, p.NoiseBandMg) {
			if err := emitJudgment(st, corrID, nj.Rule, nj); err != nil {
				return err
			}
		}
	}

	// 4. DTG
	dtg := tga.ComputeDTG(corrected, segs, p.DTGWindow)
	dtgID, _, err := emitDerived(st, state, "dtg", paramsMap(), []string{DerRef(corrID)}, dtg)
	if err != nil {
		return err
	}

	// 5. atmosphere timeline (valve time != reaction time)
	atmosID := ""
	if srcGas := latestSrc(state, SrcGasValveEvents); srcGas != nil {
		vevents, err := ParseValveEvents(srcGas.Raw)
		if err != nil {
			return err
		}
		atmos, atmosJudgments := tga.BuildAtmosphere(vevents, p.InitialGas, tga.AtmosParams{
			DelayMin: p.AtmosDelayMin, TauMin: p.AtmosTauMin,
		})
		var atmosNew bool
		atmosID, atmosNew, err = emitDerived(st, state, "atmosphere", paramsMap(),
			[]string{SrcRef(srcGas.SourceID, srcGas.Version)}, atmos)
		if err != nil {
			return err
		}
		if atmosNew {
			for _, j := range atmosJudgments {
				if err := emitJudgment(st, atmosID, j.Rule, j); err != nil {
					return err
				}
			}
		}
	}

	// 6. stage candidates (overlap allowed, sum not forced, ambiguity kept)
	srcMass := latestSrc(state, SrcInitialMass)
	if srcMass == nil {
		return fmt.Errorf("source %s is required", SrcInitialMass)
	}
	initialMg, initialRaw, err := ParseInitialMass(srcMass.Raw)
	if err != nil {
		return err
	}
	stageInputs := []string{DerRef(dtgID), SrcRef(srcMass.SourceID, srcMass.Version)}
	if atmosID != "" {
		stageInputs = append(stageInputs, DerRef(atmosID))
	}
	cands, overlaps, amb := tga.FindCandidates(corrected, dtg, tga.CandidateParams{
		MinRateMgMin:    p.MinRateMgMin,
		OnsetRatio:      p.OnsetRatio,
		OverlapRatio:    p.OverlapRatio,
		NoiseBandMg:     p.NoiseBandMg,
		GapThresholdMin: p.GapThresholdMin,
		InitialMassMg:   initialMg,
	})
	stageID, stageNew, err := emitDerived(st, state, "stages", paramsMap(), stageInputs, cands)
	if err != nil {
		return err
	}
	if stageNew {
		for _, o := range overlaps {
			if err := emitJudgment(st, stageID, o.Rule, o); err != nil {
				return err
			}
		}
		if amb != nil {
			if err := emitJudgment(st, stageID, amb.Rule, amb); err != nil {
				return err
			}
		}
	}

	// 7. residue with expandable calculation chain
	res := tga.ComputeResidue(corrected, initialMg, initialRaw,
		SrcRef(srcMass.SourceID, srcMass.Version), corrID)
	_, _, err = emitDerived(st, state, "residue", paramsMap(),
		[]string{DerRef(corrID), SrcRef(srcMass.SourceID, srcMass.Version)}, res)
	return err
}

// RebuildState is a convenience: load events and project.
func RebuildState(st *store.Store, upTo int64) (*State, error) {
	events, err := st.Events(upTo)
	if err != nil {
		return nil, err
	}
	return Build(events), nil
}

// CurrentParams derives the effective pipeline parameters from the event
// history: defaults plus every active (non-undone) adjust decision, in
// sequence order. This makes parameter state fully event-sourced, so
// restarts and time-travel see exactly the parameters of that moment.
func CurrentParams(state *State) PipelineParams {
	p := DefaultParams()
	type adj struct {
		seq    int64
		params map[string]any
	}
	var adjs []adj
	for _, ds := range state.Decisions {
		for _, d := range ds {
			if d.Action == "adjust" && !d.Undone && len(d.Params) > 0 {
				adjs = append(adjs, adj{seq: d.Seq, params: d.Params})
			}
		}
	}
	for i := 0; i < len(adjs); i++ {
		for j := i + 1; j < len(adjs); j++ {
			if adjs[j].seq < adjs[i].seq {
				adjs[i], adjs[j] = adjs[j], adjs[i]
			}
		}
	}
	apply := func(m map[string]any) {
		get := func(k string) (float64, bool) {
			v, ok := m[k]
			if !ok {
				return 0, false
			}
			f, ok := v.(float64)
			return f, ok
		}
		if v, ok := get("atmos_delay_min"); ok {
			p.AtmosDelayMin = v
		}
		if v, ok := get("atmos_tau_min"); ok {
			p.AtmosTauMin = v
		}
		if v, ok := get("min_rate_mg_min"); ok {
			p.MinRateMgMin = v
		}
		if v, ok := get("onset_ratio"); ok {
			p.OnsetRatio = v
		}
		if v, ok := get("overlap_ratio"); ok {
			p.OverlapRatio = v
		}
		if v, ok := get("noise_band_mg"); ok {
			p.NoiseBandMg = v
		}
		if v, ok := get("gap_threshold_min"); ok {
			p.GapThresholdMin = v
		}
		if v, ok := get("dtg_window"); ok {
			p.DTGWindow = int(v)
		}
	}
	for _, a := range adjs {
		apply(a.params)
	}
	return p
}

// RunPipelineCurrent computes the pipeline with the parameters derived
// from the current event history.
func RunPipelineCurrent(st *store.Store) error {
	state, err := RebuildState(st, 0)
	if err != nil {
		return err
	}
	return RunPipeline(st, CurrentParams(state))
}
