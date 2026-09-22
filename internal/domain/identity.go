package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// canonicalJSON encodes with sorted keys (encoding/json sorts map keys) and
// no HTML escaping so the bytes are stable across machines.
func canonicalJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("canonical json: %v", err))
	}
	return b
}

func hashBytes(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// DigestOf returns a stable content digest for any JSON-able value.
func DigestOf(v any) string { return hashBytes(canonicalJSON(v)) }

// RawID builds a content-addressed raw source version ID.
// tga:raw:<project8>:<kind>:v<version>:<hash12>
func RawID(projectID string, kind SourceKind, version int, digest string) string {
	return fmt.Sprintf("tga:raw:%s:%s:v%d:%s", short(projectID), kind, version, digest[:12])
}

// ParamID builds a content-addressed parameter version ID.
func ParamID(projectID string, version int, digest string) string {
	return fmt.Sprintf("tga:params:%s:v%d:%s", short(projectID), version, digest[:12])
}

// DerivedID builds a derived computation ID from param version + direct source
// digests + algorithm version, guaranteeing identical inputs reproduce the
// identical identity after export/import.
func DerivedID(projectID, algoVersion, paramID string, sources map[string]int, sourceDigests []string) string {
	key := map[string]any{
		"p": projectID, "a": algoVersion, "pv": paramID,
		"s": sources, "d": sourceDigests,
	}
	return fmt.Sprintf("tga:derived:%s:%s", short(projectID), hashBytes(canonicalJSON(key))[:16])
}

// DecisionID builds a stable decision ID from project + sequence + target.
func DecisionID(projectID string, seq int, targetID string) string {
	return fmt.Sprintf("tga:dec:%s:d%04d:%s", short(projectID), seq, DigestOf(targetID)[:8])
}

// ImportRef returns a deterministic import batch identifier.
func ImportRef(seed string, attempt int) string {
	return fmt.Sprintf("imp_%s_%d", hashBytes([]byte(seed))[:10], attempt)
}

func short(id string) string {
	if len(id) <= 10 {
		return id
	}
	return id[len(id)-8:]
}

// HistoryHash chains history events.
func HistoryHash(seq int64, projectID, typ string, payload []byte, prevHash string) string {
	return hashBytes([]byte(fmt.Sprintf("%d|%s|%s|%s|%s", seq, projectID, typ, payload, prevHash)))
}
