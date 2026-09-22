package engine

import (
	"fmt"

	"tga/internal/model"
)

// atmosphereArtifact turns a valve event into an uncertain reaction window.
//
// Valve switch time is NOT the sample reaction time: gas transport delay and
// thermal lag mean the sample sees the new atmosphere over a window. When the
// window cannot be narrowed to one event, every legal association is kept.
func (e *Engine) atmosphereArtifacts(params *model.Params) []*model.Artifact {
	// NOTE: gas submissions are read via state in derive; this helper is kept
	// thin and the real builder lives in atmosphereFor.
	return nil
}

// gasState is injected by derive (kept here for clarity).
type gasState struct {
	sub    *model.Submission
	events []model.GasEvent
}

func (e *Engine) atmosphereFor(st *State, params *model.Params) []*model.Artifact {
	gasSub := st.ActiveSubmission(model.LayerGas)
	if gasSub == nil {
		return nil
	}
	delay := num(params.Values["gas_delay_s"], 60)
	half := num(params.Values["reaction_window_halfwidth_s"], 30)
	var out []*model.Artifact
	for _, g := range gasSub.Gas {
		if !g.TimeOK || g.FromGas == g.ToGas {
			continue
		}
		reactionT0 := g.TimeSec + delay - half
		reactionT1 := g.TimeSec + delay + half
		rng := [2]float64{reactionT0, reactionT1}
		a := &model.Artifact{
			Kind: "atmosphere_window", Title: fmt.Sprintf("气氛切换 %s → %s", g.FromGas, g.ToGas),
			Algorithm: AlgorithmVersion, ParamFP: params.Fingerprint,
			Sources:    []model.SourceRef{e.srcRef(gasSub, "gas_valve_events")},
			CreatedSeq: gasSub.EventSeq, CreatedAt: gasSub.CreatedAt,
		}
		a.ID = e.hashID("atm_window", fmt.Sprint(gasSub.EventSeq), fmt.Sprintf("%.0f", g.TimeSec), g.FromGas, g.ToGas, params.Fingerprint)
		a.Quantities = []model.Quantity{
			{Name: "valve_t", Label: "阀切换时间", Value: g.TimeSec, Unit: "s", State: "ok", Display: fmt.Sprintf("%.0f s（仅为阀门动作）", g.TimeSec)},
			{Name: "reaction_t", Label: "样品反应时间窗", Value: g.TimeSec + delay, Unit: "s", State: "unknown",
				Display: fmt.Sprintf("%.0f ~ %.0f s", reactionT0, reactionT1), Range: &rng},
			{Name: "delay", Label: "输运延迟", Value: delay, Unit: "s", State: "ok", Display: fmt.Sprintf("%.0f s", delay)},
		}
		a.Steps = []model.Step{
			{Label: "关系", Formula: "reaction ∈ [valve + delay ± halfwidth]", Result: "阀切换时间不等于样品反应时间；不确定时保留窗口"},
		}
		a.Data = map[string]any{
			"valve_time_sec": g.TimeSec, "delay_s": delay, "halfwidth_s": half,
			"reaction_t0_sec": reactionT0, "reaction_t1_sec": reactionT1,
			"from": g.FromGas, "to": g.ToGas,
		}
		out = append(out, a)
	}
	return out
}

// associateStagesWithAtmosphere labels stage candidates whose peak falls in an
// atmosphere reaction window; ambiguous stages keep multiple associations.
func associateStagesWithAtmosphere(stages, atms []*model.Artifact) {
	for _, stg := range stages {
		peakT := stg.Data["peak_time_sec"].(float64)
		var hits []string
		for _, a := range atms {
			t0 := a.Data["reaction_t0_sec"].(float64)
			t1 := a.Data["reaction_t1_sec"].(float64)
			if peakT >= t0 && peakT <= t1 {
				hits = append(hits, fmt.Sprintf("%s→%s", a.Data["from"], a.Data["to"]))
			}
		}
		if len(hits) == 0 {
			stg.Data["atmosphere"] = "unlinked"
			continue
		}
		stg.Data["atmosphere"] = joinStrings(hits, ",")
		if len(hits) > 1 {
			stg.Data["atmosphere_ambiguous"] = true
		}
	}
}

func joinStrings(xs []string, sep string) string {
	out := ""
	for i, x := range xs {
		if i > 0 {
			out += sep
		}
		out += x
	}
	return out
}
