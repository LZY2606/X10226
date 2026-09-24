package engine

import "tgareview/internal/domain"

// AlgoVersion is bumped when any calculation semantics change.
const AlgoVersion = "tga-decomp-v1"

// Params are the reviewable analysis parameters. Time is seconds,
// temperature Celsius, mass milligrams, rate %/min or C/min.
type Params struct {
	AlgoVersion       string  `json:"algo_version"`
	AtmDelayS         float64 `json:"atm_delay_s"`
	TempLagC          float64 `json:"temp_lag_c"`
	GapFactor         float64 `json:"gap_factor"`
	DTGWindow         int     `json:"dtg_window"`
	PeakPromPctPerMin float64 `json:"peak_prom_pct_per_min"`
	MinLossPct        float64 `json:"min_loss_pct"`
	DriftApplied      bool    `json:"drift_applied"`
	BlankApplied      bool    `json:"blank_applied"`
	NoiseRunMax       int     `json:"noise_run_max"`
}

// DefaultParams returns the v1 default parameter set.
func DefaultParams() Params {
	return Params{
		AlgoVersion:       AlgoVersion,
		AtmDelayS:         0,
		TempLagC:          0,
		GapFactor:         5,
		DTGWindow:         0,
		PeakPromPctPerMin: 0.05,
		MinLossPct:        0.5,
		DriftApplied:      true,
		BlankApplied:      true,
		NoiseRunMax:       3,
	}
}

// Fingerprint hashes the effective parameter set.
func (p Params) Fingerprint() string {
	return domain.Fingerprint(AlgoVersion, p)
}
