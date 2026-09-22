package derive

import (
	"math"

	"tga/internal/domain"
)

// residueRates computes the final residue (%) from the tail plateau. If a
// sampling gap or unknown tail prevents a unique plateau, the result is marked
// unknown with all legal window candidates retained; it never invents a number.
func residueRates(p domain.Params, in *Inputs, s domain.NormalizedSeries, phases []domain.PhaseCandidate) ([]domain.ResidueRate, []domain.RuleEvent) {
	n := len(s.Times)
	if n == 0 {
		return nil, nil
	}
	initial := resolveInitialMass(in, s.MassMG, validMask(s))
	lastValid := -1
	for i := n - 1; i >= 0; i-- {
		if !math.IsNaN(s.MassMG[i]) {
			lastValid = i
			break
		}
	}
	if lastValid < 0 {
		return []domain.ResidueRate{{
			ID: "res:0", Status: domain.CandProposed, Unknown: true,
			UnknownReason: "no valid mass points after normalization",
			Chain:         []domain.CalcChainStep{{Order: 0, Op: "residue_blocked", Outputs: map[string]any{"reason": "no_valid_mass"}}},
		}}, nil
	}

	// tail window: last up-to-5% of valid samples, but never across a gap.
	start := lastValid
	cnt := 0
	windowFrac := int(math.Ceil(float64(n) * 0.05))
	if windowFrac < 3 {
		windowFrac = 3
	}
	if windowFrac > n {
		windowFrac = n
	}
	for i := lastValid; i >= 0 && cnt < windowFrac; i-- {
		if math.IsNaN(s.MassMG[i]) {
			break
		}
		if i < lastValid && !withinRun(s.Gaps, i, i+1) {
			break
		}
		start = i
		cnt++
	}
	var sum, sq float64
	for i := start; i <= lastValid; i++ {
		sum += s.MassMG[i]
		sq += s.MassMG[i] * s.MassMG[i]
	}
	mean := sum / float64(cnt)
	variance := math.Max(0, sq/float64(cnt)-mean*mean)
	sd := math.Sqrt(variance)
	pct := math.NaN()
	if initial > 0 {
		pct = mean / initial * 100
	}
	errPct := math.NaN()
	if initial > 0 {
		errPct = (sd / initial) * 100
	}

	chain := []domain.CalcChainStep{
		{Order: 0, Op: "select_initial_mass", Unit: "mg",
			Inputs:    map[string]any{"method": initialMethod(in), "version": in.Versions[domain.KindInitialMass]},
			Outputs:   map[string]any{"initial_mg": round(initial, 6)},
			SourceIDs: sourceID(in, domain.KindInitialMass)},
		{Order: 1, Op: "tail_window_without_gap_spanning", Unit: "index",
			Inputs:  map[string]any{"start_index": start, "end_index": lastValid, "points": cnt},
			Outputs: map[string]any{"start_time_s": round(s.Times[start], 6), "end_time_s": round(s.Times[lastValid], 6)}},
		{Order: 2, Op: "plateau_mean", Unit: "mg",
			Inputs:  map[string]any{"points": cnt},
			Outputs: map[string]any{"mean_mg": round(mean, 6), "stddev_mg": round(sd, 6)}},
		{Order: 3, Op: "residue_percent", Unit: "%",
			Inputs:  map[string]any{"mean_mg": round(mean, 6), "initial_mg": round(initial, 6)},
			Outputs: map[string]any{"residue_pct": round(pct, 4), "error_pct": round(errPct, 6)}},
	}

	r := domain.ResidueRate{
		ID: "res:0", ValuePct: round(pct, 4), WindowEnd: round(s.Times[lastValid], 6),
		Status: domain.CandProposed, Chain: chain,
	}
	if !math.IsNaN(errPct) {
		e := round(errPct, 6)
		r.ErrorPct = &e
	}
	var rules []domain.RuleEvent
	// If the tail is cut by a gap, keep an unknown alternative.
	if start > 0 && !withinRun(s.Gaps, start-1, start) {
		r.Unknown = false
		alt := domain.ResidueRate{
			ID: "res:0:gap", Status: domain.CandProposed, Unknown: true,
			UnknownReason: "tail plateau begins at a sampling gap; post-gap-only residue is a separate legal candidate",
			WindowEnd:     round(s.Times[lastValid], 6),
			Chain: []domain.CalcChainStep{{Order: 0, Op: "residue_gap_split",
				Inputs:  map[string]any{"gap_start_index": start},
				Outputs: map[string]any{"candidate_ids": []string{"res:0", "res:0:gap"}}}},
		}
		r.AlternateIDs = append(r.AlternateIDs, alt.ID)
		rules = append(rules, domain.RuleEvent{
			Code: domain.RuleAmbiguousCandidates, Severity: "warn",
			Message:  "final residue not uniquely decidable across a tail gap; both plateau candidates retained",
			Evidence: map[string]any{"candidates": []string{"res:0", "res:0:gap"}},
		})
		return []domain.ResidueRate{r, alt}, rules
	}
	return []domain.ResidueRate{r}, rules
}

func validMask(s domain.NormalizedSeries) []bool {
	v := make([]bool, len(s.Times))
	for i := range s.Times {
		v[i] = !math.IsNaN(s.Times[i]) && !math.IsNaN(s.MassMG[i])
	}
	return v
}
