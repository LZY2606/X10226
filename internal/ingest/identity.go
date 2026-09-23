package ingest

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// StableID derives a content-addressed stable identifier. Because it has no
// random component, export -> delete -> import preserves every identity.
func StableID(parts ...any) string {
	b, _ := json.Marshal(parts)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:16])
}

// ShortID keeps IDs readable in the UI while staying globally unique.
func ShortID(prefix string, parts ...any) string {
	return prefix + "_" + StableID(parts...)[:16]
}

// Fingerprint returns a short content fingerprint for parameters/payloads.
func Fingerprint(algoVersion string, v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return fmt.Sprintf("%s:%s", algoVersion, hex.EncodeToString(sum[:8]))
}
