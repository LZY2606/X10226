package analysis

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"

	"tga-archive/internal/model"
)

func shortHash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:12]
}

// canonical serializes deterministically. Go's encoding/json emits struct keys
// in declaration order and map keys sorted, which is stable across replays.
func canonical(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

// ParamFingerprint identifies the exact algorithm + parameter set.
func ParamFingerprint(p model.Params) string {
	return "pf_" + shortHash(canonical(p))
}

type layerRef struct {
	Kind    string `json:"kind"`
	ID      string `json:"id"`
	Version int    `json:"version"`
	Hash    string `json:"hash"`
}

// StateHash is content-addressed from parameter fingerprint and the exact
// direct-source layers. Same sources + same parameters => same hash, so all
// stable value identities reproduce after delete/re-import.
func StateHash(pf string, refs []layerRef) string {
	return "st_" + shortHash(canonical(struct {
		PF    string     `json:"pf"`
		Refs []layerRef `json:"refs"`
	}{pf, refs}))
}

// LayerContentHash hashes the payload of a layer (without identity metadata).
func LayerContentHash(l *model.Layer) string {
	payload := struct {
		Kind     model.LayerKind `json:"kind"`
		Samples  *model.Samples  `json:"samples,omitempty"`
		Blank    *model.Blank    `json:"blank,omitempty"`
		Furnace  *model.Furnace  `json:"furnace,omitempty"`
		Gases    *model.Gases    `json:"gases,omitempty"`
		Drift    *model.Drift    `json:"drift,omitempty"`
		Initial  *model.Initial  `json:"initial,omitempty"`
	}{l.Kind, l.Samples, l.Blank, l.Furnace, l.Gases, l.Drift, l.Initial}
	return "lh_" + shortHash(canonical(payload))
}

// nid builds a stable node identity inside a state.
func nid(stateHash, key string, parts ...int) string {
	id := stateHash + ":" + key
	for _, p := range parts {
		id += ":" + strconv.Itoa(p)
	}
	return id
}
