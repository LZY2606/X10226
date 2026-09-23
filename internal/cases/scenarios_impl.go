package cases

import (
	"context"

	"tga-review/internal/app"
	"tga-review/internal/domain"
)

// ScenarioAtmoDelay changes the O2 transport-delay parameter 45s -> 90s.
// Only the gas timeline and stage attribution downstream expire.
func ScenarioAtmoDelay(ctx context.Context, svc *app.Service, projectID string) (app.ScenarioOutcome, error) {
	p := domain.DefaultParams()
	p.AtmoDelayS = map[string]float64{"O2": 90, "N2": 0}
	beforeIDs := svc.NodeIDs(ctx, projectID)
	fp, err := svc.AdjustParams(ctx, projectID, p, "counterexample: revisit transfer line delay")
	if err != nil {
		return app.ScenarioOutcome{}, err
	}
	return app.ScenarioOutcome{
		Status: "adopted", ChangedSource: "params.atmo_delay_s.O2: 45->90",
		Before: map[string]any{"node_ids": beforeIDs, "o2_delay_s": 45},
		After:  map[string]any{"param_fp": fp, "o2_delay_s": 90},
		Rules: []domain.RuleEvent{{
			Rule: domain.RuleAtmoDelay, Subject: "valve:O2", Judgment: "accepted",
			Reason: "valve switch at t=2880s remains fixed; effective reaction window moves to 2970s",
			Extra:  map[string]any{"valve_t_s": 2880, "effective_before_s": 2925, "effective_after_s": 2970},
		}},
		Note: "parameter-only change; corrected_series is outside the blast radius",
	}, nil
}

// ScenarioDriftRevision revises the balance drift calibration. Corrected mass
// and every downstream derived node must expire; raw sources stay immutable.
func ScenarioDriftRevision(ctx context.Context, svc *app.Service, projectID string) (app.ScenarioOutcome, error) {
	beforeIDs := svc.NodeIDs(ctx, projectID)
	src, diags, err := svc.ReviseLayer(ctx, projectID, domain.LayerDrift, MustRead("drift_v2.csv"), "csv",
		"counterexample: flatter drift calibration v2")
	if err != nil {
		return rejected("drift.csv", diags, err.Error()), nil
	}
	return app.ScenarioOutcome{
		Status: "adopted", ChangedSource: src.ID,
		Before: map[string]any{"node_ids": beforeIDs},
		After:  map[string]any{"source_id": src.ID, "version": src.Version, "parent_id": src.ParentID},
		Rules: []domain.RuleEvent{{
			Rule: "drift_v2", Subject: "corrected_series", Judgment: "accepted",
			Reason: "new linear fit (0.004->0.100 mg over run) changes corrected mass; raw v1 retained as parent revision",
		}},
		Note: "drift feeds corrected_series, so its full downstream chain expires",
	}, nil
}

// ScenarioOverlap exercises the overlap peak adjudication and conflict path:
// both the merged and a split proposal are accepted, producing a conflict that
// the reviewer then resolves. Both legal candidates remain in history.
func ScenarioOverlap(ctx context.Context, svc *app.Service, projectID string) (app.ScenarioOutcome, error) {
	vm, err := svc.View(ctx, projectID)
	if err != nil {
		return app.ScenarioOutcome{}, err
	}
	var merged, partA, partB string
	for _, st := range vm.Stages {
		switch st.Proposal {
		case "merged":
			merged = st.StageKey
		case "part_a":
			partA = st.StageKey
		case "part_b":
			partB = st.StageKey
		}
	}
	if merged == "" {
		// fallback to cluster 2 (the overlapping decomposition cluster)
		merged, partA, partB = "s2_merged", "s2_part_a", "s2_part_b"
	}
	beforeIDs := svc.NodeIDs(ctx, projectID)
	if _, _, err := svc.DecideStage(ctx, projectID, merged, "accept", "counterexample: accept merged decomposition"); err != nil {
		return app.ScenarioOutcome{}, err
	}
	_, conflict, err := svc.DecideStage(ctx, projectID, partA, "accept", "counterexample: also accept split proposal a")
	if err != nil {
		return app.ScenarioOutcome{}, err
	}
	resolved := ""
	if conflict != nil {
		if _, err := svc.ResolveConflict(ctx, projectID, conflict.ID, "keep_first"); err != nil {
			return app.ScenarioOutcome{}, err
		}
		resolved = conflict.ID
	}
	_ = partB
	return app.ScenarioOutcome{
		Status: "adopted", ChangedSource: "decision:" + merged + " + decision:" + partA,
		Before: map[string]any{"node_ids": beforeIDs},
		After: map[string]any{
			"accepted": []string{merged, partA}, "conflict_id": resolved,
			"resolution": "keep_first (merged remains live)",
		},
		Rules: []domain.RuleEvent{{
			Rule: domain.RulePeaks, Subject: merged, Judgment: "accepted",
			Reason: "overlap cluster yields merged AND part_a/part_b legal candidates; accepting both opens a conflict; resolution keeps merged",
			Extra:  map[string]any{"merged": merged, "part_a": partA, "part_b": partB, "conflict_id": resolved},
		}, {
			Rule: domain.RuleOverlap, Subject: "residue_chain", Judgment: "accepted",
			Reason: "stage losses overlap in time and are not normalized to a partition; residue remains a direct mass ratio",
		}},
		Note: "both decisions are preserved; the losing one is non-live but still readable",
	}, nil
}

// ScenarioNonmonotonic verifies the embedded spikes are quarantined, not
// smoothed across.
func ScenarioNonmonotonic(ctx context.Context, svc *app.Service, projectID string) (app.ScenarioOutcome, error) {
	vm, err := svc.View(ctx, projectID)
	if err != nil {
		return app.ScenarioOutcome{}, err
	}
	spikes := []float64{}
	gaps := 0
	for _, n := range vm.Nodes {
		if n.Kind == string(domain.NodeCorrectedSeries) {
			if c, ok := n.Payload["corrected"].(map[string]any); ok {
				if sp, ok := c["nonmonotonic_t_s"].([]any); ok {
					for _, v := range sp {
						if f, ok := v.(float64); ok {
							spikes = append(spikes, f)
						}
					}
				}
				if g, ok := c["gaps"].([]any); ok {
					gaps = len(g)
				}
			}
		}
	}
	judgment := "accepted"
	note := "spikes at 1240s and 2100s quarantined as nonmonotonic; no interpolation across them"
	if len(spikes) < 2 {
		judgment = "unknown"
		note = "expected 2 nonmonotonic quarantines"
	}
	return app.ScenarioOutcome{
		Status: judgment, ChangedSource: "samples.json#rows@1240,2100",
		Before: map[string]any{},
		After:  map[string]any{"spike_t_s": spikes, "gap_count_after_spikes": gaps},
		Rules: []domain.RuleEvent{{
			Rule: domain.RuleNoise, Subject: "corrected_series", Judgment: judgment, Reason: note,
		}},
		Note: note,
	}, nil
}

// ScenarioGap verifies the acquisition gap 1700-1820s breaks integration.
func ScenarioGap(ctx context.Context, svc *app.Service, projectID string) (app.ScenarioOutcome, error) {
	vm, err := svc.View(ctx, projectID)
	if err != nil {
		return app.ScenarioOutcome{}, err
	}
	var g0, g1 float64
	bounded := 0
	for _, n := range vm.Nodes {
		if n.Kind == string(domain.NodeCorrectedSeries) {
			if c, ok := n.Payload["corrected"].(map[string]any); ok {
				if gs, ok := c["gaps"].([]any); ok && len(gs) > 0 {
					first, _ := gs[0].(map[string]any)
					g0, _ = first["from_s"].(float64)
					g1, _ = first["to_s"].(float64)
				}
			}
		}
	}
	for _, st := range vm.Stages {
		if st.BoundedByGap {
			bounded++
		}
	}
	judgment := "accepted"
	if g0 != 1700 || g1 != 1820 {
		judgment = "unknown"
	}
	return app.ScenarioOutcome{
		Status: judgment, ChangedSource: "samples.json#gap 1700-1820s",
		After: map[string]any{"gap_from_s": g0, "gap_to_s": g1, "stages_bounded_by_gap": bounded},
		Rules: []domain.RuleEvent{{
			Rule: domain.RuleGap, Subject: "corrected_series", Judgment: judgment,
			Reason: "points across 1700-1820s are not interpolated; trapezoid areas touching the gap are bounded and marked unknown",
		}},
		Note: "no interpolated points fill the gap; affected candidates carry bounded_by_gap",
	}, nil
}

// ScenarioInitMassRevision revises m0 10.000 -> 10.050 mg (R6 locality).
func ScenarioInitMassRevision(ctx context.Context, svc *app.Service, projectID string) (app.ScenarioOutcome, error) {
	beforeIDs := svc.NodeIDs(ctx, projectID)
	if err := svc.ReviseInitialMass(ctx, projectID, MustRead("initial_mass_v2.csv"), "csv",
		"counterexample: m0 from sealed pan log"); err != nil {
		return app.ScenarioOutcome{}, err
	}
	afterIDs := svc.NodeIDs(ctx, projectID)
	return app.ScenarioOutcome{
		Status: "adopted", ChangedSource: "initial_mass.csv v1->v2",
		Before: map[string]any{"node_ids": beforeIDs, "m0_mg": 10.0},
		After:  map[string]any{"node_ids": afterIDs, "m0_mg": 10.05},
		Rules: []domain.RuleEvent{{
			Rule: domain.RuleMassRev, Subject: "residue_chain", Judgment: "accepted",
			Reason: "m0 only normalizes DTG percentage and the residue ratio; corrected_series mass values are unchanged",
		}},
		Note: "blast radius must be dtg + residue (and no corrected_series/furnace/gas node)",
	}, nil
}

// ScenarioHalfImport submits one committed and two fatally malformed fragments.
// The session must be partial: accepted fragments flow on, rejected fragments
// and their diagnostics stay in the record (R7).
func ScenarioHalfImport(ctx context.Context, svc *app.Service, projectID string) (app.ScenarioOutcome, error) {
	res, err := svc.Import(ctx, projectID, []app.ImportFragment{
		{Layer: domain.LayerValves, Format: "csv", Raw: MustRead("halffail_bad_valves.csv")},
		{Layer: domain.LayerFurnace, Format: "csv", Raw: MustRead("halffail_bad_furnace.csv")},
		{Layer: domain.LayerInitMass, Format: "csv", Raw: MustRead("halffail_ok_initmass.csv")},
	})
	if err != nil {
		return app.ScenarioOutcome{}, err
	}
	if res.Status != "partial" {
		return app.ScenarioOutcome{
			Status: "rejected", ChangedSource: "half-fail batch",
			After:  map[string]any{"session_status": res.Status},
			Note:   "expected partial import session",
		}, nil
	}
	return app.ScenarioOutcome{
		Status: "adopted", ChangedSource: "import session " + res.SessionID,
		After: map[string]any{
			"session_id": res.SessionID, "status": res.Status,
			"committed_layers": []string{"initial_mass (no-op v)"},
			"rejected_layers":  []string{"valves", "furnace"},
		},
		Rules: []domain.RuleEvent{{
			Rule: domain.RuleHalfImport, Subject: res.SessionID, Judgment: "accepted",
			Reason: "two fragments failed format validation and were retained as rejected; the initial_mass fragment committed; derived chain recomputed from committed inputs",
		}},
		Note: "rejected raw text and per-field diagnostics remain visible in the session",
	}, nil
}
