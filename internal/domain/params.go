package domain

// Params is the revisioned computation parameter set. Every derived value
// carries the fingerprint of the Params it was computed under.
type Params struct {
	Version       string  `json:"version"`
	DriftMode     string  `json:"drift_mode"` // "linear" | "blank" | "none"
	SmoothWindow  int     `json:"smooth_window"`
	GapFactor     float64 `json:"gap_factor"` // gap if dt > median*factor
	PeakMinProm   float64 `json:"peak_min_prom"` // fraction of mass0
	PeakMinSepS   float64 `json:"peak_min_sep_s"`
	AtmDelayS     float64 `json:"atm_delay_s"`     // 气氛延迟
	TempLagC      float64 `json:"temp_lag_c"`      // 温度滞后
	OverlapAllow  bool    `json:"overlap_allow"`
	NoiseZSigmas  float64 `json:"noise_z_sigmas"`
	NoiseMinRun   int     `json:"noise_min_run"`
	GapInterp     bool    `json:"gap_interpolate"` // always false; gaps never span
}

// DefaultParams returns the baseline algorithm parameter version.
func DefaultParams() Params {
	return Params{
		Version:      "tga-algo-1.0",
		DriftMode:    "linear",
		SmoothWindow: 3,
		GapFactor:    5,
		PeakMinProm:  0.005,
		PeakMinSepS:  20,
		AtmDelayS:    0,
		TempLagC:     0,
		OverlapAllow: true,
		NoiseZSigmas: 4,
		NoiseMinRun:  3,
		GapInterp:    false,
	}
}
