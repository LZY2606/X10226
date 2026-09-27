package domain

// Algorithm versions (the domain rule set that produced a derived value).
const (
	AlgoCorrectSeries  = "correct-series@v1"
	AlgoTG             = "tg@v1"
	AlgoDTG            = "dtg@v1"
	AlgoReactionWindow = "reaction-window@v1"
	AlgoStages         = "stages@v1"
	AlgoResidue        = "residue@v1"
)

// Params are the tunable analysis parameters. All derived fingerprints embed
// the exact params they used.
type Params struct {
	// GasDelayS shifts a valve switch to the sample's reaction clock.
	GasDelayS float64 `json:"gas_delay_s"`
	// TempLagS shifts measured temperature relative to program clock.
	TempLagS float64 `json:"temp_lag_s"`
	// DTG smoothing half-window in seconds (boxcar on valid, adjacent points).
	SmoothHalfWindowS float64 `json:"smooth_half_window_s"`
	// PeakDepthPctMin: minimum smoothed-DTG depth to seed a peak (%/min).
	PeakDepthPctMin float64 `json:"peak_depth_pct_per_min"`
	// StageExtendS extends a peak valley into shoulders by flatness threshold.
	StageExtendS float64 `json:"stage_extend_s"`
	// OverlapZoneS: valleys within this window generate overlapping candidates.
	OverlapZoneS float64 `json:"overlap_zone_s"`
	// ResidueTailS: final plateau window length for the residue read.
	ResidueTailS float64 `json:"residue_tail_s"`
	// NoisePctMax flags non-monotone upticks above this (of initial mass).
	NoisePctMax float64 `json:"noise_pct_max"`
}

// DefaultParams returns the domain defaults used by the case pack baseline.
func DefaultParams() Params {
	return Params{
		GasDelayS:         20,
		TempLagS:          30,
		SmoothHalfWindowS: 30,
		PeakDepthPctMin:   0.02,
		StageExtendS:      45,
		OverlapZoneS:      120,
		ResidueTailS:      90,
		NoisePctMax:       0.25,
	}
}

// ParamFingerprint hashes the parameter set actually used.
func ParamFingerprint(p Params) string {
	return Hash(p)[:16]
}
