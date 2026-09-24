package core

import (
	"crypto/sha256"
	"encoding/hex"
)

// ShortHash returns a stable, content-addressed identifier fragment.
// Identities derived from it survive export / delete / re-import cycles.
func ShortHash(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
