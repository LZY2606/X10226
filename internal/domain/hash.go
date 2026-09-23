package domain

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// StableHash returns a lowercase hex SHA-256 over a canonical JSON encoding
// (object keys sorted lexicographically; no extra whitespace).
func StableHash(v any) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(canonical(v))
	sum := sha256.Sum256(bytes.TrimRight(buf.Bytes(), "\n"))
	return hex.EncodeToString(sum[:])
}

func ShortHash(v any) string {
	h := StableHash(v)
	if len(h) >= 8 {
		return h[:8]
	}
	return h
}

func canonical(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return orderedMap{m: t, keys: keys}
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = canonical(t[i])
		}
		return out
	default:
		return v
	}
}

type orderedMap struct {
	m    map[string]any
	keys []string
}

func (o orderedMap) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, k := range o.keys {
		if i > 0 {
			buf.WriteByte(',')
		}
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		buf.Write(kb)
		buf.WriteByte(':')
		vb, err := json.Marshal(canonical(o.m[k]))
		if err != nil {
			return nil, err
		}
		buf.Write(vb)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}
