package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// hashCanonical returns the first 16 hex chars of SHA-256 over a canonical
// (key-sorted) JSON encoding. It makes ids deterministic for replay and
// import/export, independent of map ordering.
func hashCanonical(v any) string {
	b, err := json.Marshal(canonicalize(v))
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// fullHash returns the full 32-byte hex digest, used for content blobs.
func fullHash(v any) string {
	b, err := json.Marshal(canonicalize(v))
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			out[k] = canonicalize(val)
		}
		return out
	default:
		// json.Marshal already sorts object keys; json.RawMessage is passed
		// through as-is. Structs encode with deterministic field order.
		return v
	}
}

// idWith builds a stable, prefixed content id.
func idWith(prefix string, v any) string {
	return prefix + "_" + hashCanonical(v)
}
