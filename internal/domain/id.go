package domain

import (
	"fmt"
	"strings"
)

// Stable identifiers are deterministic from content/order so that export ->
// delete database -> import preserves identity, order and source relations.

func ProjectStableID(seq int) string                { return fmt.Sprintf("P%03d", seq) }
func EventStableID(pid string, seq int) string      { return fmt.Sprintf("%s-E%04d", pid, seq) }
func SubmissionStableID(pid string, seq int) string { return fmt.Sprintf("%s-S%03d", pid, seq) }
func DerivationID(pid string, seq int) string       { return fmt.Sprintf("%s-D%03d", pid, seq) }
func CandidateID(pid string, dseq, group, alt, pos int) string {
	return fmt.Sprintf("%s-D%03d-G%d-A%d-P%d", pid, dseq, group, alt, pos)
}
func ResidueID(pid string, dseq, idx int) string {
	return fmt.Sprintf("%s-D%03d-R%d", pid, dseq, idx)
}
func DecisionStableID(pid string, seq int) string { return fmt.Sprintf("%s-C%03d", pid, seq) }

// StableSubKey identifies a layer payload within a project independent of the
// submission id, e.g. S:raw_points:hash8.
func StableSubKey(layer Layer, hash string) string {
	return "S:" + string(layer) + ":" + ShortID(hash, 8)
}

// MutationID returns a deterministic seed for fixed-seed synthetic data.
func MutationID(name string) int {
	h := Fingerprint(strings.ToUpper(name))
	var n int
	for _, c := range h[:8] {
		n = n*31 + int(c)
	}
	if n < 0 {
		n = -n
	}
	return n
}
