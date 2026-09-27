// Package compute is the deterministic derived layer.
//
// It never mutates imported data. Every output value carries the algorithm
// version, a parameter fingerprint and direct source identities, and exposes
// the arithmetic chain used to obtain it. Unknowns and rejected candidates are
// first-class outputs.
package compute

// AlgorithmVersion identifies the calculation code. Bump on any semantic change.
const AlgorithmVersion = "tga-compute-3"

// Params are the tunable rules for one derivation.
type Params struct {
	// AtmosphereDelay is the constant transport delay applied to every valve
	// event before it denotes a reaction-atmosphere boundary (minutes).
	AtmosphereDelay float64 `json:"atmosphere_delay_min"`
	// FurnaceLag is the simple first-order-ish temperature lag applied when
	// mapping furnace setpoints onto measured sample temperature (minutes).
	FurnaceLag float64 `json:"furnace_lag_min"`
	// GapThreshold: a time spacing above this between adjacent accepted sample
	// points opens a data gap; curves are never interpolated across it (min).
	GapThreshold float64 `json:"gap_threshold_min"`
	// PeakProminence is the minimum DTG magnitude for a peak (%/min).
	PeakProminence float64 `json:"peak_prominence_pct_per_min"`
	// OverlapTolerance permits two peak candidates to merge as an overlapping
	// cluster when closer than this temperature separation (K).
	OverlapTolerance float64 `json:"overlap_tolerance_K"`
	// DriftCorrection toggles balance baseline-drift correction.
	DriftCorrection bool `json:"drift_correction"`
	// BlankCorrection toggles empty-pan blank subtraction.
	BlankCorrection bool `json:"blank_correction"`
	// StageFootExtension widens each detected stage beyond the peak shoulders (min).
	StageFootExtension float64 `json:"stage_foot_extension_min"`
}

// DefaultParams used by the first derivation and by the demo.
func DefaultParams() Params {
	return Params{
		AtmosphereDelay:    0.8,
		FurnaceLag:         0.5,
		GapThreshold:       2.0,
		PeakProminence:    0.15,
		OverlapTolerance:  35.0,
		DriftCorrection:   true,
		BlankCorrection:   true,
		StageFootExtension: 0.4,
	}
}

// Fingerprint returns a stable short content fingerprint for the parameters.
func (p Params) Fingerprint() string {
	// Fixed key order via struct; format manually to stay stable.
	return shortHash(
		ftoa(p.AtmosphereDelay), ftoa(p.FurnaceLag), ftoa(p.GapThreshold),
		ftoa(p.PeakProminence), ftoa(p.OverlapTolerance),
		btoa(p.DriftCorrection), btoa(p.BlankCorrection),
		ftoa(p.StageFootExtension),
	)
}
