// Package engine performs versioned, provenance-bearing derivation over the
// immutable domain records: baseline drift correction, gap-aware segmentation,
// DTG, overlapping stage candidates, atmosphere-delay mapping and residue
// chains. It never mutates its inputs and never interpolates across gaps.
package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// AlgorithmVersion is bumped when any calculation changes semantics. Every
// derived value records it, so old conclusions never silently take new math.
const AlgorithmVersion = "tgacalc-v1"

// Params are the versioned review parameters. A parameter revision is a new
// immutable ParamsVersion with a parent link, never an overwrite.
type Params struct {
	// GasDelayS models atmosphere transport delay: the composition at the
	// sample changes GasDelayS after the valve command.
	GasDelayS float64 `json:"gas_delay_s"`
	// ThermalLagC models measured sample-vs-furnace temperature lag.
	ThermalLagC float64 `json:"thermal_lag_c"`
	// SmoothingWindowS is the centered DTG smoothing window (seconds).
	SmoothingWindowS float64 `json:"smoothing_window_s"`
	// PeakProminenceUgPerS is the minimum DTG peak prominence (ug/s).
	PeakProminenceUgPerS float64 `json:"peak_prominence_ug_per_s"`
	// OverlapResolveWidthS: peaks closer than this stay in one overlap cluster.
	OverlapResolveWidthS float64 `json:"overlap_resolve_width_s"`
	// GapFactor: a delta greater than GapFactor x median interval is a gap.
	GapFactor float64 `json:"gap_factor"`
	// NonMonotonicRel flags rises in mass above this fraction of local level.
	NonMonotonicRel float64 `json:"non_monotonic_rel"`
	// MinPeakFraction filters noise peaks below this fraction of the global max.
	MinPeakFraction float64 `json:"min_peak_fraction"`
}

// DefaultParams is the initial review parameter set.
func DefaultParams() Params {
	return Params{
		GasDelayS:            18,
		ThermalLagC:          12,
		SmoothingWindowS:     12,
		PeakProminenceUgPerS: 0.8,
		OverlapResolveWidthS: 30,
		GapFactor:            4,
		NonMonotonicRel:      0.0008,
		MinPeakFraction:      0.10,
	}
}

// Fingerprint is a stable content hash over algorithm version + parameters.
func Fingerprint(p Params) string {
	b, _ := json.Marshal(struct {
		V string `json:"algorithm_version"`
		P Params `json:"params"`
	}{AlgorithmVersion, p})
	sum := sha256.Sum256(b)
	return "fp_" + hex.EncodeToString(sum[:])[:16]
}
