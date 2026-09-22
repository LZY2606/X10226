package store

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
	"strings"
)

// hash12 returns the first 12 hex chars of a SHA-256 over the stable joining of
// the parts. Identities are content-addressed so an archive exported, deleted
// locally and re-imported keeps identical IDs, ordering and source relations.
func hash12(parts ...string) string {
	h := sha256.New()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0})
		}
		h.Write([]byte(p))
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}

// CanonicalJSON renders JSON with sorted object keys for fingerprints.
func CanonicalJSON(v any) string {
	b, err := json.Marshal(canonicalize(v))
	if err != nil {
		panic(err)
	}
	return string(b)
}

func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var sb strings.Builder
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			sb.Write(kb)
			sb.WriteByte(':')
			sb.WriteString(CanonicalJSON(canonicalize(t[k])))
		}
		sb.WriteByte('}')
		return json.RawMessage(sb.String())
	default:
		return v
	}
}

// SubmissionID derives the stable identity of a raw layer version.
func SubmissionID(projectID string, kind string, version int, canonicalPayload string) string {
	return kind + "_" + hash12("sub", projectID, kind, itoa(version), canonicalPayload)
}

// ParamsID derives the stable identity of a parameter revision.
func ParamsID(projectID string, version int, canonicalParams string) string {
	return "params_" + hash12("par", projectID, itoa(version), canonicalParams)
}

// ResultID derives the stable, content-addressed identity of a derived result.
func ResultID(algo, paramsFP string, sources [][2]string) string {
	parts := make([]string, 0, len(sources)*2+2)
	parts = append(parts, "res", algo, paramsFP)
	cp := make([][2]string, len(sources))
	copy(cp, sources)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i][0] != cp[j][0] {
			return cp[i][0] < cp[j][0]
		}
		return cp[i][1] < cp[j][1]
	})
	for _, s := range cp {
		parts = append(parts, s[0], s[1])
	}
	return hash12(parts...)
}

// DecisionID derives stable identity for a human decision.
func DecisionID(projectID, subjectID, verdict, rationale string, seq int64) string {
	return "dec_" + hash12("dec", projectID, subjectID, verdict, rationale, itoaSeq(seq))
}

// EventID derives stable identity for ledger events (seq keeps replay order).
func EventID(projectID string, seq int64, t, summary string) string {
	return "evt_" + hash12("evt", projectID, itoaSeq(seq), t, summary)
}

func itoa(v int) string { return itoaSeq(int64(v)) }

func itoaSeq(v int64) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
