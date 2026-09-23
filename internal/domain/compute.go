package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// GapFactor: an internal time gap wider than GapFactor * median interval is a
// real sampling gap; derived integration never spans it (no interpolation).
const GapFactor = 5.0

// SmoothingK is the moving-average half-window for DTG noise suppression.
const SmoothingK = 2

// SmoothingID identifies the smoothing method in fingerprints.
const SmoothingID = "moving_average_k2"

// fingerprint builds a stable parameter fingerprint for one derived node.
func fingerprint(projectID, kind, groupKey string, direct []Ref, params interface{}) string {
	b, _ := json.Marshal(params)
	type fr struct {
		Kind string `json:"kind"`
		ID   string `json:"id"`
		Role string `json:"role"`
	}
	rs := make([]fr, 0, len(direct))
	for _, r := range direct {
		rs = append(rs, fr{Kind: r.Kind, ID: r.ID, Role: r.Role})
	}
	sort.Slice(rs, func(i, j int) bool {
		if rs[i].Kind != rs[j].Kind {
			return rs[i].Kind < rs[j].Kind
		}
		if rs[i].ID != rs[j].ID {
			return rs[i].ID < rs[j].ID
		}
		return rs[i].Role < rs[j].Role
	})
	rb, _ := json.Marshal(rs)
	h := sha256.Sum256([]byte(projectID + "\n" + kind + "\n" + groupKey + "\n" +
		AlgoVersion + "\n" + string(b) + "\n" + string(rb)))
	return "fp_" + hex.EncodeToString(h[:])[:16]
}

// StableID derives the stable identity of a node from what it is, not when it
// was created. Re-export/delete/re-import therefore keeps identity, order and
// provenance relationships.
func StableID(projectID, kind, groupKey string, direct []Ref, params interface{}, payload json.RawMessage) string {
	fp := fingerprint(projectID, kind, groupKey, direct, params)
	h := sha256.Sum256(append(append([]byte(fp), 0x1e), payload...))
	return kind + "_" + hex.EncodeToString(h[:])[:16]
}

func ptr(v float64) *float64 { return &v }
