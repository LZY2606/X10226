package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ContentID is a stable, content-addressed identifier prefixed with its kind.
// The same logical bytes always produce the same ID, which is what lets an
// export -> delete database -> import cycle preserve identity, order and
// provenance relationships.
func ContentID(kind string, canonical []byte) string {
	sum := sha256.Sum256(canonical)
	return kind + "_" + hex.EncodeToString(sum[:])[:16]
}

// CanonicalJSON serializes v deterministically (sorted keys) for hashing.
func CanonicalJSON(v any) []byte {
	b, err := json.Marshal(canonicalize(v))
	if err != nil {
		panic(err)
	}
	return b
}

// canonicalize normalizes maps so key order never affects hashes.
func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = canonicalize(val)
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
