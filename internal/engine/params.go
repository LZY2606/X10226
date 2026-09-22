package engine

import (
	"tga-review/internal/ident"
)

// AlgoVersion is bumped only when the deterministic calculation changes.
const AlgoVersion = "tga-stage-decomp/v1.0.0"

// DefaultParams for the stage decomposition ruleset.
func DefaultParams() map[string]float64 {
	return map[string]float64{
		"gap_multiple":           5.0,  // dt > median*this marks a sampling gap
		"noise_jitter_mg":        0.05, // mass rises above this are non-monotonic
		"min_dtg_pct_min":        0.20, // |DTG| floor for a real loss peak
		"peak_prominence_pctmin": 0.15, // trough prominence; below it merge alt.
		"min_stage_span_c":       3.0,  // minimum stage width in degC
		"atmos_delay_s":          30.0, // valve -> sample atmosphere delay
		"temp_lag_s":             20.0, // programmed -> sample temperature lag
		"residue_window_s":       60.0, // final-residue averaging window
		"dtg_window_pts":         2.0,  // central difference half-width in points
	}
}

// Fingerprint hashes algo version + params.
func Fingerprint(params map[string]float64) (string, error) {
	return ident.RulesetFingerprint(AlgoVersion, params)
}
