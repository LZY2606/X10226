package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// SHAID returns a short stable content id. Identities are content-derived so
// export -> delete -> import preserves exactly the same ids and ordering.
func SHAID(kind string, parts ...any) string {
	h := sha256.New()
	fmt.Fprint(h, kind)
	for _, p := range parts {
		fmt.Fprint(h, "|")
		fmt.Fprint(h, p)
	}
	sum := h.Sum(nil)
	return kind + "_" + hex.EncodeToString(sum[:10])
}

// Hash returns the full SHA-256 hex of a payload.
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}
