package domain

import (
	"encoding/json"
	"strconv"
)

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// MarshalCanonical renders a deterministic JSON byte stream for fingerprints.
func MarshalCanonical(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}
