package tga

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Algorithm versions. Bumping any of these creates new artifact identities so
// history always replays against the rules that produced it.
const (
	AlgoNormalise = "tga.normalise/1"
	AlgoTG        = "tga.tgdtg/1"
	AlgoStages    = "tga.stages/1"
	AlgoResidue   = "tga.residue/1"
)

// hashCanonical produces a deterministic content fingerprint. Floats are
// rendered through encoding/json so the fingerprint is stable across runs.
func hashCanonical(v any) string {
	b, err := json.Marshal(canonSort(v))
	if err != nil {
		panic(err)
	}
	sum := sha1.Sum(b)
	return hex.EncodeToString(sum[:])
}

// canonSort sorts map keys so fingerprints are independent of Go map order.
func canonSort(v any) any {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		// simple insertion sort keeps zero deps
		for i := 1; i < len(keys); i++ {
			for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
				keys[j-1], keys[j] = keys[j], keys[j-1]
			}
		}
		out := make(map[string]any, len(x))
		for _, k := range keys {
			out[k] = canonSort(x[k])
		}
		return out
	case []any:
		for i := range x {
			x[i] = canonSort(x[i])
		}
		return x
	default:
		return v
	}
}

// commitID is the stable identity of an immutable raw commit: project + section
// + content, so export/import and replay keep the same id.
func commitID(projectID string, seq int, section Section, rawText, label string) string {
	return "raw_" + short(hashCanonical([]any{projectID, int(seq), string(section), rawText, label}))
}

// eventID is content-addressed by position and payload, making replay
// deterministic and idempotent.
func eventID(projectID string, seq int, kind string, payload any) string {
	return "evt_" + short(hashCanonical([]any{projectID, seq, kind, payload}))
}

// artifactID identifies a derived artifact by algorithm, parameter fingerprint
// and direct sources. A local input change only changes ids that depend on it.
func artifactID(algo string, paramFP string, sources []SourceVersion, extra any) string {
	return "art_" + short(hashCanonical(map[string]any{
		"algo": algo, "params": paramFP, "sources": sources, "extra": extra,
	}))
}

// valueID identifies a single displayed derived number, stable across export /
// delete / re-import. It chains through the owning artifact.
func valueID(artID, name string, parts ...any) string {
	return "val_" + short(hashCanonical(append([]any{artID, name}, parts...)))
}

func short(h string) string {
	if len(h) <= 12 {
		return h
	}
	return h[:12]
}

// stableProjectID derives a project id from its title seed.
func stableProjectID(title string) string {
	return "prj_" + short(hashCanonical([]string{"project", title}))
}

// DecisionID is content-addressed from event position and payload.
func decisionID(projectID string, seq int, payload any) string {
	return "dec_" + short(hashCanonical([]any{projectID, seq, payload}))
}

// basisID identifies a machine-readable judgement basis event.
func basisID(projectID string, seq int, code string, payload any) string {
	return "rule_" + short(hashCanonical([]any{projectID, seq, code, payload}))
}

var _ = fmt.Sprintf
