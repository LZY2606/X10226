package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Hash returns the stable SHA-256 content identity of canonical JSON.
// Field order follows struct declaration; maps are sorted by encoding/json.
func Hash(parts ...any) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{'|'})
		}
		b, err := json.Marshal(p)
		if err != nil {
			panic(err)
		}
		h.Write(b)
	}
	return hex.EncodeToString(h.Sum(nil))
}

// ShortID returns a short content-prefixed stable identifier.
func ShortID(prefix string, parts ...any) string {
	sum := Hash(parts...)
	return prefix + sum[:16]
}
