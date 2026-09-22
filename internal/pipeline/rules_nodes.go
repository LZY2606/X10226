package pipeline

import (
	"encoding/json"
	"fmt"

	"tgaaudit/internal/domain"
)

func (d *Derivation) buildRules(switches []gasSwitch) {
	add := func(code, subj, sev, msg string, refs ...string) {
		d.RuleEvents = append(d.RuleEvents, domain.RuleEvent{
			Code: code, Subject: subj, Severity: sev, Message: msg, Refs: refs})
	}

	// Rule 1: valve switching time is not reaction time.
	if len(switches) > 0 && d.Params.AtmosphereDelayS > 0 {
		for _, r := range d.Reactions {
			match := ""
			if !r.PeakMatches {
				match = " observed peak lies outside the delay/lag window"
			}
			add("atmosphere_delay", r.StageKey, "rule",
				fmt.Sprintf("valve switch at %.3fs is not the reaction time: transport delay %.0fs plus thermal lag tau %.0fs give reaction window %.3f-%.3fs (peak %.3fs).%s",
					r.ValveT, r.DelayS, r.LagTauS, r.ReactionTLo, r.ReactionTHi,
					r.ObservedPeakT, match),
				d.SourceBatchIDs[domain.KindGas], "params")
		}
	}

	// Rule 2: stages may overlap; losses are not forced to sum to 100%.
	for hyp, stages := range d.Stages {
		var sum float64
		overlapped := false
		for _, st := range stages {
			if st.MassLossFrac != nil {
				sum += *st.MassLossFrac
			}
			if len(st.OverlapsWith) > 0 {
				overlapped = true
			}
		}
		if overlapped {
			add("overlapping_stages", "stages-"+hyp, "rule",
				fmt.Sprintf("hypothesis %s contains overlapping stages; stage mass losses are reported independently and are not normalised to sum to 100%% (uncorrected sum %.2f%%)",
					hyp, sum*100))
		} else {
			add("stage_sum_free", "stages-"+hyp, "info",
				fmt.Sprintf("hypothesis %s stage losses are independent; no 100%% closure constraint is applied (sum %.2f%%)",
					hyp, sum*100))
		}
	}

	// Rule 3: gaps are never interpolated.
	for _, g := range d.Gaps {
		add("no_interpolation_across_gap", fmt.Sprintf("gap-%d", g.Index), "rule",
			fmt.Sprintf("data gap %.3f-%.3fs (%s): no DTG, stage boundary or residue value is interpolated across it; affected candidates remain unknown",
				g.FromT, g.ToT, g.Reason))
	}

	// Rule 4: non-monotonic noise flagged, points retained.
	for _, pt := range d.Corrected.Points {
		if pt.NonMonotonic {
			add("non_monotonic_noise", fmt.Sprintf("point-%d", pt.I), "warn",
				fmt.Sprintf("sample at %.3fs shows corrected mass increase; retained as noise evidence and not deleted",
					pt.TS))
		}
	}

	// Rule 5: multiple legal candidates kept when not uniquely decidable.
	if len(d.PeakSets) >= 2 && len(d.PeakSets[0].Peaks) != len(d.PeakSets[1].Peaks) {
		add("ambiguous_decomposition", "peak_hypotheses", "warn",
			"overlapping peaks admit more than one legal decomposition; both permissive and conservative hypotheses are retained for human review")
	}

	// Rule 6: initial mass provenance.
	for _, dg := range d.Corrected.Diagnostics {
		add("initial_mass_inferred", "initial_mass", "warn", dg)
	}
}

func marshalNode(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func (d *Derivation) assembleNodes() {
	if d.Corrected == nil {
		return
	}
	fp := ParamFingerprint(d.Params)
	batchSources := func(extra ...domain.SourceRef) []domain.SourceRef {
		out := []domain.SourceRef{{Kind: "params", ID: fp}}
		for kind, id := range d.SourceBatchIDs {
			out = append(out, domain.SourceRef{Kind: "batch", ID: id, Role: kind})
		}
		out = append(out, extra...)
		return out
	}

	corrID := nodeID(domain.NodeCorrected, d.SetID)
	d.corrNodeID = corrID
	d.Nodes = append(d.Nodes, domain.Node{
		ID: corrID, Kind: domain.NodeCorrected,
		AlgorithmVersion: d.Params.AlgorithmVersion, ParamFingerprint: fp,
		Sources: batchSources(), Data: marshalNode(d.Corrected)})

	dtgPayload := DtgData{Key: "dtg", Unit: "1/s",
		WindowS: d.Params.DtgWindowS, SmoothS: d.Params.SmoothWindowS,
		Points: d.dtgPoints, Gaps: d.Gaps, SourceNodeID: corrID}
	d.Dtg = &dtgPayload
	dtgID := nodeID(domain.NodeDtg, d.SetID)
	d.dtgNodeID = dtgID
	d.Nodes = append(d.Nodes, domain.Node{
		ID: dtgID, Kind: domain.NodeDtg,
		AlgorithmVersion: d.Params.AlgorithmVersion, ParamFingerprint: fp,
		Sources: batchSources(domain.SourceRef{Kind: "node", ID: corrID,
			Role: "corrected_series"}), Data: marshalNode(dtgPayload)})

	for i := range d.Gaps {
		g := d.Gaps[i]
		id := nodeID(domain.NodeGap, d.SetID, itoa(i))
		d.Nodes = append(d.Nodes, domain.Node{
			ID: id, Kind: domain.NodeGap,
			AlgorithmVersion: d.Params.AlgorithmVersion, ParamFingerprint: fp,
			Sources: batchSources(domain.SourceRef{Kind: "node", ID: dtgID}),
			Data:    marshalNode(g)})
	}

	// quality flags (non-monotonic noise points)
	for _, pt := range d.Corrected.Points {
		if !pt.NonMonotonic {
			continue
		}
		id := nodeID(domain.NodeFlag, d.SetID, "nm", itoa(pt.I))
		d.Nodes = append(d.Nodes, domain.Node{
			ID: id, Kind: domain.NodeFlag,
			AlgorithmVersion: d.Params.AlgorithmVersion, ParamFingerprint: fp,
			Sources: batchSources(domain.SourceRef{Kind: "node", ID: corrID}),
			Data: marshalNode(map[string]any{
				"point_index": pt.I, "t": pt.TS,
				"code":    "non_monotonic_noise",
				"message": "corrected mass increases relative to previous point",
			})})
	}

	for _, set := range d.PeakSets {
		for _, pk := range set.Peaks {
			id := nodeID(domain.NodePeak, d.SetID, set.Hypothesis, itoa(pk.Index))
			pl := map[string]any{"hypothesis": set.Hypothesis, "label": set.Label,
				"peak": pk}
			d.Nodes = append(d.Nodes, domain.Node{
				ID: id, Kind: domain.NodePeak,
				AlgorithmVersion: d.Params.AlgorithmVersion, ParamFingerprint: fp,
				Sources: batchSources(domain.SourceRef{Kind: "node", ID: dtgID}),
				Data:    marshalNode(pl)})
		}
	}

	for hyp, stages := range d.Stages {
		for _, st := range stages {
			id := nodeID(domain.NodeStage, d.SetID, hyp, itoa(st.Index))
			peakNode := nodeID(domain.NodePeak, d.SetID, hyp, itoa(st.PeakIndex))
			d.Nodes = append(d.Nodes, domain.Node{
				ID: id, Kind: domain.NodeStage,
				AlgorithmVersion: d.Params.AlgorithmVersion, ParamFingerprint: fp,
				Sources: batchSources(
					domain.SourceRef{Kind: "node", ID: corrID},
					domain.SourceRef{Kind: "node", ID: peakNode, Role: "peak"}),
				Data: marshalNode(st)})
		}
	}

	for _, r := range d.Reactions {
		id := nodeID(domain.NodeReaction, d.SetID, r.StageHyp, itoa(r.StageIndex),
			fmt.Sprintf("%.3f", r.ValveT))
		d.Nodes = append(d.Nodes, domain.Node{
			ID: id, Kind: domain.NodeReaction,
			AlgorithmVersion: d.Params.AlgorithmVersion, ParamFingerprint: fp,
			Sources: batchSources(
				domain.SourceRef{Kind: "node",
					ID: nodeID(domain.NodeStage, d.SetID, r.StageHyp,
						itoa(r.StageIndex))}),
			Data: marshalNode(r)})
	}

	for hyp, rd := range d.Residues {
		// attach chain node ids after node ids known
		for ci := range rd.Chain {
			switch rd.Chain[ci].Path {
			case "corrected.initial_mass_kg":
				rd.Chain[ci].NodeID = corrID
			case "residue.value", "residue.percent", "residue.mean_corrected_mass_kg",
				"residue.tail_from_t", "residue.tail_to_t":
			}
		}
		id := nodeID(domain.NodeResidue, d.SetID, hyp)
		rd.Chain = withResidueNode(rd.Chain, id)
		d.Residues[hyp] = rd
		d.Nodes = append(d.Nodes, domain.Node{
			ID: id, Kind: domain.NodeResidue,
			AlgorithmVersion: d.Params.AlgorithmVersion, ParamFingerprint: fp,
			Sources: batchSources(
				domain.SourceRef{Kind: "node", ID: corrID},
				domain.SourceRef{Kind: "node", ID: dtgID}),
			Data: marshalNode(rd)})
	}
}

func withResidueNode(chain []ChainStep, id string) []ChainStep {
	for i := range chain {
		if chain[i].Path == "residue.value" || chain[i].Path == "residue.percent" ||
			chain[i].Path == "residue.tail_from_t" ||
			chain[i].Path == "residue.tail_to_t" ||
			chain[i].Path == "residue.mean_corrected_mass_kg" {
			chain[i].NodeID = id
		}
	}
	return chain
}
