package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strconv"
)

// Stable, content-addressed identifiers. Re-importing an identical export
// reproduces the same ids, preserving identity, order and provenance.

func hash16(parts ...string) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write([]byte(p))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// ProjectID is derived from the submitted name + creation nonce so that a
// repeated demo import of the bundled "same" project stays stable.
func ProjectID(name, nonce string) string { return "prj_" + hash16("project", name, nonce) }

// SourceID identifies an immutable raw-input version.
func SourceID(project, kind string, version int, parent string, canonical any) string {
	return "src_" + hash16("source", project, kind, itoa(version), parent, canonicalJSON(canonical))
}

// EventID is stable for (project, seq, kind, payload); seq alone is the order key.
func EventID(project string, seq int64, kind string, payload any) string {
	return "evt_" + hash16("event", project, itoa64(seq), kind, canonicalJSON(payload))
}

// DerivedID is content-addressed by algorithm version, parameter fingerprint
// and the sorted set of direct source versions. Local input changes produce a
// new id only for derivations that depend on the changed input.
func DerivedID(kind, algoVersion, paramFingerprint string, sourceIDs []string) string {
	cp := append([]string(nil), sourceIDs...)
	sort.Strings(cp)
	return "der_" + hash16("derived", kind, algoVersion, paramFingerprint, canonicalJSON(cp))
}

// BatchID identifies one import submission (may partially fail).
func BatchID(project string, seq int64) string {
	return "bat_" + hash16("batch", project, itoa64(seq))
}

// FragmentID identifies an import fragment inside a batch.
func FragmentID(batch, kind, index string) string {
	return "frg_" + hash16("fragment", batch, kind, index)
}

func canonicalJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "ERR:" + err.Error()
	}
	return string(b)
}

func itoa(i int) string     { return strconv.Itoa(i) }
func itoa64(i int64) string { return strconv.FormatInt(i, 10) }
