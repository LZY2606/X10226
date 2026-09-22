// Package engine performs deterministic TGA stage decomposition. It is a pure
// function of (layer revisions, parameters): identical inputs and parameter
// fingerprints always reproduce identical candidates, residue chains, rule
// judgements and ids. It never mutates history.
package engine

import (
	"encoding/json"

	"tgareview/internal/domain"
	"tgareview/internal/ids"
)

const AlgorithmVersion = "tga-decomp/1"

// Params are the tunable decomposition parameters (units explicit).
type Params struct {
	// AtmosphereDelayS models transport delay between valve and sample.
	AtmosphereDelayS float64 `json:"atmosphere_delay_s"`
	// ThermalLagTauS first-order lag: T_sample = lowpass(T_furnace), tau seconds.
	ThermalLagTauS float64 `json:"thermal_lag_tau_s"`
	// BaselineDriftRateMgPerMin overrides the raw drift-correction layer rate.
	BaselineDriftRateMgPerMin float64 `json:"baseline_drift_rate_mg_per_min"`
	// DtgWindow is the moving-average smoothing window (seconds) for DTG.
	DtgWindowS float64 `json:"dtg_window_s"`
	// MinPeakProminencePctPerMin is the minimum DTG prominence (%/min).
	MinPeakProminencePctPerMin float64 `json:"min_peak_prominence_pct_per_min"`
	// SplitOverlap enables multiple legal decompositions for overlapping peaks.
	SplitOverlap bool `json:"split_overlap"`
	// GapMaxS: adjacent samples farther apart than this are a gap; intervals
	// are never interpolated across it.
	GapMaxS float64 `json:"gap_max_s"`
	// NoiseBlipRelTol flags non-monotonic excursions below this relative size.
	NoiseBlipRelTol float64 `json:"noise_blip_rel_tol"`
	// RngSeed fixes every stochastic choice (candidate tie-breaking).
	RngSeed int64 `json:"rng_seed"`
}

// DefaultParams used by v1 analyses.
func DefaultParams() Params {
	return Params{
		AtmosphereDelayS:           12,
		ThermalLagTauS:             18,
		BaselineDriftRateMgPerMin:  -1,
		DtgWindowS:                 20,
		MinPeakProminencePctPerMin: 0.05,
		SplitOverlap:               true,
		GapMaxS:                    15,
		NoiseBlipRelTol:            5e-4,
		RngSeed:                    20260922,
	}
}

// Fingerprint is the stable hash of parameters; stored on every derived value.
func (p Params) Fingerprint() string {
	b, _ := json.Marshal(p)
	return ids.Hash(string(domain.CanonicalJSON(b)))
}
