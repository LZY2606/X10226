package model

// Params are the versioned analysis parameters. Every revision is stored as
// a new Params version with a fingerprint; derived results record which
// parameter fingerprint they were produced under.
type Params struct {
	AlgoVersion string  `json:"algoVersion"`
	// GasDelayS: transport delay between valve switch and gas reaching sample.
	GasDelayS float64 `json:"gasDelayS"`
	// TempLagK: temperature lag between measured furnace temperature and the
	// temperature actually experienced by the sample.
	TempLagK float64 `json:"tempLagK"`
	// PeakPromPct: minimum DTG prominence for a peak (in %/min).
	PeakPromPct float64 `json:"peakPromPct"`
	// OverlapSplitPct: two peaks closer in time than this (fraction of their
	// widths) are treated as an overlapping group; both merged and split
	// candidates are retained.
	OverlapSplitPct float64 `json:"overlapSplitPct"`
	// NoiseAbsPct: absolute deviation threshold for non-monotonic noise (in
	// mass percent) on what should locally be a monotone descent.
	NoiseAbsPct float64 `json:"noiseAbsPct"`
	// GapMaxS: a missing-time interval wider than this is a gap that is NOT
	// bridged by interpolation.
	GapMaxS float64 `json:"gapMaxS"`
	// SmoothWinS: smoothing window (s) used before differentiation.
	SmoothWinS float64 `json:"smoothWinS"`
}

const AlgoV1 = "tga-stage-v1"

func DefaultParams() Params {
	return Params{
		AlgoVersion:     AlgoV1,
		GasDelayS:       0,
		TempLagK:        0,
		PeakPromPct:     0.15,
		OverlapSplitPct: 1.5,
		NoiseAbsPct:     0.08,
		GapMaxS:         20,
		SmoothWinS:      6,
	}
}
