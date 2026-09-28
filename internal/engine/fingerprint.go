package engine

import (
	"fmt"

	"tga/internal/domain"
)

// inputsFor returns (param fingerprint, direct-input fingerprints) for a layer.
// Each layer depends only on its direct sources and on the previous layer's
// result, which is what makes staleness local.
func inputsFor(kind domain.DerivationKind, cur *currentSources, prev *layerResults) (domain.Params, []any) {
	p := cur.params
	switch kind {
	case domain.DerivSeries:
		return p, []any{
			"v1", cur.samples, driftInput(cur), blankInput(cur), seriesParamSubset(p),
		}
	case domain.DerivDTG:
		return p, []any{"v1", prev.seriesID, dtgParamSubset(p)}
	case domain.DerivStages:
		return p, []any{
			"v1", prev.seriesID, prev.dtgID, cur.gas, cur.program, stagesParamSubset(p),
		}
	default: // residue
		return p, []any{"v1", prev.seriesID, prev.stagesID, cur.initMass, residueParamSubset(p)}
	}
}

type layerResults struct {
	seriesID string
	dtgID    string
	stagesID string
}

func driftInput(cur *currentSources) any {
	if cur.drift == nil {
		return nil
	}
	return cur.drift.Drift
}
func blankInput(cur *currentSources) any {
	if cur.blank == nil {
		return nil
	}
	return cur.blank.Blank
}

func seriesParamSubset(p domain.Params) any {
	return map[string]any{
		"alg": domain.AlgorithmVersion, "window": p.SmoothingWindowN,
		"noise_window": p.NoiseMonotonicWindowN, "gap_max_bridge": p.GapMaxBridgeN,
	}
}
func dtgParamSubset(p domain.Params) any {
	return map[string]any{"alg": domain.AlgorithmVersion, "window": p.SmoothingWindowN, "prom": p.MinProminence}
}
func stagesParamSubset(p domain.Params) any {
	return map[string]any{
		"alg": domain.AlgorithmVersion, "delay": p.GasDelayS, "lag": p.TempLagC,
		"min_prom": p.MinProminence, "min_loss": p.MinStageLossPct,
	}
}
func residueParamSubset(p domain.Params) any {
	return map[string]any{"alg": domain.AlgorithmVersion, "min_loss": p.MinStageLossPct}
}

// Fingerprint is the parameter component of provenance.
func paramFingerprint(p domain.Params) string {
	return domain.StableID("params", []any{domain.AlgorithmVersion, p})
}

func deriveID(kind domain.DerivationKind, fp string, payload any) string {
	return domain.StableID(fmt.Sprintf("deriv-%s", kind), []any{fp, payload})
}
