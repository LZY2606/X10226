package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

func CanonicalJSON(value any) json.RawMessage {
	raw, _ := json.Marshal(canonicalValue(value))
	return raw
}

func canonicalValue(value any) any {
	switch typed := value.(type) {
	case json.RawMessage:
		var decoded any
		if err := json.Unmarshal(typed, &decoded); err == nil {
			return canonicalValue(decoded)
		}
		return string(typed)
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		out := make([][2]any, 0, len(keys))
		for _, key := range keys {
			out = append(out, [2]any{key, canonicalValue(typed[key])})
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i := range typed {
			out[i] = canonicalValue(typed[i])
		}
		return out
	default:
		return value
	}
}

func Fingerprint(parts ...any) string {
	values := make([]any, len(parts))
	for i, part := range parts {
		values[i] = canonicalValue(part)
	}
	sum := sha256.Sum256(CanonicalJSON(values))
	return hex.EncodeToString(sum[:])
}

func ShortFingerprint(parts ...any) string {
	return Fingerprint(parts...)[:16]
}
