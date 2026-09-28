package app

import (
	"context"
	"errors"

	"tgareview/internal/domain"
	"tgareview/internal/derive"
	"tgareview/internal/store"
)

// DerivationRun is the result of a derivation.
type DerivationRun struct {
	Event     domain.Event       `json:"event"`
	Params    domain.Params      `json:"params"`
	ParamFP   string             `json:"param_fp"`
	Inputs    []string           `json:"inputs"`
	TG        derive.TGResult    `json:"tg"`
	DTG       derive.DTGResult   `json:"dtg"`
	Atmo      derive.AtmoResult  `json:"atmo"`
	Candidates derive.CandidatesResult `json:"candidates"`
}

// RunDerivation recomputes the whole pipeline from the current sources and
// records one derivation_run event plus five artifacts in one transaction.
func (a *App) RunDerivation(ctx context.Context, projectID, actor string) (*DerivationRun, error) {
	in, err := a.gatherInputs(ctx, projectID)
	if err != nil {
		return nil, err
	}
	if in.Sample == nil {
		return nil, errors.New("缺少 sample_points 来源，无法派生")
	}
	p, err := a.CurrentParams(ctx, projectID)
	if err != nil {
		return nil, err
	}

	tg := derive.BuildTG(in, p)
	dtg := derive.BuildDTG(tg, p)
	atmo := derive.BuildAtmo(in, p)
	cands := derive.BuildCandidates(tg, dtg, atmo, p)
	inputIDs := in.IDs()
	fp := p.Fingerprint()

	tx, err := a.St.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	ev, err := a.St.AppendEventTx(ctx, tx, projectID, domain.EvDerivationRun, actor,
		"派生运行（算法 "+domain.AlgoVersion+"，参数指纹 "+fp[:12]+"…）", map[string]any{
			"algo": domain.AlgoVersion, "param_fp": fp, "inputs": inputIDs,
			"params_version": p.Version,
			"n_stages":       len(cands.Stages), "n_residues": len(cands.Residues),
			"gaps":           len(tg.Gaps),
		})
	if err != nil {
		return nil, err
	}
	mk := func(kind string, data any, cs []domain.Candidate) *domain.Artifact {
		return &domain.Artifact{ProjectID: projectID, Kind: kind, AlgoVersion: domain.AlgoVersion,
			ParamFP: fp, Inputs: inputIDs, Data: data, Candidates: cs}
	}
	if err := a.St.SaveArtifact(ctx, tx, mk(domain.ArtCorrectedTG, tg, nil), ev); err != nil {
		return nil, err
	}
	if err := a.St.SaveArtifact(ctx, tx, mk(domain.ArtDTG, dtg, nil), ev); err != nil {
		return nil, err
	}
	if err := a.St.SaveArtifact(ctx, tx, mk(domain.ArtAtmoTimeline, atmo, nil), ev); err != nil {
		return nil, err
	}
	if err := a.St.SaveArtifact(ctx, tx, mk(domain.ArtCandidates, cands,
		append(append([]domain.Candidate{}, cands.Stages...), cands.Residues...)), ev); err != nil {
		return nil, err
	}
	if err := a.St.SaveArtifact(ctx, tx, mk(domain.ArtResidue, map[string]any{
		"residues": cands.Residues, "final_mass_mg": tg.FinalMass,
		"initial_mass_mg": tg.InitialMass,
	}, cands.Residues), ev); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &DerivationRun{Event: ev, Params: p, ParamFP: fp, Inputs: inputIDs,
		TG: tg, DTG: dtg, Atmo: atmo, Candidates: cands}, nil
}

func (a *App) gatherInputs(ctx context.Context, projectID string) (derive.Inputs, error) {
	var in derive.Inputs
	get := func(kind string) *domain.Source {
		s, err := a.St.LatestSource(ctx, projectID, kind)
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		if err != nil {
			return nil
		}
		return s
	}
	in.Sample = get(domain.SrcSamplePoints)
	in.Drift = get(domain.SrcDrift)
	in.Furnace = get(domain.SrcFurnace)
	in.Gas = get(domain.SrcGasValve)
	in.Blank = get(domain.SrcBlank)
	in.InitialMass = get(domain.SrcInitialMass)
	return in, nil
}

// IsStale reports whether an artifact is current: its algorithm version,
// parameter fingerprint and all direct source inputs must match the present.
func (a *App) IsStale(ctx context.Context, projectID string, art domain.Artifact) (bool, []string) {
	var reasons []string
	if art.AlgoVersion != domain.AlgoVersion {
		reasons = append(reasons, "算法版本已更新: "+art.AlgoVersion+" → "+domain.AlgoVersion)
	}
	p, err := a.CurrentParams(ctx, projectID)
	if err == nil && art.ParamFP != p.Fingerprint() {
		reasons = append(reasons, "参数指纹不匹配（参数已修订）")
	}
	latest := map[string]string{}
	for _, k := range domain.SourceKinds {
		if s, err := a.St.LatestSource(ctx, projectID, k); err == nil {
			latest[k] = s.ID
		}
	}
	idKind := map[string]string{}
	for _, k := range domain.SourceKinds {
		if s, err := a.St.LatestSource(ctx, projectID, k); err == nil {
			idKind[s.ID] = k
		}
	}
	_ = idKind
	for _, in := range art.Inputs {
		found := false
		for _, cur := range latest {
			if cur == in {
				found = true
				break
			}
		}
		if !found {
			reasons = append(reasons, "直接来源已有新版本: "+short(in))
		}
	}
	return len(reasons) > 0, reasons
}

func short(id string) string {
	if len(id) > 24 {
		return id[:24] + "…"
	}
	return id
}
