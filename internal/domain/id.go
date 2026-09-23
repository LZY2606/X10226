package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// shortHash returns a 16-hex-char content hash over canonical JSON.
func shortHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// Canonical serializes a value deterministically (sorted JSON keys).
func Canonical(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ContentID builds a stable, content-addressed identifier with a prefix.
// Identical inputs anywhere (this DB or a re-imported archive) produce the
// same ID; that is what keeps identities and source relations stable across
// export/delete-database/import cycles.
func ContentID(prefix string, parts ...any) string {
	h := sha256.New()
	h.Write([]byte(prefix))
	h.Write([]byte{0})
	for _, p := range parts {
		h.Write(Canonical(p))
		h.Write([]byte{0})
	}
	return prefix + "-" + hex.EncodeToString(h.Sum(nil))[:16]
}

// RowID identifies a preserved raw row by its stratum, line and content.
func RowID(stratum, raw string, lineNo int) string {
	return ContentID("raw", stratum, lineNo, raw)
}
