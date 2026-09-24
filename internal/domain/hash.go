package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

type orderedKV struct {
	k string
	v any
}

// CanonicalBytes renders v as deterministic JSON: object keys sorted
// lexicographically, no insignificant whitespace, NaN/Inf rejected.
func CanonicalBytes(v any) ([]byte, error) {
	var b []byte
	if err := appendCanonical(&b, v); err != nil {
		return nil, err
	}
	return b, nil
}

func appendCanonical(b *[]byte, v any) error {
	switch t := v.(type) {
	case nil:
		*b = append(*b, "null"...)
	case string:
		kb, err := json.Marshal(t)
		if err != nil {
			return err
		}
		*b = append(*b, kb...)
	case bool:
		if t {
			*b = append(*b, "true"...)
		} else {
			*b = append(*b, "false"...)
		}
	case int:
		*b = strconvAppendInt(*b, int64(t))
	case int64:
		*b = strconvAppendInt(*b, t)
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return fmt.Errorf("canonical: non-finite number")
		}
		*b = appendFloat(*b, t)
	case float32:
		f := float64(t)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return fmt.Errorf("canonical: non-finite number")
		}
		*b = appendFloat(*b, f)
	case Q:
		return appendCanonical(b, t.Canonical())
	case orderedKV:
		kb, _ := json.Marshal(t.k)
		*b = append(*b, kb...)
		*b = append(*b, ':')
		if err := appendCanonical(b, t.v); err != nil {
			return err
		}
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		*b = append(*b, '{')
		for i, k := range keys {
			if i > 0 {
				*b = append(*b, ',')
			}
			if err := appendCanonical(b, orderedKV{k, t[k]}); err != nil {
				return err
			}
		}
		*b = append(*b, '}')
	case []any:
		*b = append(*b, '[')
		for i, e := range t {
			if i > 0 {
				*b = append(*b, ',')
			}
			if err := appendCanonical(b, e); err != nil {
				return err
			}
		}
		*b = append(*b, ']')
	default:
		raw, err := json.Marshal(v)
		if err != nil {
			return err
		}
		var back any
		if err := json.Unmarshal(raw, &back); err != nil {
			return err
		}
		return appendCanonical(b, back)
	}
	return nil
}

// Hash returns the hex sha256 of the canonical encoding.
func Hash(v any) (string, error) {
	b, err := CanonicalBytes(v)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// ShortID returns a stable content id with a one-letter-ish prefix.
// It panics on non-hashable input: id inputs are constructed internally
// and must never contain NaN/Inf or non-finite values.
func ShortID(prefix string, v any) string {
	h, err := Hash(v)
	if err != nil {
		panic(fmt.Sprintf("domain: cannot hash id input: %v", err))
	}
	return prefix + h[:16]
}
