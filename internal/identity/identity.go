// Package identity builds stable, content-derived identifiers so identity,
// ordering and provenance survive export, database deletion and re-import.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

func short(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// For creates "prefix:<16 hex chars>" identifiers from canonical parts.
func For(prefix string, parts ...string) string {
	return prefix + ":" + short(parts...)
}

// Fingerprint returns a full content hash for structured data.
func Fingerprint(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
