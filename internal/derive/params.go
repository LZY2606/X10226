package derive

import (
	"encoding/json"

	"tga-review/internal/domain"
)

// AlgoVersion identifies the derivation code. Bump when the math changes so
// stale results are detected rather than silently replaced.
const AlgoVersion = "tga-derive-2026.09.0"

// DefaultParams are the built-in analysis parameters (revision 0).
//
// 气氛延迟 gas_delay_s: valve switch -> gas actually reaching the sample.
// 温度滞后 temp_lag_s: furnace setpoint -> measured sample temperature.
func DefaultParams() map[string]domain.Quantity {
	return map[string]domain.Quantity{
		"gas_delay_s":            domain.Q("0", "s", "gas_transport_delay"),
		"temp_lag_s":             domain.Q("30", "s", "sample_temp_lag"),
		"dtg_window_points":      domain.Q("3", "", "dtg_smoothing_window"),
		"peak_prominence_frac":   domain.Q("0.00020", "%/min", "min_dtg_prominence"),
		"stage_min_width_s":      domain.Q("60", "s", "min_stage_width"),
		"max_gap_span_s":         domain.Q("45", "s", "gap_threshold"),
		"nonmonotonic_band_mg":   domain.Q("0.020", "mg", "noise_band"),
		"overlap_split_depth":    domain.Q("2", "", "overlap_candidate_depth"),
		"residue_window_s":       domain.Q("120", "s", "final_residue_window"),
	}
}

// Fingerprint is the content hash of a parameter revision.
func Fingerprint(params map[string]domain.Quantity) string {
	b, _ := json.Marshal(domain.Canonical{V: params})
	sum := domain.SHA256(b)
	return "fp_" + hexShort(sum[:])
}
