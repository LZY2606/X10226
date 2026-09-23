package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// HashID returns a short stable content identifier: prefix + 16 hex chars.
func HashID(prefix string, parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		b, err := json.Marshal(p)
		if err != nil {
			b = []byte(fmt.Sprint(p))
		}
		h.Write(b)
		h.Write([]byte{0})
	}
	return prefix + "_" + hex.EncodeToString(h.Sum(nil))[:16]
}

// ParamFingerprint identifies a parameter version deterministically.
func ParamFingerprint(p Params) string {
	return HashID("params", p)
}
