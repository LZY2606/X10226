package tga

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

func hashBytes(parts ...[]byte) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write(p)
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))
}

func shortHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// SourceID is deterministic in project, kind, sequence and raw bytes so the
// same archive round-trips through export/delete/import with identical ids.
func SourceID(project string, kind StreamKind, seq int, parent, raw string) string {
	return "src-" + shortHash([]byte(project + "|" + string(kind) + "|" +
		itoa(seq) + "|" + parent + "|" + raw))
}

// EventID is deterministic in project, era, type, payload and clock stamp.
func EventID(project string, era int64, typ string, payload []byte, clock string) string {
	return "evt-" + shortHash([]byte(project + "|" + itoa64(era) + "|" + typ + "|" +
		string(payload) + "|" + clock))
}

// ArtifactID derives a stable identity for a derived artifact from the
// algorithm version, parameter fingerprint, the sorted set of source versions
// in its basis and an artifact-type-specific key.
func ArtifactID(algo, paramFP string, basisVersionIDs []string, key string) string {
	b := append([]string(nil), basisVersionIDs...)
	sort.Strings(b)
	j, _ := json.Marshal(struct {
		A string   `json:"a"`
		P string   `json:"p"`
		B []string `json:"b"`
		K string   `json:"k"`
	}{algo, paramFP, b, key})
	return "art-" + shortHash(j)
}
