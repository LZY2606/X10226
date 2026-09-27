package store

import (
	"database/sql"
	"encoding/json"

	"tga/internal/compute"
	"tga/internal/domain"
)

func jsonMap(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}

func fingerprintBody(p domain.Params) map[string]any {
	return map[string]any{
		"window_sec": p.WindowSec, "peak_min_prom": p.PeakMinProm,
		"valve_delay_sec": p.ValveDelaySec, "temp_lag_sec": p.TempLagSec,
		"overlap_tol_sec": p.OverlapTolSec, "gap_max_fill_sec": p.GapMaxFillSec,
		"residue_tail_sec": p.ResidueTailSec, "drift_model": p.DriftModel,
	}
}

// buildInputs assembles compute.Inputs from the current source versions.
func buildInputs(p *Project, params domain.Params) (compute.Inputs, error) {
	in := compute.Inputs{Params: params}
	if raw := p.Current[domain.LayerRaw]; raw != nil {
		in.Raw = raw.Parsed.Raw
	}
	if im := p.Current[domain.LayerInitMass]; im != nil {
		in.Init = im.Parsed.InitMass
	}
	if d := p.Current[domain.LayerDrift]; d != nil {
		in.Drift = d.Parsed.Drift
	}
	if f := p.Current[domain.LayerFurnace]; f != nil {
		in.Furnace = f.Parsed.Furnace
	}
	if g := p.Current[domain.LayerGas]; g != nil {
		in.Gas = g.Parsed.Gas
	}
	if b := p.Current[domain.LayerBlank]; b != nil {
		in.Blank = b.Parsed.Blank
	}
	return in, nil
}

// RunAnalysis derives a new analysis from the project's current source
// versions and the given params. Identity is derived from the exact direct
// source set + params + algorithm version, so identical replays yield the same
// id and a local source change only invalidates analyses that depend on it.
func (s *Store) RunAnalysis(projectID string, params domain.Params, actor string) (*domain.Analysis, error) {
	p, err := s.LoadProject(projectID)
	if err != nil {
		return nil, err
	}
	params, err = s.EnsureParams(projectID, params, actor)
	if err != nil {
		return nil, err
	}
	in, err := buildInputs(p, params)
	if err != nil {
		return nil, err
	}
	if len(in.Raw) == 0 {
		return nil, ErrNoRaw
	}
	result := compute.Derive(in)
	srcIDs := map[string]string{}
	for layer, src := range p.Current {
		srcIDs[string(layer)] = src.ID
	}
	seq := len(p.Analyses) + 1
	a := &domain.Analysis{
		ProjectID: projectID, Seq: seq, SourceIDs: srcIDs,
		Params: params, Result: result, Current: true,
	}
	a.ID = domain.ShortID("analysis", map[string]any{
		"project": projectID, "sources": srcIDs,
		"params": params.Fingerprint, "algo": domain.AlgoVersion})
	err = s.tx(func(tx *sql.Tx) error {
		if _, e := tx.Exec(`UPDATE analyses SET current=0 WHERE project_id=?`, projectID); e != nil {
			return e
		}
		if _, e := tx.Exec(`INSERT OR REPLACE INTO analyses
		 (id,project_id,seq,source_ids,params_fingerprint,body,created_at,current)
		 VALUES (?,?,?,?,?,?,?,1)`, a.ID, projectID, seq, mustJSON(srcIDs),
			params.Fingerprint, mustJSON(a), s.now()); e != nil {
			return e
		}
		_, e := s.appendEventTx(tx, projectID, "analysis_created", actor,
			map[string]any{"analysis": jsonMap(a)})
		return e
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// IsStale reports whether an analysis is outdated relative to current sources:
// only a newer version of a layer the analysis actually used invalidates it.
func (p *Project) IsStale(a *domain.Analysis) bool {
	for layer, usedID := range a.SourceIDs {
		cur, ok := p.Current[domain.Layer(layer)]
		if !ok || cur.ID != usedID {
			return true
		}
	}
	return false
}
