// Package identity builds deterministic, human-readable stable identifiers.
// Identities are content-addressed where possible so that deleting the local
// database and re-importing an archive preserves ids, ordering and provenance.
package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
)

// Hash returns the hex sha256 of the canonical JSON encoding of v.
func Hash(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Short returns a 12-char stable prefix.
func Short(v any) string {
	return Hash(v)[:12]
}

// ProjectID is stable across export/import; it is supplied by the operator
// (or the case pack) and never regenerated.
func ProjectID(code string) string { return "proj_" + sanitize(code) }

// SubmissionID identifies one immutable submitted source blob:
// proj / layer / ordinal / content hash.
func SubmissionID(project, layer string, ordinal int, contentHash string) string {
	return fmt.Sprintf("src_%s_%s_%03d_%s", sanitize(project), layer, ordinal, contentHash[:12])
}

// ImportSessionID identifies a (possibly half-failed) import attempt.
func ImportSessionID(project string, ordinal int, hash string) string {
	return fmt.Sprintf("imp_%s_%03d_%s", sanitize(project), ordinal, hash[:12])
}

// NodeID identifies a derived result node. Deps must be the ordered stable ids
// of its direct sources, paramsFP the algorithm parameter fingerprint.
func NodeID(kind, algo string, deps []string, paramsFP string) string {
	body := struct {
		Kind   string   `json:"kind"`
		Algo   string   `json:"algo"`
		Deps   []string `json:"deps"`
		Params string   `json:"params_fp"`
	}{kind, algo, deps, paramsFP}
	return "node_" + kind + "_" + Short(body)
}

// CandidateID identifies a candidate object (peak, stage ...). Deterministic
// from the node it belongs to, its rank and its key interval.
func CandidateID(nodeID, kind string, rank int, key string) string {
	return "cand_" + Short(struct {
		Node string `json:"n"`
		Kind string `json:"k"`
		Rank int    `json:"r"`
		Key  string `json:"x"`
	}{nodeID, kind, rank, key})
}

// DecisionID identifies a human decision. Sequence is the global event
// sequence, so decisions are ordered and unique across export/import.
func DecisionID(project string, seq int64) string {
	return "dec_" + sanitize(project) + "_" + strconv.FormatInt(seq, 10)
}

// EventID identifies a ledger event by its position and content hash.
func EventID(seq int64, contentHash string) string {
	return "evt_" + strconv.FormatInt(seq, 10) + "_" + contentHash[:12]
}

func sanitize(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			out = append(out, r)
		case r >= 'A' && r <= 'Z':
			out = append(out, r+('a'-'A'))
		default:
			out = append(out, '-')
		}
	}
	res := string(out)
	if len(res) > 24 {
		res = res[:24]
	}
	return res
}
