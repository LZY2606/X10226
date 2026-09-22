package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// ContentID derives a stable content-addressed id: prefix + 16 hex chars of
// SHA-256 over a canonical JSON rendering of v. Identity therefore survives
// export -> database deletion -> import.
func ContentID(prefix string, v any) string {
	b, err := json.Marshal(canonical(v))
	if err != nil {
		// canonical uses JSON-native types only in call sites; fall back to
		// marshalling the error surface.
		b = []byte("marshal-error:" + err.Error())
	}
	sum := sha256.Sum256(b)
	return prefix + "_" + hex.EncodeToString(sum[:])[:16]
}

// Fingerprint returns the full hex fingerprint of canonical JSON.
func Fingerprint(v any) string {
	b, _ := json.Marshal(canonical(v))
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// canonical converts maps/structs through JSON so map keys sort (Go's
// encoding/json sorts map keys) and floats are formatted deterministically.
func canonical(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var out any
	if err := json.Unmarshal(b, &out); err != nil {
		return v
	}
	return out
}
