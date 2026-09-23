package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// CanonicalJSON serializes a value deterministically: struct field order via
// encoding/json, and maps are sorted by key.
func CanonicalJSON(v any) []byte {
	b, err := json.Marshal(sortMaps(v))
	if err != nil {
		panic(err)
	}
	return b
}

func sortMaps(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		// insertion-independent: json.Marshal already sorts map keys,
		// but recurse into values.
		for _, k := range keys {
			t[k] = sortMaps(t[k])
		}
		return t
	case []any:
		for i := range t {
			t[i] = sortMaps(t[i])
		}
		return t
	default:
		return v
	}
}

// HashID returns a short stable content id: prefix + 16 hex chars.
func HashID(prefix string, v any) string {
	sum := sha256.Sum256(CanonicalJSON(v))
	return prefix + "_" + hex.EncodeToString(sum[:])[:16]
}

// Fingerprint returns the full sha256 hex of the canonical form.
func Fingerprint(v any) string {
	sum := sha256.Sum256(CanonicalJSON(v))
	return hex.EncodeToString(sum[:])
}
