// Package ident builds stable, deterministic identifiers and fingerprints.
// Derived ids are a function of their inputs, so export -> delete -> import
// preserves identity, order and source relationships.
package ident

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func hashHex(b []byte, n int) string {
	h := sha256.Sum256(b)
	s := hex.EncodeToString(h[:])
	if n > 0 && n < len(s) {
		return s[:n]
	}
	return s
}

// Short is a stable 12-char hash of any encodable value.
func Short(v any) string {
	b, _ := json.Marshal(canonical(v))
	return hashHex(b, 12)
}

// Fingerprint is a full sha256 hash of canonical JSON.
func Fingerprint(v any) string {
	b, _ := json.Marshal(canonical(v))
	return hashHex(b, 0)
}

func canonical(v any) any {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	var x any
	if err := json.Unmarshal(b, &x); err != nil {
		return string(b)
	}
	return x
}

// ProjectID derives a stable project id from its name.
func ProjectID(name string) string { return "prj-" + hashHex([]byte("project:"+name), 10) }

// BatchID derives a stable batch id from project, source and submission bytes.
func BatchID(project, source string, payload []byte) string {
	return "bat-" + hashHex(append([]byte(project+"|"+source+"|"), payload...), 12)
}

// EventID is sequential: ids only ever move forward.
func EventID(project string, seq int64) string {
	return fmt.Sprintf("evt-%s-%06d", Short(project), seq)
}

// RunID derives from project and run counter.
func RunID(project string, n int) string {
	return fmt.Sprintf("run-%s-%03d", Short(project), n)
}

// SampleID etc. derive directly from batch id and ordinal, so re-import of the
// same batch bytes reconstructs identical ids.
func SampleID(batch string, ord int) string  { return fmt.Sprintf("smp-%s-%04d", Short(batch), ord) }
func DriftID(batch string, ord int) string   { return fmt.Sprintf("dft-%s-%04d", Short(batch), ord) }
func ProgramID(batch string, ord int) string { return fmt.Sprintf("prg-%s-%04d", Short(batch), ord) }
func GasID(batch string, ord int) string     { return fmt.Sprintf("gas-%s-%04d", Short(batch), ord) }
func BlankID(batch string, ord int) string   { return fmt.Sprintf("blk-%s-%04d", Short(batch), ord) }
func InitialID(batch string, ord int) string { return fmt.Sprintf("inm-%s-%04d", Short(batch), ord) }

// DecisionID derives from project, target and event seq.
func DecisionID(project, targetID string, seq int64) string {
	return "dec-" + hashHex([]byte(fmt.Sprintf("%s|%s|%d", project, targetID, seq)), 10)
}
