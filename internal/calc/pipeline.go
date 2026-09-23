package calc

import (
	"time"

	"tga-review/internal/domain"
)

// PipelineResult bundles the freshly computed nodes and machine-readable
// domain rule judgments emitted by this run.
type PipelineResult struct {
	Nodes       []domain.Node
	Rules       []domain.RuleEvent
}

// Run executes all six derived steps deterministically. Each node is content
// addressed from algo version + parameter fingerprint + direct inputs, so the
// same project state always yields identical ids across machines/archives.
func Run(in Inputs, projectID string, now time.Time) PipelineResult {
	var res PipelineResult
	ts := now.UTC().Format(time.RFC3339Nano)
	paramFP := in.Params.Fingerprint()

	corr := ComputeCorrected(in)
	times := make([]float64, len(corr.Points))
	for i, cp := range corr.Points {
		times[i] = cp.TS
	}
	trace := ComputeFurnace(in, times)
	gas := ComputeGas(in, trace.TotalS)
	dtg := ComputeDTG(in, corr, trace)
	stages := ComputeStages(in, corr, trace, gas, dtg)

	srcID := func(s *domain.Source) string {
		if s == nil {
			return ""
		}
		return s.ID
	}
	ids := func(ss ...string) []string {
		out := []string{}
		for _, s := range ss {
			if s != "" {
				out = append(out, s)
			}
		}
		return out
	}

	mkNode := func(kind domain.NodeKind, algo string, inputs []string, payload map[string]any, diags []domain.Diag, status string) domain.Node {
		id := domain.ContentID(string(kind), map[string]any{
			"project":  projectID,
			"algo":     algo,
			"params":   paramFP,
			"inputs":   inputs,
			"payload":  payload,
		})
		return domain.Node{
			ID: id, ProjectID: projectID, Kind: kind, Algo: algo, AlgoVer: algo,
			ParamFP: paramFP, Inputs: inputs, Payload: payload, Diag: diags,
			Status: status, Current: true, CreatedAt: ts,
		}
	}

	corrStatus := "ok"
	var corrDiag []domain.Diag
	if len(corr.Gaps) > 0 {
		corrStatus = "partial"
		corrDiag = append(corrDiag, domain.Diag{Code: "W_GAP", Severity: "warning", Message: "acquisition gaps detected; no interpolation across gaps"})
	}
	if len(corr.Nonmonotonic) > 0 {
		corrStatus = "partial"
		corrDiag = append(corrDiag, domain.Diag{Code: "W_SPIKE", Severity: "warning", Message: "non-monotonic spikes quarantined, not smoothed away"})
	}
	nCorr := mkNode(domain.NodeCorrectedSeries, AlgoCorrected,
		ids(srcID(in.Samples), srcID(in.Blank), srcID(in.Drift)),
		map[string]any{"corrected": corr}, corrDiag, corrStatus)

	nFurn := mkNode(domain.NodeFurnaceTrace, AlgoFurnace,
		ids(srcID(in.Furnace), srcID(in.Valves), paramFP),
		map[string]any{"trace": trace}, nil, "ok")

	nGas := mkNode(domain.NodeGasTimeline, AlgoGas,
		ids(srcID(in.Valves), paramFP),
		map[string]any{"timeline": gas}, nil, "ok")

	dtgStatus := "ok"
	if corrStatus == "partial" {
		dtgStatus = "partial"
	}
	nDTG := mkNode(domain.NodeDTG, AlgoDTG,
		ids(nCorr.ID, nFurn.ID, srcID(in.InitMass)),
		map[string]any{"dtg": dtg}, nil, dtgStatus)

	nStages := mkNode(domain.NodeStages, AlgoStages,
		ids(nDTG.ID, nGas.ID, nFurn.ID, nCorr.ID, paramFP),
		map[string]any{"stages": stages}, nil, "ok")

	res.Nodes = []domain.Node{nCorr, nFurn, nGas, nDTG, nStages}

	// machine-readable rule judgments
	if len(corr.Gaps) > 0 {
		var affected []string
		for _, g := range corr.Gaps {
			affected = append(affected, gapID(g))
		}
		res.Rules = append(res.Rules, domain.RuleEvent{
			Rule: domain.RuleGap, Subject: "corrected_series", ObjectIDs: []string{nCorr.ID},
			Judgment: "accepted", Reason: "data gaps break regions; trapezoid integration never interpolates across them",
			Affected: []string{nDTG.ID, nStages.ID}, Extra: map[string]any{"gaps": corr.Gaps},
		})
	}
	if len(corr.Nonmonotonic) > 0 {
		res.Rules = append(res.Rules, domain.RuleEvent{
			Rule: domain.RuleNoise, Subject: "corrected_series", ObjectIDs: []string{nCorr.ID},
			Judgment: "accepted", Reason: "non-monotonic rises exceeding noise floor are quarantined, never smoothed away",
			Affected: []string{nDTG.ID}, Extra: map[string]any{"spike_t_s": corr.Nonmonotonic},
		})
	}
	for _, sw := range gas.Switches {
		if sw.DelayS > 0 {
			res.Rules = append(res.Rules, domain.RuleEvent{
				Rule: domain.RuleAtmoDelay, Subject: "valve:" + sw.Gas,
				ObjectIDs: []string{srcID(in.Valves), nGas.ID},
				Judgment: "accepted",
				Reason:   "valve switch time is not sample reaction time; effective arrival shifted by transport delay",
				Affected: []string{nStages.ID},
				Extra:    map[string]any{"valve_t_s": sw.ValveTS, "effective_t_s": sw.EffectiveTS, "delay_s": sw.DelayS},
			})
		}
	}
	res.Rules = append(res.Rules, domain.RuleEvent{
		Rule: domain.RuleTempLag, Subject: "furnace", ObjectIDs: []string{nFurn.ID},
		Judgment: "accepted", Reason: "sample temperature follows setpoint with first-order lag; attribution uses effective temperature",
		Affected: []string{nDTG.ID, nStages.ID},
		Extra:    map[string]any{"lag_s": in.Params.TempLagS, "peak_setpoint_c": trace.PeakSetpointC, "peak_effective_c": trace.PeakEffectiveC},
	})
	// overlap legal candidates
	for _, c := range stages.Candidates {
		if c.Proposal == "part_a" || c.Proposal == "part_b" {
			res.Rules = append(res.Rules, domain.RuleEvent{
				Rule: domain.RulePeaks, Subject: c.Key, ObjectIDs: []string{nStages.ID},
				Judgment: "unknown",
				Reason:   "overlapping peaks admit multiple legal decompositions; merged and split proposals both retained pending human decision",
				Extra: map[string]any{
					"cluster": c.Cluster, "start_s": c.StartS, "end_s": c.EndS, "atmosphere": c.Atmosphere,
				},
			})
		}
	}
	return res
}

func gapID(g Gap) string {
	return "gap_" + ftoa(g.FromS) + "_" + ftoa(g.ToS)
}

func ftoa(x float64) string {
	return formatFloat(x)
}

// ResidueStep computes the residue node separately because it depends on the
// set of live accepted-stage decisions.
func ResidueStep(in Inputs, projectID string, stageNodeID string, stages Stages, corr Corrected, accepted map[string]bool, confirmed bool, now time.Time) domain.Node {
	chain := ComputeResidue(in, corr, stages, accepted, confirmed)
	paramFP := in.Params.Fingerprint()
	status := "ok"
	if chain.M0Mg <= 0 {
		status = "unknown"
	}
	id := domain.ContentID(string(domain.NodeResidue), map[string]any{
		"project":  projectID,
		"algo":     AlgoResidue,
		"params":   paramFP,
		"inputs":   append(append([]string{stageNodeID}, nonEmpty(sourceID(in.InitMass), sourceID(in.Samples), sourceID(in.Blank), sourceID(in.Drift))...), keysOf(accepted)...),
		"payload":  map[string]any{"chain": chain},
	})
	return domain.Node{
		ID: id, ProjectID: projectID, Kind: domain.NodeResidue, Algo: AlgoResidue, AlgoVer: AlgoResidue,
		ParamFP: paramFP, Inputs: nonEmpty(stageNodeID, sourceID(in.InitMass), sourceID(in.Samples), sourceID(in.Blank), sourceID(in.Drift)),
		Payload: map[string]any{"chain": chain, "accepted_keys": keysOf(accepted)},
		Status: status, Current: true, CreatedAt: now.UTC().Format(time.RFC3339Nano),
	}
}

func sourceID(s *domain.Source) string {
	if s == nil {
		return ""
	}
	return s.ID
}

func nonEmpty(xs ...string) []string {
	out := []string{}
	for _, x := range xs {
		if x != "" {
			out = append(out, x)
		}
	}
	return out
}

func keysOf(m map[string]bool) []string {
	out := []string{}
	for k, v := range m {
		if v {
			out = append(out, k)
		}
	}
	return out
}
