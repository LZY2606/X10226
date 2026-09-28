package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// StableID derives a content-addressed identity for a piece of data. JSON is
// encoded deterministically (struct field order, sorted keys) so the same
// logical object always yields the same ID across export/delete/re-import.
func StableID(prefix string, payload any) string {
	b, err := json.Marshal(canonical{V: payload})
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return prefix + "_" + hex.EncodeToString(sum[:])[:20]
}

type canonical struct{ V any }

// MarshalJSON sorts map keys so map-bearing payloads stay deterministic.
func (c canonical) MarshalJSON() ([]byte, error) {
	b, err := json.Marshal(c.V)
	if err != nil {
		return nil, err
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v) // encoding/json sorts map keys
}
