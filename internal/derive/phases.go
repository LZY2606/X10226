package derive

import (
	"fmt"
	"math"

	"tga/internal/domain"
)

// decomposePhases turns DTG peaks into phase candidates. Phases may overlap and
// mass losses are not normalized to sum to 100%. When a gap or an atmosphere
// reaction boundary intersects a basin, every legal bounding choice is kept as
// an alternate candidate instead of guessing.
func decomposePhases(p domain.Params, s domain.NormalizedSeries, peaks []domain.Peak, atmos []domain.AtmosReaction) ([]domain.PhaseCandidate, []domain.RuleEvent) {
	var phases []domain.PhaseCandidate
	var rules []domain.RuleEvent
	overlapSeen := false
	ambiguousSeen := false

	for pi, pk := range peaks {
		startIdx, endIdx := basinBounds(s, pi, peaks)
		bounds := legalBounds(s, startIdx, endIdx, atmos)
		primary := bounds[0]
		cand := makePhase(fmt.Sprintf("ph:%d", pi), s, primary[0], primary[1], pk)
		cand.PeakIDs = []string{pk.ID}
		for _, o := range pk.OverlapIDs {
			cand.OverlapsWith = append(cand.OverlapsWith, o)
			overlapSeen = true
		}
		// link atmosphere reaction when reaction time lies in the basin
		for _, a := range atmos {
			if a.ReactionTime >= cand.StartTime && a.ReactionTime <= cand.EndTime {
				cand.AtmosLinks = append(cand.AtmosLinks, a.ValveID)
			}
		}
		if len(bounds) > 1 {
			cand.Uncertainty = "ambiguous_bounds"
			ambiguousSeen = true
			for bi, bnd := range bounds[1:] {
				alt := makePhase(fmt.Sprintf("ph:%d:alt%d", pi, bi), s, bnd[0], bnd[1], pk)
				alt.Uncertainty = "ambiguous_bounds"
				alt.OverlapsWith = append(alt.OverlapsWith, cand.OverlapsWith...)
				cand.AlternateBoundIDs = append(cand.AlternateBoundIDs, alt.ID)
				phases = append(phases, alt)
			}
		}
		if hasGapInRange(s.Gaps, cand.StartTime, cand.EndTime) {
			cand.Uncertainty = "gap_inside_basin"
		}
		phases = append(phases, cand)
	}
	if overlapSeen {
		rules = append(rules, domain.RuleEvent{
			Code: domain.RulePhasesOverlap, Severity: "info",
			Message: "decomposition phases may overlap in time; overlapping candidates retained",
		})
	}
	rules = append(rules, domain.RuleEvent{
		Code: domain.RuleNoForcedSum, Severity: "info",
		Message:  "phase mass losses are reported independently and are not rescaled to force a 100% total",
		Evidence: map[string]any{"force_sum_100": p.ForceSum100},
	})
	if ambiguousSeen {
		rules = append(rules, domain.RuleEvent{
			Code: domain.RuleAmbiguousCandidates, Severity: "warn",
			Message: "phase bounds not uniquely decidable; all legal candidates retained",
		})
	}
	return phases, rules
}

// basinBounds finds the shoulder indices of a peak (DTG closest to 0 or NaN).
func basinBounds(s domain.NormalizedSeries, pi int, peaks []domain.Peak) (int, int) {
	peakIdx := indexAtTime(s.Times, peaks[pi].Time)
	start := peakIdx
	for j := peakIdx - 1; j >= 0; j-- {
		if math.IsNaN(s.DTG[j]) {
			break
		}
		start = j
		if s.DTG[j] >= 0 {
			break
		}
	}
	end := peakIdx
	for j := peakIdx + 1; j < len(s.DTG); j++ {
		if math.IsNaN(s.DTG[j]) {
			break
		}
		end = j
		if s.DTG[j] >= 0 {
			break
		}
	}
	return start, end
}

// legalBounds returns all defensible [start,end] index pairs for a basin. In
// addition to the widest basin, a gap edge or atmosphere reaction boundary
// inside the basin creates additional legal (narrower) candidates.
func legalBounds(s domain.NormalizedSeries, start, end int, atmos []domain.AtmosReaction) [][2]int {
	bounds := [][2]int{{start, end}}
	seen := map[[2]int]bool{{start, end}: true}
	add := func(a, b int) {
		if a >= start && b <= end && a < b {
			k := [2]int{a, b}
			if !seen[k] {
				seen[k] = true
				bounds = append(bounds, k)
			}
		}
	}
	for _, g := range s.Gaps {
		if g.StartIndex > start && g.StartIndex < end {
			add(start, g.StartIndex) // only-samples-before-gap candidate
		}
		if g.EndIndex > start && g.EndIndex < end {
			add(g.EndIndex, end) // only-samples-after-gap candidate
		}
	}
	for _, a := range atmos {
		ai := indexAtTime(s.Times, a.ReactionTime)
		if ai > start && ai < end {
			add(start, ai)
			add(ai, end)
		}
	}
	return bounds
}

func makePhase(id string, s domain.NormalizedSeries, startIdx, endIdx int, pk domain.Peak) domain.PhaseCandidate {
	loss := s.MassMG[startIdx] - s.MassMG[endIdx]
	pct := s.MassPct[startIdx] - s.MassPct[endIdx]
	return domain.PhaseCandidate{
		ID: id, Label: phaseLabel(pk.TempC),
		StartTime: round(s.Times[startIdx], 6), EndTime: round(s.Times[endIdx], 6),
		StartTempC: round(s.TempC[startIdx], 3), EndTempC: round(s.TempC[endIdx], 3),
		MassLossMG: round(loss, 6), MassLossPct: round(pct, 4),
		Status: domain.CandProposed,
	}
}

func phaseLabel(tempC float64) string {
	switch {
	case tempC < 200:
		return "moisture_volatile"
	case tempC < 600:
		return "primary_decomposition"
	default:
		return "high_tail_residue_transition"
	}
}

func indexAtTime(times []float64, t float64) int {
	best, bestD := 0, math.MaxFloat64
	for i, x := range times {
		if math.IsNaN(x) {
			continue
		}
		if d := math.Abs(x - t); d < bestD {
			bestD, best = d, i
		}
	}
	return best
}

func hasGapInRange(gaps []domain.Gap, t0, t1 float64) bool {
	for _, g := range gaps {
		if g.StartTime >= t0 && g.StartTime <= t1 {
			return true
		}
	}
	return false
}
