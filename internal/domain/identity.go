package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

// Stable identity rules
//
//	Raw source nodes keep the identity minted at their first commit, forever.
//	Derived nodes are content-addressed: the same inputs + algorithm version +
//	parameter fingerprint reproduce the same identity after export/import.
//
// IDs carry a prefix so provenance chains stay legible in the UI and database.

const (
	PrefixProject    = "prj_"
	PrefixSource     = "src_"
	PrefixVersion    = "ver_"
	PrefixEvent      = "evt_"
	PrefixBatch      = "bat_"
	PrefixDerived    = "der_"
	PrefixDecision   = "dec_"
	PrefixImportSess = "ims_"
	PrefixDiagnostic = "diag_"
	PrefixCandidate  = "cand_"
)

// ContentID derives a stable, content-addressed identifier from a namespace and
// a deterministically serialized payload.
func ContentID(prefix, namespace string, payload any) (string, error) {
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|", prefix, namespace)
	h.Write(b)
	return prefix + hex.EncodeToString(h.Sum(nil))[:32], nil
}

// Fingerprint is the parameter hash attached to every derived value.
type Fingerprint struct {
	Algorithm string `json:"algorithm"` // algorithm/rule name
	Version   string `json:"version"`   // semantic version of that algorithm
	Params    string `json:"params"`    // canonical JSON of the parameter set
}

func (f Fingerprint) String() string { return f.Algorithm + "@" + f.Version + ":" + f.Hash() }

// Hash returns a short stable hash of the whole fingerprint.
func (f Fingerprint) Hash() string {
	b, _ := json.Marshal(f)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])[:16]
}

// Equal compares two fingerprints.
func (f Fingerprint) Equal(o Fingerprint) bool {
	return f.Algorithm == o.Algorithm && f.Version == o.Version && f.Params == o.Params
}
