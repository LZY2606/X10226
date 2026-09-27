package unit

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
)

func math_Float64frombits(b uint64) float64 { return math.Float64frombits(b) }

// stableHex returns the first n chars of the lowercase sha256 hex digest.
// Deterministic across machines and Go versions (used for stable identities).
func stableHex(s string, n int) string {
	sum := sha256.Sum256([]byte(s))
	h := hex.EncodeToString(sum[:])
	if n > len(h) {
		n = len(h)
	}
	return h[:n]
}
