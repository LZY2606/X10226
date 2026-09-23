package calc

import (
	"fmt"

	"tga-review/internal/domain"
)

// ComputeResidue builds the fully expanded residue calculation chain.
//
// R3: accepted stage losses are reported with their overlap flags and their
// percentages are NOT forced to sum to 100 (residue comes from direct mass
// ratio, not from 100 minus stage losses).
func ComputeResidue(in Inputs, corr Corrected, stages Stages, acceptedKeys map[string]bool, confirmed bool) ResidueChain {
	m0 := initialMassMg(in.InitMass)
	out := ResidueChain{M0Mg: m0}
	if m0 <= 0 {
		out.BalanceNote = "initial mass missing; residue cannot be normalized"
		return out
	}
	out.Steps = append(out.Steps, ChainStep{
		Label: "initial mass m0", Formula: "m0 = stratified initial mass layer (revision v)",
		Value: &domain.Q{Value: m0, Unit: domain.UnitMilligram, Original: fmt.Sprintf("%g", m0), Valid: true},
	})

	// final mass: mean of the last trailing window of consecutive usable points
	tailS := 60.0
	usable := []CorrectedPoint{}
	for _, cp := range corr.Points {
		if cp.Usable {
			usable = append(usable, cp)
		}
	}
	if len(usable) < 2 {
		out.BalanceNote = "no usable points remain after quarantine/gaps"
		return out
	}
	tmax := usable[len(usable)-1].TS
	var tail []CorrectedPoint
	lastRegion := usable[len(usable)-1].Region
	for i := len(usable) - 1; i >= 0; i-- {
		if usable[i].Region != lastRegion || tmax-usable[i].TS > tailS {
			break
		}
		tail = append(tail, usable[i])
	}
	sum := 0.0
	for _, cp := range tail {
		sum += cp.CorrMg
	}
	final := sum / float64(len(tail))
	out.FinalMg = final
	out.Steps = append(out.Steps,
		ChainStep{
			Label: "final plateau mean", Formula: "mean(corrected mass over last 60 s within final region)",
			Detail: map[string]float64{"window_start_s": tail[len(tail)-1].TS, "window_end_s": tail[0].TS, "n_points": float64(len(tail))},
			Value:  &domain.Q{Value: final, Unit: domain.UnitMilligram, Original: fmt.Sprintf("%g", final), Valid: true},
		},
		ChainStep{
			Label: "residue", Formula: "residue_% = m_final / m0 * 100",
			Inputs: []string{fmt.Sprintf("m_final=%g mg", final), fmt.Sprintf("m0=%g mg", m0)},
			Value:  &domain.Q{Value: final / m0 * 100, Unit: domain.UnitPercent, Original: fmt.Sprintf("%g%%", final/m0*100), Valid: true},
		},
	)
	out.ResiduePct = final / m0 * 100

	// accepted stage losses with explicit overlap accounting
	var accepted []StageLoss
	sumAcc := 0.0
	overlap := false
	for _, c := range stages.Candidates {
		if !acceptedKeys[c.Key] {
			continue
		}
		accepted = append(accepted, StageLoss{Key: c.Key, AreaPct: c.AreaPct})
		sumAcc += c.AreaPct
	}
	// mark overlap among accepted windows
	for i := range accepted {
		ci := findStage(stages, accepted[i].Key)
		for j := range accepted {
			if i == j {
				continue
			}
			cj := findStage(stages, accepted[j].Key)
			if ci.StartS < cj.EndS && cj.StartS < ci.EndS {
				accepted[i].Overlap = true
				overlap = true
			}
		}
	}
	out.AcceptedLoss = accepted
	out.SumAccepted = sumAcc
	switch {
	case overlap:
		out.BalanceNote = "accepted stages overlap: percentages overlap in time and are NOT a partition (R3); they need not sum to 100%"
	case confirmed:
		out.BalanceNote = "accepted stages are disjoint; their sum is informational, residue is measured directly"
	default:
		out.BalanceNote = "residue is a direct mass ratio; stage-loss percentages are not forced to sum to 100% (R3)"
	}
	return out
}

func findStage(st Stages, key string) StageCandidate {
	for _, c := range st.Candidates {
		if c.Key == key {
			return c
		}
	}
	return StageCandidate{}
}
