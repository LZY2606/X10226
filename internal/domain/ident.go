package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

// Hash8 returns the first 8 bytes (16 hex chars) of SHA-256 over data.
func Hash8(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:8])
}

// CanonicalJSON hashes JSON after normalizing map/struct ordering.
func CanonicalJSON(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ContentID builds a stable, content-addressed source identity.
// Re-importing byte-identical payloads yields the same id, so exports and
// re-imports preserve provenance.
func ContentID(projectID, kind, label string, version int, payload []byte) string {
	key := struct {
		P       string `json:"p"`
		K       string `json:"k"`
		L       string `json:"l"`
		V       int    `json:"v"`
		Payload []byte `json:"payload"`
	}{projectID, kind, label, version, payload}
	return "src_" + kind + "_" + Hash8(CanonicalJSON(key))
}

// Fingerprint returns a stable hash for algorithm parameters.
func Fingerprint(algo string, params any) string {
	key := struct {
		Algo   string `json:"algo"`
		Params any    `json:"params"`
	}{algo, params}
	return "fp_" + Hash8(CanonicalJSON(key))
}

// DerivationID is content-addressed over algorithm, params and direct sources.
// A local change to one input only changes ids of derivations that consume it.
func DerivationID(algo string, paramFP string, sources ...string) string {
	key := struct {
		Algo    string   `json:"algo"`
		ParamFP string   `json:"param_fp"`
		Sources []string `json:"sources"`
	}{algo, paramFP, sources}
	return "der_" + algo + "_" + Hash8(CanonicalJSON(key))
}

// EventID is stable from the sequence position and payload content.
func EventID(projectID string, seq int, payload []byte, at string, etype string) string {
	key := struct {
		P   string `json:"p"`
		S   int    `json:"s"`
		T   string `json:"t"`
		E   string `json:"e"`
		Pay []byte `json:"pay"`
	}{projectID, seq, at, etype, payload}
	return "evt_" + Hash8(CanonicalJSON(key))
}

// BatchID derives from project sequence and immutable raw text.
func BatchID(projectID string, seq int, raw string) string {
	key := struct {
		P string `json:"p"`
		S int    `json:"s"`
		R string `json:"r"`
	}{projectID, seq, raw}
	return "bat_" + Hash8(CanonicalJSON(key))
}

// UID prefixes for UI display values.
const (
	UIDPoint      = "pt"
	UIDSeries     = "series"
	UIDTimeline   = "tl"
	UIDSegment    = "seg"
	UIDGas        = "gas"
	UIDProg       = "prog"
	UIDStage      = "stage"
	UIDResidue    = "res"
	UIDGap        = "gap"
	UIDDerivation = "der"
)
