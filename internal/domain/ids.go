package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// NewCID returns a stable content-addressed identity: prefix + first 16
// hexadecimal chars of SHA-256 over a deterministic JSON rendering.
// The same logical object rebuilt after export/delete/import keeps its id.
func NewCID(prefix string, v any) string {
	b, err := stableJSON(v)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return prefix + hex.EncodeToString(sum[:])[:16]
}

// BlobID is the full SHA-256 over the raw bytes of a submitted input.
func BlobID(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "bl_" + hex.EncodeToString(sum[:])
}

// SID builds a project-scoped sequential identity.
func SID(prefix string, projectID string, seq int64) string {
	b := stableJSONOrPanic([]any{"seq", projectID, seq})
	sum := sha256.Sum256(b)
	return prefix + hex.EncodeToString(sum[:])[:16]
}

func stableJSONOrPanic(v any) []byte {
	b, err := stableJSON(v)
	if err != nil {
		panic(err)
	}
	return b
}

// stableJSON marshals with map keys sorted, so identity hashing is
// independent of Go map iteration order.
func stableJSON(v any) ([]byte, error) {
	return json.Marshal(canonicalize(v))
}

func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		for i := 1; i < len(keys); i++ {
			for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
				keys[j-1], keys[j] = keys[j], keys[j-1]
			}
		}
		out := make(map[string]any, len(keys))
		for _, k := range keys {
			out[k] = canonicalize(t[k])
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = canonicalize(t[i])
		}
		return out
	default:
		return v
	}
}
