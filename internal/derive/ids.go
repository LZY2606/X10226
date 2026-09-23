// Package derive implements the TGA derivation pipeline. Every derived value
// is content-addressed: identical direct sources + parameters + algorithm
// version reproduce identical artifact and value identities, across
// recomputation, export and re-import.
package derive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

func hashOf(v any) string {
	b, _ := json.Marshal(v)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

func short(v any) string {
	h := hashOf(v)
	return h[:12]
}

func fkv(v float64) string { return fmt.Sprintf("%.6g", v) }

// ArtifactID is stable for a type and its inputs.
func ArtifactID(t string, inputsHash string) string {
	return t + "_" + short(t+"|"+inputsHash)[:10]
}

// ValueID gives every displayed value a stable identity rooted in the
// enclosing artifact and the value's role.
func ValueID(artifactID, role, key string) string {
	return artifactID + ":" + role + ":" + short(role+"|"+key)[:8]
}

// StageID is content-addressed from boundaries and peak position.
func StageID(group, ordinal int, onsetS, offsetS, peakS float64, gas string) string {
	key := fmt.Sprintf("%d|%d|%.4f|%.4f|%.4f|%s", group, ordinal, onsetS, offsetS, peakS, gas)
	return "stage_" + short(key)[:10]
}

// ParamsFingerprint captures everything that changes computation output.
// AlgorithmVersion is tracked separately in provenance, not the fingerprint.
func ParamsFingerprint(p map[string]any) string {
	keys := make([]string, 0, len(p))
	for k := range p {
		if k == "algorithm_version" {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)
	b := ""
	for _, k := range keys {
		b += k + "=" + fmt.Sprintf("%v", p[k]) + ";"
	}
	return "pf_" + short(b)[:10]
}
