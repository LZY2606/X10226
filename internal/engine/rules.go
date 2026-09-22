package engine

import (
	"strconv"

	"tgareview/internal/domain"
	"tgareview/internal/ids"
)

func itoa(i int) string     { return strconv.Itoa(i) }
func ftoa(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }

func appendRule(a *Artifact, r RuleJudgement) {
	a.Judgements = append(a.Judgements, r)
}

func (a *Artifact) addStandardRules() {
	appendRule(a, RuleJudgement{
		Code: "R2", Status: "applies",
		Message: "sample temperature is a first-order-lag estimate of furnace temperature; valve and thermal time bases are distinct",
		Refs:    []string{"basis:" + domain.BasisFurnaceTempC, "basis:" + domain.BasisSampleTempC, "tau_s=" + ftoa(a.Params.ThermalLagTauS)},
	})
	if len(a.Gaps) > 0 {
		refs := make([]string, 0, len(a.Gaps))
		for _, g := range a.Gaps {
			refs = append(refs, "gap["+ftoa(g.T1S)+"->"+ftoa(g.T2S)+"s]")
		}
		appendRule(a, RuleJudgement{
			Code: "R4", Status: "applies",
			Message: "acquisition gaps are not interpolated; DTG smoothing and stage windows stop at interval boundaries",
			Refs:    refs,
		})
	} else {
		appendRule(a, RuleJudgement{
			Code: "R4", Status: "pass",
			Message: "no acquisition gaps detected at gap_max_s threshold",
			Refs:    []string{"gap_max_s=" + ftoa(a.Params.GapMaxS)},
		})
	}
	var nmRefs []string
	for _, p := range a.Points {
		if p.NonMonotonic {
			nmRefs = append(nmRefs, "point#"+itoa(p.Index)+"@t="+ftoa(p.TimeS))
		}
	}
	if len(nmRefs) > 0 {
		appendRule(a, RuleJudgement{
			Code: "R5", Status: "applies",
			Message: "non-monotonic micro-excursions retained at source and flagged; not deleted to make curves smooth",
			Refs:    nmRefs,
		})
	}
	appendRule(a, RuleJudgement{
		Code: "R6", Status: "applies",
		Message: "stage losses are reported individually; stages may overlap and their percentages are not renormalized to total 100%",
	})
	if rev := a.Basis[string(domain.DriftLayer)]; rev != "" {
		appendRule(a, RuleJudgement{
			Code: "R7", Status: "pass",
			Message: "balance baseline drift is applied only on the derived layer; raw sample_points remain immutable",
			Refs:    []string{"rev:" + rev},
		})
	}
}

func buildResidue(a *Artifact, in Inputs, m0 *float64, m0Rev string, driftRate, driftRef float64) {
	a.m0Mg = m0
	chain := &ResidueChain{ID: ids.Of("res", a.ID, "final")}
	add := func(label string, n ChainNode) ChainNode {
		n.ID = ids.Of("node", a.ID, n.Label+"|"+n.Formula+"|"+itoa(len(chain.Nodes)))
		n.Label = label
		chain.Nodes = append(chain.Nodes, n)
		return n
	}

	add("initial_mass", ChainNode{Value: m0, Unit: "mg", Basis: domain.BasisBalanceMassMg,
		SourceKind: string(domain.InitialMass), SourceRev: m0Rev, Status: valStatus(m0),
		Formula: "m0 = initial_mass.mass_mg (revision tracked)"})
	add("drift_rate", ChainNode{Value: &driftRate, Unit: "mg/min", Basis: domain.BasisBalanceMassMg,
		SourceKind: string(domain.DriftLayer), SourceRev: a.Basis[string(domain.DriftLayer)], Status: "ok",
		Formula: "balance linear drift model applied at derived layer"})
	add("drift_reference_time", ChainNode{Value: &driftRef, Unit: "s", Basis: domain.BasisFurnaceTimeS,
		SourceKind: string(domain.DriftLayer), SourceRev: a.Basis[string(domain.DriftLayer)], Status: "ok"})

	var finalPt *Point
	for i := len(a.Points) - 1; i >= 0; i-- {
		if !a.Points[i].MissingInput && a.Points[i].IntervalID != "" {
			finalPt = &a.Points[i]
			break
		}
	}
	if finalPt == nil {
		chain.Status = "unknown"
		add("final_corrected_mass", ChainNode{Status: "unknown",
			Diagnostics: []string{"no valid point available"}})
		a.Residue = chain
		return
	}
	add("final_raw_mass", ChainNode{Value: &finalPt.MRawMg, Unit: "mg", Basis: domain.BasisBalanceMassMg,
		SourceKind: string(domain.SamplePoints), SourceRev: a.Basis[string(domain.SamplePoints)], Status: "ok",
		Formula: "raw balance reading at t=" + ftoa(finalPt.TimeS) + "s"})
	driftAtEnd := driftRate * (finalPt.TimeS - driftRef) / 60.0
	add("drift_correction_at_end", ChainNode{Value: &driftAtEnd, Unit: "mg", Basis: domain.BasisBalanceMassMg,
		Formula: "rate*(t-t_ref)/60"})
	blankAtEnd := finalPt.BlankMg
	add("blank_subtraction_at_end", ChainNode{Value: &blankAtEnd, Unit: "mg", Basis: domain.BasisBalanceMassMg,
		SourceKind: string(domain.BlankCurve), SourceRev: a.Basis[string(domain.BlankCurve)], Status: "ok",
		Formula: "blank(t) linearly interpolated within measured blank range, never extrapolated"})
	corr := finalPt.MCorrMg
	add("final_corrected_mass", ChainNode{Value: &corr, Unit: "mg", Basis: domain.BasisSampleMassMg,
		Formula: "m_raw - drift(t) - blank(t)", Status: "ok"})

	if m0 != nil && *m0 != 0 {
		rp := 100.0 * corr / *m0
		chain.ResiduePct = &rp
		chain.Status = "ok"
		add("residue_percent", ChainNode{Value: &rp, Unit: "%", Basis: domain.BasisSampleMassMg,
			Formula: "100 * m_corr_final / m0", Status: "ok"})
	} else {
		chain.Status = "unknown"
		add("residue_percent", ChainNode{Unit: "%", Status: "unknown",
			Diagnostics: []string{"initial mass unavailable; residue percentage cannot be uniquely determined"}})
	}
	a.Residue = chain
}

func valStatus(v *float64) string {
	if v == nil {
		return "unknown"
	}
	return "ok"
}
