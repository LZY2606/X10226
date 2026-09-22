package engine

import (
	"fmt"

	"tga-review/internal/domain"
)

func buildResidual(snap *Snapshot, valID, corrID, setID string,
	vs *domain.ValidatedSeries, cs *domain.CorrectedSeries, setArt domain.Artifact) domain.Artifact {

	init := currentInitial(snap)
	last := lastUsable(cs)
	res := domain.Residual{InitialMassMg: init}
	if last != nil {
		res.FinalMassMg = last.CorrectedMg
		res.FinalTempC = last.TempC
		res.FinalTimeS = last.TimeS
		if init > 0 {
			res.ResidualPercent = 100 * last.CorrectedMg / init
		}
	}
	initRec := ""
	if rs := snap.Records[domain.SrcInitial]; len(rs) > 0 {
		initRec = rs[len(rs)-1].ID
	}
	var blankIDs, driftIDs []string
	for _, rs := range snap.Records {
		for _, r := range rs {
			switch r.Kind {
			case domain.SrcBlank:
				blankIDs = append(blankIDs, r.ID)
			case domain.SrcDrift:
				driftIDs = append(driftIDs, r.ID)
			}
		}
	}
	lastPoint := ""
	if rs := snap.Records[domain.SrcPoints]; len(rs) > 0 {
		// chain node points at the validated series artifact instead
	}
	_ = lastPoint

	res.Chain = []domain.ChainNode{
		{
			Step: "initial_mass", Description: "Sample initial mass (current revision)",
			ValueMg: init, Unit: "mg", SourceID: initRec,
		},
		{
			Step: "raw_observation", Description: "Last usable raw TG sample (original representation preserved)",
			ValueMg: func() float64 {
				if last != nil {
					return last.ObservedMg
				}
				return 0
			}(), Unit: "mg", ArtifactID: valID,
		},
		{
			Step:        "drift_correction",
			Description: fmt.Sprintf("Subtract balance baseline drift (%s)", snap.Params.DriftKind),
			ValueMg: func() float64 {
				if last != nil {
					return -last.DriftMg
				}
				return 0
			}(), Unit: "mg", SourceID: firstID(driftIDs),
			Children: []domain.ChainNode{{
				Step:        "drift_detail",
				Description: "Drift rate parameter revision carries a parent revision in the event log",
			}},
		},
		{
			Step: "blank_correction", Description: "Subtract interpolated blank-curve contribution at final time",
			ValueMg: func() float64 {
				if last != nil {
					return -last.BlankMg
				}
				return 0
			}(), Unit: "mg", SourceID: firstID(blankIDs),
		},
		{
			Step: "final_mass", Description: "Corrected final mass = observed - drift - blank",
			ValueMg: res.FinalMassMg, Unit: "mg", ArtifactID: corrID,
		},
		{
			Step: "residual_ratio", Description: "Residual percent = final mass / initial mass x 100",
			ValuePercent: res.ResidualPercent, Unit: "%", ArtifactID: setID,
			Children: []domain.ChainNode{{
				Step:        "no_gap_interpolation",
				Description: "Final mass is a real sample; data gaps are never bridged by interpolation",
			}},
		},
	}
	return mkArtifact(snap, domain.ArtResidual, "residual",
		"Final residual mass and expandable calculation chain",
		nil, []string{valID, corrID, setID}, res)
}

func buildBalance(snap *Snapshot, resID, setID string, cs *domain.CorrectedSeries, resArt domain.Artifact) domain.Artifact {
	init := resArtValue(resArt).InitialMassMg
	final := 0.0
	if lu := lastUsable(cs); lu != nil {
		final = lu.CorrectedMg
	}
	totalLoss := init - final
	accepted := 0.0
	// accepted loss comes from currently accepted proposals only (service layer
	// decides status); engine reports total loss and marks unaccounted after
	bal := domain.MassBalance{
		InitialMassMg: init, FinalMassMg: final,
		AcceptedLossMg:    accepted,
		UnaccountedLossMg: totalLoss, UnaccountedPercent: pct(totalLoss, init),
		AcceptedPercent: pct(accepted, init), ForcedTo100: false,
		Note: "Stage losses are not forced to sum to 100%; overlaps and unattributed loss remain explicit.",
	}
	return mkArtifact(snap, domain.ArtMassBalance, "mass-balance",
		"Mass balance with explicit unaccounted loss (never renormalized to 100%)",
		nil, []string{resID, setID}, bal)
}

func lastUsable(cs *domain.CorrectedSeries) *domain.CorrectedSample {
	for i := len(cs.Samples) - 1; i >= 0; i-- {
		row := cs.Samples[i]
		// unconvertible rows keep zero values; skip them rather than inventing data
		if row.TimeS == 0 && row.TempC == 0 && row.ObservedMg == 0 {
			continue
		}
		r := row
		return &r
	}
	return nil
}

func firstID(ids []string) string {
	if len(ids) == 0 {
		return ""
	}
	return ids[0]
}

func pct(v, of float64) float64 {
	if of == 0 {
		return 0
	}
	return 100 * v / of
}
