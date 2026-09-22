package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
)

// StableID returns a deterministic hex id from stable id parts.
func StableID(parts ...string) string {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return hex.EncodeToString(h[:16])
}

// ShortID keeps stable ids readable in the UI while remaining deterministic.
func ShortID(parts ...string) string {
	return StableID(parts...)[:12]
}

// Fingerprint is a content fingerprint for a parameter/string set.
func Fingerprint(parts ...string) string {
	sort.Strings(parts)
	return StableID(parts...)[:16]
}

// AlgorithmVersion is the current version of the pure compute pipeline.
const AlgorithmVersion = "tga-stage-v1.0.0"

// DefaultParams returns the fixed default parameter revision.
func DefaultParams() Params {
	return Params{
		AtmosphereDelayS:  45,
		TemperatureLagC:   8,
		DriftRateMgPerMin: 0.02,
		PeakProminenceMgS: 0.0004,
		PeakMergeWindowS:  120,
		NoiseTolMg:        0.01,
		GapThresholdS:     60,
		BlankScale:        1.0,
		AlgorithmVersion:  AlgorithmVersion,
	}
}

// ParamsFingerprint hashes the algorithm configuration.
func (p Params) Fingerprint() string {
	return Fingerprint(
		AlgorithmVersion,
		qf(p.AtmosphereDelayS), qf(p.TemperatureLagC), qf(p.DriftRateMgPerMin),
		qf(p.PeakProminenceMgS), qf(p.PeakMergeWindowS), qf(p.NoiseTolMg),
		qf(p.GapThresholdS), qf(p.BlankScale),
	)
}
