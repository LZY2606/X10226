package domain

import (
	"fmt"
	"math"
	"sort"
	"strconv"
)

func f2s(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }
func itoa(i int) string    { return strconv.Itoa(i) }
func shortFloat(v float64) string {
	return strconv.FormatFloat(math.Round(v*1e6)/1e6, 'g', -1, 64)
}

// residue computes final residue with an expandable chain back to raw inputs.
func (e *engine) residue(pts []pt, out *ComputeResult) {
	if len(pts) == 0 {
		return
	}
	im, hasInit := e.initMass()
	last := pts[len(pts)-1]
	refs := append([]Ref{}, e.refs[NodeCleanedSeries]...)
	if r, ok := e.sourceRef(KindInitialMass); ok {
		refs = append(refs, r)
	}
	if r, ok := e.sourceRef(KindDriftCorrection); ok {
		refs = append(refs, r)
	}
	if r, ok := e.sourceRef(KindBlankCurve); ok {
		refs = append(refs, r)
	}

	chain := []ResidueChainStep{
		{Role: "final_corrected_mass", ValueMg: last.raw, Status: "ok",
			Note: fmt.Sprintf("last corrected point index=%d t_s=%s", last.idx, f2s(last.t)),
			Ref:  Ref{Kind: "node", ID: e.nodeID(out, NodeCleanedSeries, "series"), Role: NodeCleanedSeries}},
	}
	payload := FinalResiduePayload{FinalMassMg: last.raw, AtTimeS: last.t, InitialMg: im.MassMg}
	if !hasInit {
		payload.ResiduePct = StageLoss{Status: "unknown", Reason: "initial_mass_stratum_missing"}
		chain = append(chain, ResidueChainStep{Role: "initial_mass", Status: "unknown",
			Note: "initial_mass stratum not committed"})
	} else {
		payload.ResiduePct = StageLoss{Status: "ok",
			ValueMg: im.MassMg, ValuePct: last.raw / im.MassMg * 100}
		chain = append(chain, []ResidueChainStep{
			{Role: "initial_mass", ValueMg: im.MassMg, Status: "ok",
				Ref: Ref{Kind: "source", ID: e.src.InitMass.ID, Role: KindInitialMass}},
			{Role: "residue_ratio", ValuePct: last.raw / im.MassMg * 100, Status: "ok",
				Note: "final_corrected_mass / initial_mass * 100"},
		}...)
	}
	params := map[string]interface{}{"at_time_s": last.t, "has_initial_mass": hasInit}
	e.emit(out, NodeResidue, "final", params, payload, refs...)
}

func (e *engine) nodeID(out *ComputeResult, kind, group string) string {
	if n, ok := e.findNode(out, kind, group); ok {
		return n.ID
	}
	return ""
}

// standingJudgments emits the three machine-readable judgments that must survive
// replay and be inspectable in the counterexample archive.
func (e *engine) standingJudgments(pts []pt, out *ComputeResult) {
	cleanNode, _ := e.findNode(out, NodeCleanedSeries, "series")
	var cp CleanedSeriesPayload
	_ = jsonUnmarshal(cleanNode.Payload, &cp)
	gbasis := []Ref{{Kind: "node", ID: cleanNode.ID, Role: NodeCleanedSeries}}

	// 1) valve time != reaction time; stages may overlap; no forced 100% sum
	gv := e.gas()
	if len(gv.Events) > 0 {
		var evs []map[string]interface{}
		for _, ev := range gv.Events {
			evs = append(evs, map[string]interface{}{
				"valve_id": ev.ID, "valve_time_s": ev.TimeS,
				"reaction_time_s": ev.TimeS + ev.DelayS, "delay_s": ev.DelayS,
			})
		}
		out.Judgments = append(out.Judgments, Judgment{Type: "atmosphere_delay_and_lag", Props: map[string]interface{}{
			"rule":               "valve_switch_time_is_not_sample_reaction_time",
			"events":             evs,
			"stages_may_overlap": true,
			"forced_sum_100":     false,
			"residue_basis":      "final_corrected_mass/initial_mass",
		}, Basis: gbasis})
	}

	// 2) gaps are never bridged by interpolation
	if len(cp.Gaps) > 0 {
		gs := make([]map[string]interface{}, 0, len(cp.Gaps))
		for _, g := range cp.Gaps {
			gs = append(gs, map[string]interface{}{
				"after_index": g.AfterIndex, "before_t_s": g.BeforeT,
				"after_t_s": g.AfterT, "width_s": g.WidthS,
				"interpolation_allowed": false,
			})
		}
		out.Judgments = append(out.Judgments, Judgment{Type: "sampling_gaps_no_interpolation", Props: map[string]interface{}{
			"rule": "derived_quantities_never_span_sampling_gaps",
			"gaps": gs,
		}, Basis: gbasis})
	}

	// 3) non-unique decomposition: all legal candidates retained
	var legal []string
	var rejected []string
	for _, n := range out.Nodes {
		if n.Kind != NodeStage && n.Kind != NodeAtmoStage {
			continue
		}
		var sc StageCandidatePayload
		_ = jsonUnmarshal(n.Payload, &sc)
		entry := map[string]interface{}{"key": sc.Key, "origin": sc.Origin,
			"start_s": sc.StartS, "end_s": sc.EndS, "loss_status": sc.Loss.Status}
		_ = entry
		if len(sc.OverlapsWith) > 0 || len(sc.SubPeaks) > 1 {
			legal = append(legal, sc.Key)
		}
		if sc.Loss.Status == "unknown" {
			rejected = append(rejected, sc.Key)
		}
	}
	sort.Strings(legal)
	if len(legal) > 0 {
		out.Judgments = append(out.Judgments, Judgment{Type: "nonunique_candidates_retained", Props: map[string]interface{}{
			"rule":                        "all_legal_candidates_retained_when_not_uniquely_decidable",
			"candidate_keys":              legal,
			"unknown_loss_candidate_keys": rejected,
		}, Basis: gbasis})
	}
}
