package compute

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"github.com/thermal/tga/internal/domain"
)

const AlgorithmVersion = "tga-decomp-1.3.0"

func DefaultParams() domain.Params {
	return domain.Params{
		Version:        1,
		AtmDelayS:      18,
		TempLagS:       12,
		SmoothWindowS:  24,
		PeakPromMGMin:  0.02,
		MinStageLossMG: 0.05,
		GapThresholdS:  45,
		DriftEnabled:   true,
		BlankEnabled:   true,
	}
}

// Fingerprint is a stable content hash of the parameter set.
func Fingerprint(p domain.Params) string {
	b, _ := json.Marshal(p)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}
