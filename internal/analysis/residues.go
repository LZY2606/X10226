package analysis

import (
	"fmt"

	"tga-review/internal/domain"
)

// buildResidues computes the final residual percentage relative to the
// declared initial mass, with an expandable calculation chain that cites the
// exact source layer versions and samples. When the final reading is bounded
// by a gap the residue is reported as unknown/gap_bounded, never invented.
func buildResidues(in Inputs, b *domain.AnalysisBundle) []domain.Residue {
	tr := b.Trace
	if in.InitialMass == nil {
		return []domain.Residue{{
			ID:            "residue-final",
			Label:         "Final residue",
			Status:        "unknown",
			UnknownReason: "no initial mass committed; residual percentage cannot be normalised",
			Chain: []domain.CalcChainStep{{
				Order: 1, Op: "require", Detail: "initial mass layer", Source: "source:initial_mass",
			}},
		}}
	}
	// final present sample (do not cross trailing gaps)
	lastIdx := -1
	for i := range tr.Times {
		if tr.Present[i] {
			lastIdx = i
		}
	}
	if lastIdx < 0 {
		return []domain.Residue{{ID: "residue-final", Label: "Final residue", Status: "unknown",
			UnknownReason: "no present sample mass available", InitialMg: in.InitialMass.Mass.Value}}
	}
	gapBounded := false
	for _, g := range tr.Gaps {
		if g.AfterIndex > lastIdx || g.BeforeIndex == lastIdx || g.AfterIndex == lastIdx+1 {
			// gap after the last present reading: final mass may be incomplete
			if g.StartT >= tr.Times[lastIdx]-pseudoEps {
				gapBounded = true
			}
		}
	}
	rawFinal := tr.RawMg[lastIdx]
	corrFinal := tr.CorrectedMg[lastIdx]
	t := tr.Times[lastIdx]
	init := in.InitialMass.Mass.Value
	var driftDelta float64
	if in.Drift != nil {
		driftDelta = in.Drift.Slope.Value*t + in.Drift.Intercept.Value
	}

	src := func(layer string) string {
		for _, r := range b.SourceLayers {
			if r.Layer == layer {
				return "source:" + layer + "@" + r.Version
			}
		}
		return "source:" + layer
	}

	r := domain.Residue{
		ID:         "residue-final",
		Label:      "Final residue",
		FinalT:     t,
		FinalTempC: tr.TempC[lastIdx],
		FinalMg:    corrFinal,
		InitialMg:  init,
		Percent:    corrFinal / init * 100,
		Status:     "ok",
		Chain: []domain.CalcChainStep{
			{Order: 1, Op: "read", Detail: "declared initial sample mass " + in.InitialMass.MassRaw, ValueMg: init, Source: src(domain.LayerInitialMass) + "#mass"},
			{Order: 2, Op: "read", Detail: fmt.Sprintf("raw balance reading at final present sample (index %d, t=%.1fs)", lastIdx, t), ValueMg: rawFinal, Time: t, Source: src(domain.LayerSamplePoints) + "#points[" + itoa(lastIdx) + "].mass"},
		},
	}
	order := 3
	if in.Drift != nil {
		r.Chain = append(r.Chain, domain.CalcChainStep{
			Order: order, Op: "subtract_baseline",
			Detail:  fmt.Sprintf("balance drift correction %.6g*t + %.6g at t=%.1fs", in.Drift.Slope.Value, in.Drift.Intercept.Value, t),
			ValueMg: driftDelta, Time: t, Source: src(domain.LayerDriftCorr) + "#model",
		})
		order++
	}
	r.Chain = append(r.Chain,
		domain.CalcChainStep{Order: order, Op: "correct", Detail: "corrected final mass = raw - baseline drift", ValueMg: corrFinal, Time: t, Source: "derived:trace#corrected_mg[" + itoa(lastIdx) + "]"},
	)
	order++
	r.Chain = append(r.Chain, domain.CalcChainStep{
		Order: order, Op: "ratio", Detail: "residual % = corrected final mass / declared initial mass * 100", ValueMg: corrFinal, Time: t, Source: "derived:residue-final",
	})
	if gapBounded {
		r.Status = "gap_bounded"
		r.UnknownReason = "the run ends inside or before a sampling gap; final residue is a lower-bound, not a unique value"
	}
	return []domain.Residue{r}
}

func residueJudgements(rs []domain.Residue) []domain.Judgement {
	var js []domain.Judgement
	for _, r := range rs {
		switch r.Status {
		case "ok":
			js = append(js, j(domain.SevInfo, "residue_ok",
				fmt.Sprintf("final residue %.2f%% with complete calculation chain", r.Percent), r.ID,
				map[string]any{"percent": r.Percent, "initial_mg": r.InitialMg, "final_mg": r.FinalMg}))
		case "gap_bounded":
			js = append(js, j(domain.SevWarning, "residue_gap_bounded", r.UnknownReason, r.ID,
				map[string]any{"percent": r.Percent}))
		default:
			js = append(js, j(domain.SevError, "residue_unknown", r.UnknownReason, r.ID, nil))
		}
	}
	return js
}
