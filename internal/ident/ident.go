// Package ident builds stable, content-derived identifiers so that identity,
// ordering and source relations survive export/delete-database/re-import.
package ident

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"tga-review/internal/types"
)

// CanonicalJSON serializes a value deterministically.
func CanonicalJSON(v any) ([]byte, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var c any
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(c); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

// Hash returns the sha256 hex of canonical JSON.
func Hash(v any) (string, error) {
	b, err := CanonicalJSON(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func short(h string) string {
	if len(h) > 12 {
		return h[:12]
	}
	return h
}

// ProjectID derives from the immutable project key.
func ProjectID(key string) string {
	sum := sha256.Sum256([]byte("project:" + key))
	return "prj_" + hex.EncodeToString(sum[:])[:16]
}

// SourceID is stable for a (project, kind, seq) source version; the content
// hash additionally guarantees tamper detection.
func SourceID(projectID, kind string, seq int, contentHash string) string {
	return fmt.Sprintf("src_%s_%s_v%d_%s", projectID[4:12], kind, seq, short(contentHash))
}

// RulesetID identifies a parameter revision.
func RulesetID(projectID string, seq int, fp string) string {
	return fmt.Sprintf("rul_%s_v%d_%s", projectID[4:12], seq, short(fp))
}

// RulesetFingerprint hashes algo version + sorted params.
func RulesetFingerprint(algo string, params map[string]float64) (string, error) {
	return Hash(struct {
		Algo   string             `json:"algo"`
		Params map[string]float64 `json:"params"`
	}{algo, params})
}

// RunID is content-derived from its direct source versions and ruleset.
func RunID(projectID string, seq int, sources map[string]string, rulesetID string) (string, error) {
	h, err := Hash(struct {
		Sources map[string]string `json:"sources"`
		Ruleset string            `json:"ruleset"`
	}{sources, rulesetID})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("run_%s_%d_%s", projectID[4:12], seq, short(h)), nil
}

// CandidateID identifies a candidate within a run.
func CandidateID(runID, stageID, kind string) string {
	sum := sha256.Sum256([]byte(runID + "|" + kind + "|" + stageID))
	return "cand_" + hex.EncodeToString(sum[:])[:16]
}

// PointID gives every displayed series point a stable identity.
func PointID(runID, series string, idx int) string {
	return fmt.Sprintf("pt_%s_%s_%04d", runID[4:12], series, idx)
}

// ChainStepID gives every calculation-chain step a stable identity.
func ChainStepID(runID, stageID string, step int) string {
	return fmt.Sprintf("step_%s_%s_%02d", runID[4:12], stageID, step)
}

// DecisionID is derived from candidate, action and sequence.
func DecisionID(projectID string, seq int, candidateID, action string) string {
	sum := sha256.Sum256([]byte(candidateID + "|" + action + "|" + fmt.Sprint(seq)))
	return fmt.Sprintf("dec_%s_%03d_%s", projectID[4:12], seq, hex.EncodeToString(sum[:])[:8])
}

// EventID is content-derived so re-importing the same event is idempotent.
func EventID(projectID string, seq int64, kind, payload, at string) string {
	sum := sha256.Sum256([]byte(projectID + "|" + fmt.Sprint(seq) + "|" + kind + "|" + at + "|" + payload))
	return "evt_" + hex.EncodeToString(sum[:])[:16]
}

// DecisionSeqNumber etc. are just ints; keep types referenced.
var _ = types.EvProjectCreated
