package compute

import (
	"fmt"

	"tgasys/internal/domain"
	"tgasys/internal/ingest"
)

// buildResidues computes interim and final residue rates with fully expanded
// calculation chains that name every source version and algorithm step.
func buildResidues(tg []domain.TGPoint, m0 float64, samples []domain.SamplePoint,
	initial []ingest.InitialMass, initChain []domain.ChainStep,
	drift *linearDrift, blank *blankCurve, p *domain.Parameters, gaps []domain.Gap) []domain.Residue {
	var out []domain.Residue

	mk := func(label string, idxFromEnd int) domain.Residue {
		idx := len(tg) - 1 - idxFromEnd
		if idx < 0 {
			idx = 0
		}
		r := domain.Residue{
			ID:    residueID(label),
			Label: label,
			AtT:   tg[idx].T,
		}
		order := 0
		for _, c := range initChain {
			c.Order = order
			order++
			r.Chain = append(r.Chain, c)
		}
		r.Chain = append(r.Chain,
			domain.ChainStep{Order: order, Op: "correct.drift", Algo: AlgoVersion,
				Detail: drift.desc, Output: fmt.Sprintf("m_corr(t) = m_raw(t) - (%.3e + %.3e*t) mg", drift.a, drift.b)},
		)
		order++
		if blank != nil {
			r.Chain = append(r.Chain, domain.ChainStep{Order: order, Op: "correct.blank", Algo: AlgoVersion,
				Detail: fmt.Sprintf("blank curve fit on %d points", blank.n),
				Output: fmt.Sprintf("m_corr(t) -= %.3e + %.3e*t + %.3e*t^2 mg", blank.a, blank.b, blank.c)})
			order++
		}
		mCorr := tg[idx].Mass
		nearGap := inGap(r.AtT, gaps) || nearGapEdge(r.AtT, gaps)
		r.Chain = append(r.Chain, domain.ChainStep{Order: order, Op: "sample.select", Algo: AlgoVersion,
			Detail: fmt.Sprintf("choose corrected mass at t=%.1fs (no interpolation across gaps)", r.AtT),
			Output: fmt.Sprintf("%.6f mg", mCorr)},
		)
		order++
		r.Chain = append(r.Chain, domain.ChainStep{Order: order, Op: "residue.ratio", Algo: AlgoVersion,
			Detail: "residue% = 100 * m_corr(t) / m0_selected",
			Inputs: map[string]string{"m_corr": fmt.Sprintf("%.6f mg", mCorr), "m0": fmt.Sprintf("%.6f mg", m0)},
			Output: fmt.Sprintf("%.4f %%", 100*mCorr/m0)},
		)
		if nearGap {
			r.PctStatus = "unknown"
			r.Issues = append(r.Issues, domain.Issue{Code: "residue.adjacent_gap", Severity: "warning",
				Message: "residue point touches a data gap; value shown but flagged unknown, not interpolated"})
		} else {
			r.PctStatus = "ok"
		}
		r.Pct = 100 * mCorr / m0
		return r
	}

	if len(tg) == 0 {
		return out
	}
	// Half-way interim residue (at median sample) and final residue.
	out = append(out, mk("interim_residue", len(tg)/2))
	out = append(out, mk("final_residue", 0))
	return out
}

func residueID(label string) string {
	switch label {
	case "final_residue":
		return "residue-final"
	default:
		return "residue-interim"
	}
}
