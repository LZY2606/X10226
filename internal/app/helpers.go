package app

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func mustJSON(v any) []byte {
	b, _ := json.Marshal(v)
	return b
}

// randless is a deterministic nonce component derived from stable inputs, so a
// FakeClock re-run produces identical event ids. It never uses randomness.
func randless(parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		b, _ := json.Marshal(p)
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))[:8]
}
