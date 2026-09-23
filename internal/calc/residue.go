package calc

import (
	"fmt"

	"gotga/internal/domain"
)

// buildResidues computes final residue with a full calculation chain. If the
// last segment ends at a gap boundary or mass0 is missing, the value stays
// unknown with an explicit reason instead of a fabricated percentage.
func buildResidues(segs [][]domain.Point, in *Inputs, p domain.Params, fp string, seq int64, gaps []domain.Gap) []domain.Residue {
	provDocs := []string{in.SampleDoc.ID}
	if in.Mass0Doc != nil {
		provDocs = append(provDocs, in.Mass0Doc.ID)
	}
	r := domain.Residue{
		ID:     domain.HashID("residue", fp, in.Mass0.MG, len(segs)),
		Unit:   "%",
		Status: "current",
		Prov: domain.Provenance{
			Algorithm: p.Version + ":final-residue", ParamFP: fp, DocIDs: provDocs, EventSeq: seq,
			Formula: "residue% = m_final(drift-corrected) / mass0 * 100",
		},
	}
	if len(segs) == 0 || len(segs[len(segs)-1]) == 0 {
		r.Status, r.UnknownWhy = "unknown", "no convertible sample points"
		return []domain.Residue{r}
	}
	last := segs[len(segs)-1]
	final := last[len(last)-1]
	r.Chain = append(r.Chain, domain.ChainStep{RefID: "raw:final", Op: "take last observed point", Value: final.M, Unit: "mg", Note: fmt.Sprintf("t=%.0fs T=%.1fC", final.T, final.Tmp)})
	if len(gaps) > 0 {
		// If the final point is followed by nothing and a gap precedes the last
		// segment, residue is still computable from observed data but flagged.
		r.Chain = append(r.Chain, domain.ChainStep{RefID: domain.AxiomGapNoInterp, Op: "assert", Value: 0, Unit: "-", Note: fmt.Sprintf("%d gap(s) not interpolated; residue based on observed tail only", len(gaps))})
	}
	if !in.Mass0.Valid {
		r.Status = "unknown"
		r.UnknownWhy = "mass0 layer missing or unconvertible"
		r.Chain = append(r.Chain, domain.ChainStep{RefID: "raw:mass0", Op: "missing", Value: 0, Unit: "mg", Note: in.Mass0.Diag})
		r.JudgmentIDs = append(r.JudgmentIDs, domain.HashID("judg", domain.AxiomUnconvertibleKept, r.ID))
		return []domain.Residue{r}
	}
	r.Chain = append(r.Chain,
		domain.ChainStep{RefID: "raw:mass0", Op: "use stratified initial mass", Value: in.Mass0.MG, Unit: "mg", Note: "from mass0 layer (revisioned)"},
		domain.ChainStep{RefID: "op:div", Op: "divide", Value: final.M / in.Mass0.MG, Unit: "fraction"},
	)
	v := final.M / in.Mass0.MG * 100
	r.Value = &v
	r.Chain = append(r.Chain, domain.ChainStep{RefID: r.ID, Op: "times 100", Value: v, Unit: "%", Note: "final residue"})
	return []domain.Residue{r}
}

func unknownResidues(in *Inputs, p domain.Params, seq int64) []domain.Residue {
	r := domain.Residue{
		ID:         domain.HashID("residue", "unknown", seq),
		Unit:       "%",
		Status:     "unknown",
		UnknownWhy: "insufficient convertible points",
		Prov:       domain.Provenance{Algorithm: p.Version, ParamFP: domain.ParamFingerprint(p), EventSeq: seq},
	}
	return []domain.Residue{r}
}

func buildJudgments(b *domain.ResultBundle, in *Inputs, p domain.Params, seq int64, gaps []domain.Gap) []domain.Judgment {
	var js []domain.Judgment
	if len(in.Valves) > 0 {
		js = append(js, domain.Judgment{
			ID: domain.HashID("judg", domain.AxiomValveReactionLag, seq), Axiom: domain.AxiomValveReactionLag,
			SubjectID: "timeline", SubjectKind: "timeline", EventSeq: seq,
			Detail: fmt.Sprintf("valve switch time shifted by atm_delay=%.0fs and temp_lag=%.1fC when matching reactions", p.AtmDelayS, p.TempLagC),
		})
	}
	overlap := false
	sum := 0.0
	for _, st := range b.Stages {
		if len(st.OverlapsWith) > 0 {
			overlap = true
		}
		if st.LossPct != nil {
			sum += *st.LossPct
		}
	}
	if overlap || sum > 100.0001 {
		js = append(js, domain.Judgment{
			ID: domain.HashID("judg", domain.AxiomOverlapNotSum100, seq), Axiom: domain.AxiomOverlapNotSum100,
			SubjectID: "stages", SubjectKind: "stage-set", EventSeq: seq,
			Detail: fmt.Sprintf("stages may overlap; reported separately (sum %.2f%% is not normalized to 100)", sum),
		})
	}
	if len(gaps) > 0 {
		js = append(js, domain.Judgment{
			ID: domain.HashID("judg", domain.AxiomGapNoInterp, seq), Axiom: domain.AxiomGapNoInterp,
			SubjectID: "series", SubjectKind: "series", EventSeq: seq,
			Detail: fmt.Sprintf("%d sampling gap(s) split the series; interpolation never crosses", len(gaps)),
		})
	}
	amb := false
	for _, pk := range b.Peaks {
		if len(pk.AltIDs) > 0 {
			amb = true
		}
	}
	if amb {
		js = append(js, domain.Judgment{
			ID: domain.HashID("judg", domain.AxiomAmbiguityKept, seq), Axiom: domain.AxiomAmbiguityKept,
			SubjectID: "peaks", SubjectKind: "peak-set", EventSeq: seq,
			Detail: "overlapping peaks cannot be uniquely split; all legal candidates retained",
		})
	}
	if len(b.Diags) > 0 {
		js = append(js, domain.Judgment{
			ID: domain.HashID("judg", domain.AxiomUnconvertibleKept, seq), Axiom: domain.AxiomUnconvertibleKept,
			SubjectID: "raw", SubjectKind: "documents", EventSeq: seq,
			Detail: "unconvertible values remain in original position with diagnostics",
		})
	}
	return js
}

func mkJudgment(axiom, subject, kind, detail string, seq int64) domain.Judgment {
	return domain.Judgment{ID: domain.HashID("judg", axiom, subject, seq), Axiom: axiom, SubjectID: subject, SubjectKind: kind, Detail: detail, EventSeq: seq}
}
