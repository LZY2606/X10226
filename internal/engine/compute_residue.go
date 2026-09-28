package engine

import (
	"fmt"

	"tga/internal/domain"
)

// buildResidue computes per-stage losses and the final residue. Each result is
// produced together with an expandable calculation chain naming its direct
// sources. Results that cannot be uniquely determined are returned as unknown
// rather than fabricated.
func buildResidue(series *domain.SeriesResult, stages *domain.StagesResult, initSrc *domain.Source) *domain.ResidueResultSet {
	out := &domain.ResidueResultSet{}
	var initMg *float64
	initID := ""
	initBasis := ""
	if initSrc != nil && initSrc.InitMass != nil && initSrc.InitMass.Mass.ConvertOK {
		m := initSrc.InitMass.Mass.Value
		initMg = &m
		initID = initSrc.ID
		initBasis = initSrc.InitMass.Basis
	} else if initSrc != nil && initSrc.InitMass != nil {
		out.Diagnostics = append(out.Diagnostics, domain.Diagnostic{
			Code: "initial_mass_unconverted", Severity: "warning",
			Message: "recorded initial mass cannot be converted; percentage results are unknown"})
	}

	var firstMass, lastMass float64
	lastIdx := -1
	haveFirst, haveLast := false, false
	for _, pt := range series.Points {
		if pt.MassMg == nil {
			continue
		}
		if !haveFirst {
			firstMass = *pt.MassMg
			haveFirst = true
		}
		lastMass = *pt.MassMg
		lastIdx = pt.Index
		haveLast = true
	}

	stageIDs := []string{}
	for _, st := range stages.Stages {
		stageIDs = append(stageIDs, st.ID)
		r := domain.ResidueResult{
			ID:      domain.StableID("res-stage", []any{"v1", st.ID}),
			Kind:    "stage_loss", StageID: st.ID,
		}
		chain := []domain.ChainStep{
			{Order: 1, Op: "mass_at_start", Detail: fmt.Sprintf("drift-corrected mass at t=%gs", st.StartS), Value: fmt.Sprintf("%g", st.StartMassMg), Unit: "mg"},
			{Order: 2, Op: "mass_at_end", Detail: fmt.Sprintf("drift-corrected mass at t=%gs", st.EndS), Value: fmt.Sprintf("%g", st.EndMassMg), Unit: "mg"},
			{Order: 3, Op: "subtract", Detail: "loss = start - end", Value: fmt.Sprintf("%g", st.LossMg), Unit: "mg"},
		}
		lossPctVal := domain.Val{Kind: domain.KindDimensionless, Unit: "%"}
		unknown := false
		if initMg != nil {
			pct := st.LossMg / *initMg * 100
			lossPctVal = domain.MustConvert(round4(pct), domain.KindDimensionless)
			lossPctVal.Unit = "%"
			chain = append(chain, domain.ChainStep{
				Order: 4, Op: "fraction_of_initial",
				Detail: fmt.Sprintf("loss / initial mass (%s) * 100", initBasis),
				Inputs: []string{initID}, Value: fmt.Sprintf("%g", round4(pct)), Unit: "%"})
		} else {
			unknown = true
			lossPctVal.Raw = "?"
			lossPctVal.Diagnostic = "initial mass not recorded; percentage unknown"
			chain = append(chain, domain.ChainStep{
				Order: 4, Op: "fraction_of_initial",
				Detail: "no recorded initial mass: percentage cannot be uniquely determined", Value: "unknown"})
		}
		if st.RejectReason != "" {
			r.Status = "rejected"
		} else if unknown {
			r.Status = "unknown"
		} else {
			r.Status = "ok"
		}
		r.Unknown = unknown
		r.Value = lossPctVal
		r.Chain = chain
		r.DirectSources = uniq(append(seriesSourceRefs(series, st.StartS, st.EndS), initID))
		out.Results = append(out.Results, r)
	}

	// Final residue over the full recorded run.
	fin := domain.ResidueResult{ID: domain.StableID("res-final", []any{"v1", seriesFingerprint(series), initID, firstMass, lastMass}),
		Kind: "final_residue"}
	chain := []domain.ChainStep{}
	if !haveLast {
		fin.Status, fin.Unknown = "unknown", true
		fin.Value = domain.Val{Raw: "?", Unit: "%", Diagnostic: "no corrected mass available"}
		chain = append(chain, domain.ChainStep{Order: 1, Op: "final_mass", Detail: "no mass point survived gaps/correction", Value: "unknown"})
		fin.Chain = chain
		out.Results = append(out.Results, fin)
		return out
	}
	chain = append(chain,
		domain.ChainStep{Order: 1, Op: "initial_corrected_mass", Detail: "first drift-corrected sample mass", Value: fmt.Sprintf("%g", firstMass), Unit: "mg"},
		domain.ChainStep{Order: 2, Op: "final_corrected_mass", Detail: fmt.Sprintf("last drift-corrected sample mass (index %d)", lastIdx), Value: fmt.Sprintf("%g", lastMass), Unit: "mg"},
	)
	residVal := domain.Val{Kind: domain.KindDimensionless, Unit: "%"}
	if initMg != nil {
		// residue relative to the separately recorded initial mass
		pct := lastMass / *initMg * 100
		residVal = domain.MustConvert(round4(pct), domain.KindDimensionless)
		residVal.Unit = "%"
		chain = append(chain, domain.ChainStep{
			Order: 3, Op: "residue_fraction", Detail: fmt.Sprintf("final mass / recorded initial mass (%s) * 100", initBasis),
			Inputs: []string{initID}, Value: fmt.Sprintf("%g", round4(pct)), Unit: "%"})
		fin.Status = "ok"
		fin.DirectSources = []string{initID}
	} else {
		// fall back to first measured point; clearly labelled as an assumption
		pct := lastMass / firstMass * 100
		residVal = domain.MustConvert(round4(pct), domain.KindDimensionless)
		residVal.Unit = "%"
		chain = append(chain, domain.ChainStep{
			Order: 3, Op: "residue_fraction", Detail: "no recorded initial mass; final / first measured mass assumed as reference",
			Value: fmt.Sprintf("%g", round4(pct)), Unit: "%"})
		fin.Status = "ok"
		out.Diagnostics = append(out.Diagnostics, domain.Diagnostic{
			Code: "residue_reference_assumed", Severity: "warning",
			Message: "final residue normalized to first measured point because no initial mass was recorded"})
	}
	fin.Value = residVal
	fin.Chain = chain
	fin.DirectSources = uniq(append(fin.DirectSources, seriesFingerprint(series)))
	out.Results = append(out.Results, fin)
	return out
}

func seriesSourceRefs(series *domain.SeriesResult, from, to float64) []string {
	ids := []string{}
	for _, g := range series.Gaps {
		if g.ToS > from && g.FromS < to {
			ids = append(ids, fmt.Sprintf("gap:%g-%g", round4(g.FromS), round4(g.ToS)))
		}
	}
	return ids
}

func seriesFingerprint(series *domain.SeriesResult) string {
	return domain.StableID("series-ref", []any{"v1", len(series.Points), len(series.Gaps), series.DriftApplied})
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

func initialMg(initSrc *domain.Source) float64 {
	if initSrc != nil && initSrc.InitMass != nil && initSrc.InitMass.Mass.ConvertOK {
		return initSrc.InitMass.Mass.Value
	}
	return 0
}
