package domain

// AlgoVersion is bumped whenever the derivation algorithm changes.
const AlgoVersion = "tga-derive-1.0.0"

// Params are the tunable algorithm parameters. Revisions are versioned and
// fingerprinted so every derived value can say which parameter set produced it.
type Params struct {
	Version             int     `json:"version"`
	AtmoDelayS          float64 `json:"atmo_delay_s"`            // gas transport delay (furnace clock), s
	TempLagS            float64 `json:"temp_lag_s"`               // sample-vs-furnace temperature lag, s
	PeakProminencePct   float64 `json:"peak_prominence_pct"`      // DTG peak prominence threshold, %/min
	OverlapPeakPromPct  float64 `json:"overlap_peak_prom_pct"`    // lower threshold used to split overlapping peaks
	SmoothingWindow     int     `json:"smoothing_window"`         // odd, number of samples
	DriftCorrection     bool    `json:"drift_correction"`
	BlankCorrection     bool    `json:"blank_correction"`
	NoiseNonMonotonicPct float64 `json:"noise_nonmonotonic_pct"`  // |dm| below this is noise, not a rise
	GapMaxDtS           float64 `json:"gap_max_dt_s"`             // larger consecutive dt marks a sampling gap
}

// DefaultParams is the fixed starting parameter set.
func DefaultParams() Params {
	return Params{
		Version: 1, AtmoDelayS: 60, TempLagS: 45,
		PeakProminencePct: 0.15, OverlapPeakPromPct: 0.06,
		SmoothingWindow: 5, DriftCorrection: true, BlankCorrection: true,
		NoiseNonMonotonicPct: 0.12, GapMaxDtS: 25,
	}
}

// Fingerprint returns the stable content hash of a parameter set.
func (p Params) Fingerprint() string {
	return ContentID("pfp", CanonicalJSON(p))
}

// Equal compares the tunable fields (version excluded from revision content).
func (p Params) Equal(o Params) bool {
	return p == o
}
