package engine

import (
	"fmt"
	"math"

	"tga-review/internal/ident"
	"tga-review/internal/types"
)

// attachAtmosphere assigns the atmosphere at the *sample* at each stage peak:
// the valve time plus the configured delay. Valve switch time is explicitly
// recorded as different evidence and never used directly.
func (w *work) attachAtmosphere(stages []stage) {
	delay := w.p["atmos_delay_s"]
	for i := range stages {
		s := &stages[i]
		current := ""
		if len(w.n.gas) > 0 {
			current = w.n.gas[0].From
		}
		reachT := math.NaN()
		valveT := math.NaN()
		for _, g := range w.n.gas {
			if s.PeakT >= g.T+delay {
				current = g.To
				reachT = g.T + delay
				valveT = g.T
			}
		}
		s.GasAtReaction = current
		c := w.chain(s.ID)
		in := map[string]string{
			"valve_event_time_s": fmt.Sprintf("%.1f", valveT),
			"atmos_delay_s":      fmt.Sprintf("%.1f", delay),
			"sample_reaction_s":  fmt.Sprintf("%.1f", s.PeakT),
		}
		if w.src(types.GasValveEvents) != nil {
			in["gas_source"] = w.src(types.GasValveEvents).ID
		}
		c.add("attribute_atmosphere", in,
			fmt.Sprintf("%s at sample (valve at %.0f s, sample at %.0f s)",
				current, valveT, s.PeakT),
			"", "valve switch time != sample reaction time; reaction uses t+delay")
		s.ValveEvidence = append(s.ValveEvidence,
			fmt.Sprintf("valve=%.0fs reach=%.0fs peak=%.0fs", valveT, reachT, s.PeakT))
	}
}

// buildCandidates turns stages and the final residue into first-class
// candidates with complete chain, reject reasons and alternatives.
func (w *work) buildCandidates(stages []stage) {
	for _, s := range stages {
		status := "proposed"
		if s.Ambiguous {
			status = "ambiguous"
		}
		cand := types.Candidate{
			ID:            ident.CandidateID(w.run.ID, s.ID, "stage"),
			StageID:       s.ID,
			Kind:          "stage",
			Status:        status,
			Value:         s.MassLossPct,
			Unit:          units_pct,
			Bounds:        s.Bounds,
			Alternatives:  s.Alternatives,
			RejectReasons: s.RejectReasons,
			Chain:         w.steps[s.ID],
			Prov:          w.provenance(),
		}
		w.run.Candidates = append(w.run.Candidates, cand)
	}

	// Final residue: mean corrected mass over the trailing window, provided the
	// window is not crossed by a sampling gap.
	w.buildResidueCandidate()
}

func (w *work) buildResidueCandidate() {
	pts := w.corr
	window := w.p["residue_window_s"]
	end := pts[len(pts)-1].T
	var sum float64
	var n int
	gapInWindow := false
	var firstIdx int
	for i := len(pts) - 1; i >= 0; i-- {
		if pts[i].T < end-window {
			firstIdx = i + 1
			break
		}
		if pts[i].Gap {
			gapInWindow = true
		}
	}
	for i := firstIdx; i < len(pts); i++ {
		sum += pts[i].CorrMass
		n++
	}
	if n == 0 {
		n, sum, firstIdx = 1, pts[len(pts)-1].CorrMass, len(pts)-1
	}
	finalMass := sum / float64(n)
	residue := 0.0
	if w.initialMass > 0 {
		residue = finalMass / w.initialMass * 100
	}

	c := w.chain("final_residue")
	c.add("window_mean", map[string]string{
		"window_start_s": fmt.Sprintf("%.1f", end-window),
		"window_end_s":   fmt.Sprintf("%.1f", end),
		"point_count":    fmt.Sprintf("%d", n),
		"blank_curve":    sourceID(w.src(types.BlankCurve)),
		"drift_layer":    sourceID(w.src(types.DriftCorrection)),
	}, fmt.Sprintf("%.4f mg", finalMass), units_mg,
		"trailing-window mean of drift+blank corrected mass")
	c.add("residue_ratio", map[string]string{
		"final_mg":    fmt.Sprintf("%.4f", finalMass),
		"initial_mg":  fmt.Sprintf("%.4f", w.initialMass),
		"basis":       w.initialBasis,
	}, fmt.Sprintf("%.3f %%", residue), units_pct,
		"residue = final / initial * 100; independent of stage loss sum")

	reasons := []string{}
	status := "proposed"
	if gapInWindow {
		reasons = append(reasons, "sampling gap crosses residue window; value is uncertain")
		status = "ambiguous"
	}
	w.finalMass = finalMass
	w.run.Candidates = append(w.run.Candidates, types.Candidate{
		ID:            ident.CandidateID(w.run.ID, "final_residue", "final_residue"),
		StageID:       "final_residue",
		Kind:          "final_residue",
		Status:        status,
		Value:         residue,
		Unit:          units_pct,
		RejectReasons: reasons,
		Chain:         w.steps["final_residue"],
		Prov:          w.provenance(),
	})
}

func sourceID(v *types.SourceVersion) string {
	if v == nil {
		return ""
	}
	return v.ID
}

func (w *work) finalizeRun(stages []stage) {
	w.run.Series = map[string]types.Series{
		"raw":               w.rawSeries,
		"corrected":         w.corrSeries,
		"tg":                w.tgSeries,
		"dtg":               w.dtgSeries,
		"program":           w.programSeries,
		"sample_temp_est":   w.programSampleSeries,
		"valve":             w.gasSeries,
		"atmos_reach":       w.atmosReachSeries,
	}
	w.run.Summary = types.RunSummary{
		InitialMassMG:   w.initialMass,
		FinalMassMG:     w.finalMass,
		FinalResiduePct: w.finalMass / safeDiv(w.initialMass) * 100,
		StageCount:      len(stages),
	}
	for _, p := range w.corr {
		if p.Gap {
			w.run.Summary.HasGap = true
		}
		if p.NonMonotonic {
			w.run.Summary.HasNonMonotonic = true
		}
	}
	for _, c := range w.run.Candidates {
		if c.Status == "ambiguous" || len(c.Alternatives) > 0 {
			w.run.Summary.HasAmbiguity = true
		}
	}
	w.run.Notes = w.notes
	if w.run.Status == "" {
		w.run.Status = "ok"
	}
}

func safeDiv(v float64) float64 {
	if v == 0 {
		return 1
	}
	return v
}
