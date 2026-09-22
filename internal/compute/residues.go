package compute

import (
	"fmt"

	"tgareview/internal/domain"
)

// residues builds residual-rate statements at furnace-program landmarks and
// the final sample. Each residue carries an expandable calculation chain.
func residues(corr []domain.CorrPoint, ws []domain.WeightRow, gaps []domain.Gap,
	program []domain.ProgramStep, p domain.Params, prov domain.Provenance,
) []domain.Residue {
	w := activeWeight(ws)
	type landmark struct {
		t     float64
		label string
	}
	var marks []landmark
	for _, st := range program {
		if !st.EndS.Bad {
			marks = append(marks, landmark{st.EndS.Value, "program_end:" + st.ID})
		}
	}
	if len(corr) > 0 {
		marks = append(marks, landmark{corr[len(corr)-1].TimeS, "final_sample"})
	}
	var out []domain.Residue
	for _, m := range marks {
		mass, known := massAt(corr, m.t)
		// No interpolation across gaps: a landmark inside a gap is unknown.
		for _, g := range gaps {
			if m.t > g.StartS && m.t < g.EndS {
				known = false
			}
		}
		id := domain.ShortID("residue", f64(m.t))
		r := domain.Residue{
			ID: id, AtS: m.t, Status: domain.StatusCurrent,
			MassMg: domain.Q{Value: mass, Unit: domain.UnitMg, Basis: "furnace_time"},
			Prov: withSources(prov,
				domain.Source{Kind: "derived", ID: id, Detail: m.label}),
		}
		chain := []domain.ChainNode{{
			ID: domain.ShortID("node", id, "mass"), Kind: "corrected_point",
			Label:    "corrected mass at furnace time",
			Value:    domain.Q{Value: mass, Unit: domain.UnitMg, Basis: "furnace_time"},
			SourceID: domain.ShortID("corr", f64(m.t)),
			Formula:  "raw - drift - blank",
		}}
		if w != nil {
			r.InitialMg = domain.Q{Value: w.Mg.Value, Unit: domain.UnitMg, Basis: "balance_at_loading"}
			pct := 100.0 * mass / w.Mg.Value
			r.Pct = domain.Q{Value: pct, Unit: domain.UnitPct, Basis: "active_initial_mass:" + w.ID}
			chain = append(chain,
				domain.ChainNode{
					ID: domain.ShortID("node", id, "w0"), Kind: "weight",
					Label:    "active initial mass",
					Value:    domain.Q{Value: w.Mg.Value, Unit: domain.UnitMg, Basis: "balance_at_loading"},
					SourceID: w.ID,
				},
				domain.ChainNode{
					ID: domain.ShortID("node", id, "pct"), Kind: "pct",
					Label:    "residue percent",
					Value:    r.Pct,
					SourceID: id,
					Formula:  fmt.Sprintf("100 * %.6g / %.6g", mass, w.Mg.Value),
				})
		} else {
			r.Pct = domain.Q{Bad: true, Unit: domain.UnitPct, Diag: codeWeightUnknown}
			r.Status = domain.StatusUnknown
			chain = append(chain, domain.ChainNode{
				ID: domain.ShortID("node", id, "w0x"), Kind: "weight",
				Label: "initial mass (missing/half-failed import)",
				Value: domain.Q{Bad: true, Unit: domain.UnitMg, Diag: codeWeightUnknown},
			})
		}
		if !known {
			r.Pct = domain.Q{Bad: true, Unit: domain.UnitPct, Diag: codeGapNoInterp}
			r.Status = domain.StatusUnknown
		}
		r.Chain = chain
		out = append(out, r)
	}
	return out
}
