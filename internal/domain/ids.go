package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
)

// CanonicalJSON serializes a value deterministically: map keys are sorted and
// no HTML escaping or whitespace is emitted. This makes fingerprints identical
// across processes, platforms and export/import round trips.
func CanonicalJSON(v any) []byte {
	var raw any
	data, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("domain: cannot marshal for canonicalization: %v", err))
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		panic(err)
	}
	out, err := json.Marshal(canonicalize(raw))
	if err != nil {
		panic(err)
	}
	return out
}

func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make([][2]any, 0, len(keys))
		for _, k := range keys {
			out = append(out, [2]any{k, canonicalize(t[k])})
		}
		return orderedMap(out)
	case []any:
		for i := range t {
			t[i] = canonicalize(t[i])
		}
		return t
	default:
		return v
	}
}

// orderedMap preserves insertion order during a second marshal pass.
type orderedMap [][2]any

func (m orderedMap) MarshalJSON() ([]byte, error) {
	var b []byte
	b = append(b, '{')
	for i, kv := range m {
		if i > 0 {
			b = append(b, ',')
		}
		key, err := json.Marshal(kv[0])
		if err != nil {
			return nil, err
		}
		b = append(b, key...)
		b = append(b, ':')
		val, err := json.Marshal(kv[1])
		if err != nil {
			return nil, err
		}
		b = append(b, val...)
	}
	b = append(b, '}')
	return b, nil
}

// Hash returns the hex SHA-256 of the canonical serialization.
func Hash(v any) string {
	sum := sha256.Sum256(CanonicalJSON(v))
	return hex.EncodeToString(sum[:])
}

// ContentID builds a stable, content-derived identifier with a short prefix.
// IDs therefore survive export/delete/re-import as long as content is equal.
func ContentID(prefix string, parts ...any) string {
	h := sha256.New()
	for _, p := range parts {
		h.Write(CanonicalJSON(p))
		h.Write([]byte{0})
	}
	return fmt.Sprintf("%s_%s", prefix, hex.EncodeToString(h.Sum(nil))[:16])
}

// EventHash computes the integrity hash of an event over all content fields
// plus the previous event's hash (chained log).
func EventHash(e *Event) string {
	return Hash(map[string]any{
		"project": e.ProjectID,
		"seq":     e.Seq,
		"kind":    e.Kind,
		"at":      e.At.UTC().Format("2006-01-02T15:04:05.000000000Z"),
		"actor":   e.Actor,
		"payload": e.Payload,
		"prev":    e.PrevHash,
	})
}
