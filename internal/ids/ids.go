// Package ids computes stable, content-derived identifiers used across projects,
// exports and re-imports. An id is the prefix followed by the first 16 hex
// characters of SHA-256(scope + NUL + body). Because ids are derived only
// from immutable content, deleting the local database and re-importing the
// same export reconstructs identical ids, ordering and provenance edges.
package ids

import (
	"crypto/sha256"
	"encoding/hex"
)

// Of returns "<prefix>-" + 16 hex chars of sha256(scope+NUL+body).
// scope must identify the id space (e.g. project + entity kind).
func Of(prefix, scope, body string) string {
	h := sha256.New()
	h.Write([]byte(scope))
	h.Write([]byte{byte(0)})
	h.Write([]byte(body))
	return prefix + "-" + hex.EncodeToString(h.Sum(nil))[:16]
}

// Hash returns the 16-char hex sha256 of body, useful for fingerprints.
func Hash(body string) string {
	h := sha256.Sum256([]byte(body))
	return hex.EncodeToString(h[:])[:16]
}
