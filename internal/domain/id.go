package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// kind prefixes make stable identities self-describing.
const (
	KindProject  = "prj"
	KindSource   = "src"
	KindNode     = "der"
	KindDecision = "dec"
	KindConflict = "cfl"
	KindEvent    = "evt"
	KindSession  = "ses"
)

// CanonicalJSON serializes a value deterministically: JSON object keys are
// sorted. It is the basis for content fingerprints, so importing the same
// archive elsewhere yields identical ids.
func CanonicalJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("canonical marshal: %v", err))
	}
	var obj any
	if err := json.Unmarshal(raw, &obj); err != nil {
		panic(fmt.Sprintf("canonical unmarshal: %v", err))
	}
	var out []byte
	encodeCanonical(&out, obj)
	return string(out)
}

func encodeCanonical(out *[]byte, v any) {
	switch t := v.(type) {
	case nil:
		*out = append(*out, []byte("null")...)
	case bool:
		if t {
			*out = append(*out, []byte("true")...)
		} else {
			*out = append(*out, []byte("false")...)
		}
	case float64:
		*out = append(*out, []byte(jsonNumber(t))...)
	case string:
		b, _ := json.Marshal(t)
		*out = append(*out, b...)
	case []any:
		*out = append(*out, '[')
		for i, e := range t {
			if i > 0 {
				*out = append(*out, ',')
			}
			encodeCanonical(out, e)
		}
		*out = append(*out, ']')
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		*out = append(*out, '{')
		for i, k := range keys {
			if i > 0 {
				*out = append(*out, ',')
			}
			kb, _ := json.Marshal(k)
			*out = append(*out, kb...)
			*out = append(*out, ':')
			encodeCanonical(out, t[k])
		}
		*out = append(*out, '}')
	default:
		b, _ := json.Marshal(t)
		*out = append(*out, b...)
	}
}

// jsonNumber avoids trailing zeros and exponent drift across runs.
func jsonNumber(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

// Fingerprint returns a short hex hash of canonical content.
func Fingerprint(v any) string {
	sum := sha256.Sum256([]byte(CanonicalJSON(v)))
	return hex.EncodeToString(sum[:16])
}

// ContentID builds an id from a kind prefix and content fingerprint. Ids are
// content-addressed, so export/delete/import preserves identity exactly.
func ContentID(kind string, v any) string {
	return kind + "_" + Fingerprint(v)
}

// SeqID builds an ordered, sequence-numbered identity (decisions, conflicts,
// sessions). The counter is part of the exported archive so ordering survives
// database deletion and re-import.
func SeqID(kind, projectID string, seq int) string {
	return fmt.Sprintf("%s_%s_%04d", kind, short(projectID), seq)
}

func short(id string) string {
	if len(id) <= 14 {
		return id
	}
	return id[:14]
}
